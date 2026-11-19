package service

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"slices"
	"testing"
	"time"

	"archilan.fr/orchestrateur/internal/config"
	"archilan.fr/orchestrateur/internal/db"
	"archilan.fr/orchestrateur/internal/docker"
)

type fakeVolumes struct {
	outputs map[string][]byte // session id -> tar of /data/output; a key means the volume exists
	puts    map[string][][]byte
	removed []string
}

func (f *fakeVolumes) VolumeExists(_ context.Context, id string) (bool, error) {
	_, ok := f.outputs[id]
	return ok, nil
}

func (f *fakeVolumes) ListSessionVolumes(context.Context) ([]string, error) {
	ids := []string{}
	for id := range f.outputs {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids, nil
}

func (f *fakeVolumes) LatestSaveFromVolume(_ context.Context, id string) (string, []byte, bool, error) {
	return docker.LatestSaveFromTar(bytes.NewReader(f.outputs[id]))
}

func (f *fakeVolumes) PutDataToVolume(_ context.Context, id string, r io.Reader) error {
	data, _ := io.ReadAll(r)
	f.puts[id] = append(f.puts[id], data)
	if _, ok := f.outputs[id]; !ok {
		f.outputs[id] = nil
	}
	return nil
}

func (f *fakeVolumes) RemoveVolume(_ context.Context, id string) error {
	f.removed = append(f.removed, id)
	delete(f.outputs, id)
	return nil
}

type storedSave struct {
	name string
	data []byte
}

type fakeSaves struct {
	saves     map[string]storedSave
	outputs   map[string][]byte
	uploadErr error
}

func (f *fakeSaves) UploadSessionSave(_ context.Context, id, name string, data []byte) error {
	if f.uploadErr != nil {
		return f.uploadErr
	}
	f.saves[id] = storedSave{name, data}
	return nil
}

func (f *fakeSaves) DownloadSessionSave(_ context.Context, id string) (string, []byte, bool, error) {
	s, ok := f.saves[id]
	return s.name, s.data, ok, nil
}

func (f *fakeSaves) DownloadSessionOutput(_ context.Context, id, _ string) ([]byte, bool, error) {
	data, ok := f.outputs[id]
	return data, ok, nil
}

func volumesService(t *testing.T, sessions ...*db.Session) (*Service, *fakeVolumes, *fakeSaves) {
	t.Helper()
	d, err := db.New(":memory:")
	if err != nil {
		t.Fatalf("new db: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	for _, sess := range sessions {
		if sess.CreatedAt.IsZero() {
			sess.CreatedAt = sess.UpdatedAt
		}
		if err := d.InsertSession(sess); err != nil {
			t.Fatalf("insert %s: %v", sess.SessionID, err)
		}
	}
	vols := &fakeVolumes{outputs: map[string][]byte{}, puts: map[string][][]byte{}}
	saves := &fakeSaves{saves: map[string]storedSave{}, outputs: map[string][]byte{}}
	return &Service{
		db:      d,
		volumes: vols,
		saves:   saves,
		cfg:     &config.Config{SessionVolumeRetention: 7 * 24 * time.Hour, VolumeCollectInterval: time.Hour},
		log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}, vols, saves
}

type tarFile struct {
	name string
	body string
	mod  time.Time
}

func outputTar(t *testing.T, files ...tarFile) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, f := range files {
		if err := tw.WriteHeader(&tar.Header{Name: "output/" + f.name, Mode: 0644, Size: int64(len(f.body)), ModTime: f.mod}); err != nil {
			t.Fatal(err)
		}
		_, _ = tw.Write([]byte(f.body))
	}
	_ = tw.Close()
	return buf.Bytes()
}

func outputZip(t *testing.T, names ...string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, n := range names {
		w, _ := zw.Create(n)
		_, _ = w.Write([]byte("x"))
	}
	_ = zw.Close()
	return buf.Bytes()
}

// tarNames lists the entries of a tar written to a volume.
func tarNames(t *testing.T, data []byte) []string {
	t.Helper()
	names := []string{}
	tr := tar.NewReader(bytes.NewReader(data))
	for {
		hdr, err := tr.Next()
		if err != nil {
			return names
		}
		names = append(names, hdr.Name)
	}
}

func TestLatestSave_takesTheMostRecentSave(t *testing.T) {
	now := time.Now()
	data := outputTar(t,
		tarFile{"AP_1.archipelago", "multi", now},
		tarFile{"old.apsave", "old", now.Add(-time.Hour)},
		tarFile{"AP_1.apsave", "new", now},
	)
	name, body, found, err := docker.LatestSaveFromTar(bytes.NewReader(data))
	if err != nil || !found || name != "AP_1.apsave" || string(body) != "new" {
		t.Fatalf("latestSave = %q %q %v %v", name, body, found, err)
	}
}

func TestArchiveSave_copiesTheSaveAndIgnoresAMissingVolume(t *testing.T) {
	svc, vols, saves := volumesService(t)
	vols.outputs["s1"] = outputTar(t, tarFile{"AP_1.apsave", "progress", time.Now()})

	if found, err := svc.archiveSave(context.Background(), "s1"); err != nil || !found {
		t.Fatalf("archiveSave s1 = %v %v", found, err)
	}
	if got := saves.saves["s1"]; got.name != "AP_1.apsave" || string(got.data) != "progress" {
		t.Fatalf("stored save = %+v", got)
	}
	if found, err := svc.archiveSave(context.Background(), "gone"); err != nil || found {
		t.Fatalf("archiveSave on a missing volume = %v %v", found, err)
	}
	if _, ok := vols.outputs["gone"]; ok {
		t.Fatal("archiving must not create a missing volume")
	}
}

// A stopped session past the retention and an orphan lose their volume, save copied first. Anything still in use,
// or stopped recently, keeps it.
func TestCollectVolumes_removesOnlyWhatNothingNeeds(t *testing.T) {
	now := time.Now().UTC()
	old := now.Add(-8 * 24 * time.Hour)
	svc, vols, saves := volumesService(t,
		&db.Session{SessionID: "old-stopped", Status: "stopped", UpdatedAt: old},
		&db.Session{SessionID: "recent-stopped", Status: "stopped", UpdatedAt: now.Add(-time.Hour)},
		&db.Session{SessionID: "old-running", Status: "running", UpdatedAt: old},
		&db.Session{SessionID: "old-crashed", Status: "crashed", UpdatedAt: old},
	)
	for _, id := range []string{"old-stopped", "recent-stopped", "old-running", "old-crashed", "orphan"} {
		vols.outputs[id] = outputTar(t, tarFile{"AP.apsave", id, now})
	}

	svc.collectVolumes(context.Background(), now)

	slices.Sort(vols.removed)
	if want := []string{"old-stopped", "orphan"}; !slices.Equal(vols.removed, want) {
		t.Fatalf("removed = %v, want %v", vols.removed, want)
	}
	for _, id := range []string{"old-stopped", "orphan"} {
		if string(saves.saves[id].data) != id {
			t.Errorf("save of %s not archived before removal", id)
		}
	}
}

func TestCollectVolumes_keepsAVolumeWhoseSaveCannotBeCopied(t *testing.T) {
	now := time.Now().UTC()
	svc, vols, saves := volumesService(t,
		&db.Session{SessionID: "s1", Status: "stopped", UpdatedAt: now.Add(-30 * 24 * time.Hour)},
	)
	vols.outputs["s1"] = outputTar(t, tarFile{"AP.apsave", "progress", now})
	saves.uploadErr = errors.New("minio down")

	svc.collectVolumes(context.Background(), now)

	if len(vols.removed) != 0 {
		t.Fatalf("removed %v although the save was not archived", vols.removed)
	}
}

func TestCollectVolumes_retentionZeroKeepsEverything(t *testing.T) {
	now := time.Now().UTC()
	svc, vols, _ := volumesService(t)
	svc.cfg.SessionVolumeRetention = 0
	vols.outputs["orphan"] = nil

	svc.collectVolumes(context.Background(), now)

	if len(vols.removed) != 0 {
		t.Fatalf("removed %v with the collection disabled", vols.removed)
	}
}

func TestCollectVolumesIfDue_runsAtMostOncePerInterval(t *testing.T) {
	now := time.Now().UTC()
	svc, vols, _ := volumesService(t)
	vols.outputs["a"] = nil
	svc.collectVolumesIfDue(context.Background(), now)
	vols.outputs["b"] = nil
	svc.collectVolumesIfDue(context.Background(), now.Add(10*time.Minute))
	if !slices.Equal(vols.removed, []string{"a"}) {
		t.Fatalf("removed = %v, want only the first pass", vols.removed)
	}
	svc.collectVolumesIfDue(context.Background(), now.Add(2*time.Hour))
	if !slices.Equal(vols.removed, []string{"a", "b"}) {
		t.Fatalf("removed = %v after the interval", vols.removed)
	}
}

func TestRestoreVolumeIfMissing_rebuildsOutputAndSave(t *testing.T) {
	svc, vols, saves := volumesService(t)
	saves.outputs["s1"] = outputZip(t, "AP_42.archipelago", "AP_42_Spoiler.txt")
	saves.saves["s1"] = storedSave{"AP_42.apsave", []byte("progress")}

	if err := svc.restoreVolumeIfMissing(context.Background(), "s1"); err != nil {
		t.Fatalf("restore: %v", err)
	}
	puts := vols.puts["s1"]
	if len(puts) != 2 {
		t.Fatalf("puts = %d, want output then save", len(puts))
	}
	if names := tarNames(t, puts[0]); !slices.Contains(names, "output/AP_42.archipelago") {
		t.Errorf("output tar = %v", names)
	}
	if names := tarNames(t, puts[1]); !slices.Equal(names, []string{"output/AP_42.apsave"}) {
		t.Errorf("save tar = %v", names)
	}
}

// A save imported by hand has no stored name: MultiServer's own, from the multidata, is used.
func TestRestoreVolumeIfMissing_namesAnUnnamedSaveAfterTheMultidata(t *testing.T) {
	svc, vols, saves := volumesService(t)
	saves.outputs["s1"] = outputZip(t, "AP_42.archipelago")
	saves.saves["s1"] = storedSave{"", []byte("progress")}

	if err := svc.restoreVolumeIfMissing(context.Background(), "s1"); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if names := tarNames(t, vols.puts["s1"][1]); !slices.Equal(names, []string{"output/AP_42.apsave"}) {
		t.Errorf("save tar = %v", names)
	}
}

func TestRestoreVolumeIfMissing_leavesAnExistingVolumeAlone(t *testing.T) {
	svc, vols, saves := volumesService(t)
	vols.outputs["s1"] = outputTar(t)
	saves.outputs["s1"] = outputZip(t, "AP_42.archipelago")

	if err := svc.restoreVolumeIfMissing(context.Background(), "s1"); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if len(vols.puts["s1"]) != 0 {
		t.Fatal("an existing volume was written to")
	}
}

func TestRestoreVolumeIfMissing_failsWithoutStoredOutput(t *testing.T) {
	svc, _, _ := volumesService(t)
	if err := svc.restoreVolumeIfMissing(context.Background(), "s1"); !errors.Is(err, errOutputNotStored) {
		t.Fatalf("restore = %v, want errOutputNotStored", err)
	}
}

// A relaunch that cannot rebuild its volume fails before touching the session: it stays stopped.
func TestRelaunchFromSave_failsCleanlyWhenTheVolumeCannotBeRebuilt(t *testing.T) {
	out := "AP_1.archipelago"
	svc, _, _ := volumesService(t,
		&db.Session{SessionID: "s1", Status: "stopped", OutputFile: &out, UpdatedAt: time.Now().UTC()},
	)
	if err := svc.RelaunchFromSave(context.Background(), "s1"); !errors.Is(err, errOutputNotStored) {
		t.Fatalf("relaunch = %v, want errOutputNotStored", err)
	}
	sess, _ := svc.db.GetSession("s1")
	if sess.Status != "stopped" {
		t.Fatalf("status = %q, want stopped", sess.Status)
	}
}
