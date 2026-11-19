package service

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"
)

// Story 17.37: a session volume lives only as long as it is useful.
//
// The volume holds the generated output (also in MinIO since the generation) and the Archipelago save, which
// exists nowhere else. So the save is copied to MinIO each time a game stops (stop, idle via auto_shutdown, delete),
// a volume left unused past the retention is removed, and a relaunch on a removed volume rebuilds it from MinIO:
// output, yamls and worlds, then the save. Nobody has to decide whether a game is "over" - a stopped session stays
// resumable, only its volume goes away.

// sessionVolumes is what the volume lifecycle needs from Docker; the docker client in prod.
type sessionVolumes interface {
	VolumeExists(ctx context.Context, sessionID string) (bool, error)
	ListSessionVolumes(ctx context.Context) ([]string, error)
	LatestSaveFromVolume(ctx context.Context, sessionID string) (name string, data []byte, found bool, err error)
	PutDataToVolume(ctx context.Context, sessionID string, tarData io.Reader) error
	RemoveVolume(ctx context.Context, sessionID string) error
}

// sessionSaves is what it needs from durable storage; the MinIO client in prod.
type sessionSaves interface {
	UploadSessionSave(ctx context.Context, sessionID, filename string, data []byte) error
	DownloadSessionSave(ctx context.Context, sessionID string) (filename string, data []byte, found bool, err error)
	DownloadSessionOutput(ctx context.Context, sessionID, filename string) (data []byte, found bool, err error)
}

// sessionOutputArchive is the name runGeneration stores the whole output under.
const sessionOutputArchive = "archive.zip"

// volumeCollectBatch bounds the volumes one collection pass removes: each one copies its output first.
const volumeCollectBatch = 50

var errOutputNotStored = errors.New("generated output not in storage")

// archiveSave copies the latest save of the session's volume to storage. found is false when the volume holds no
// save (a game never played, or a volume already gone): nothing is lost then.
func (s *Service) archiveSave(ctx context.Context, sessionID string) (found bool, err error) {
	if s.volumes == nil || s.saves == nil {
		return false, nil
	}
	exists, err := s.volumes.VolumeExists(ctx, sessionID)
	if err != nil {
		return false, fmt.Errorf("check volume: %w", err)
	}
	if !exists {
		return false, nil
	}
	name, data, found, err := s.volumes.LatestSaveFromVolume(ctx, sessionID)
	if err != nil {
		return false, fmt.Errorf("read save: %w", err)
	}
	if !found {
		return false, nil
	}
	if err := s.saves.UploadSessionSave(ctx, sessionID, name, data); err != nil {
		return false, fmt.Errorf("upload save: %w", err)
	}
	return true, nil
}

// archiveSaveBestEffort is archiveSave on a stop path, which a storage failure must not block: the volume is kept
// anyway, and the collection retries before removing it.
func (s *Service) archiveSaveBestEffort(ctx context.Context, sessionID string) {
	if _, err := s.archiveSave(ctx, sessionID); err != nil {
		s.log.Warn("volumes: archive save failed", "session_id", sessionID, "err", err)
	}
}

// restoreVolumeIfMissing rebuilds a removed volume before a relaunch: the generated output, the yamls and worlds
// the tooling reads, then the save. A volume still there is left alone.
func (s *Service) restoreVolumeIfMissing(ctx context.Context, sessionID string) error {
	if s.volumes == nil || s.saves == nil {
		return nil
	}
	exists, err := s.volumes.VolumeExists(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("check volume: %w", err)
	}
	if exists {
		return nil
	}
	s.log.Info("volumes: rebuilding a removed volume from storage", "session_id", sessionID)

	archive, found, err := s.saves.DownloadSessionOutput(ctx, sessionID, sessionOutputArchive)
	if err != nil {
		return fmt.Errorf("download output: %w", err)
	}
	if !found {
		return errOutputNotStored
	}
	outputTar, err := zipToOutputTar(archive)
	if err != nil {
		return fmt.Errorf("unzip output: %w", err)
	}
	if err := s.volumes.PutDataToVolume(ctx, sessionID, bytes.NewReader(outputTar)); err != nil {
		return fmt.Errorf("restore output: %w", err)
	}
	s.stageSessionDataBestEffort(ctx, sessionID)
	return s.restoreSave(ctx, sessionID, archive)
}

// stageSessionDataBestEffort puts the yamls and worlds back: the reachability tooling reads them, the AP server
// does not, so a failure leaves the game playable.
func (s *Service) stageSessionDataBestEffort(ctx context.Context, sessionID string) {
	if s.storage == nil {
		return
	}
	dataTar, n, err := s.buildDataTar(ctx, sessionID)
	if err == nil && n > 0 {
		err = s.volumes.PutDataToVolume(ctx, sessionID, dataTar)
	}
	if err != nil {
		s.log.Warn("volumes: restage yamls and worlds failed", "session_id", sessionID, "err", err)
	}
}

// restoreSave puts the stored save back into /data/output, under the name MultiServer loads: its own name, or
// for a save stored without one (imported by hand) the multidata's name with .apsave.
func (s *Service) restoreSave(ctx context.Context, sessionID string, outputArchive []byte) error {
	name, data, found, err := s.saves.DownloadSessionSave(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("download save: %w", err)
	}
	if !found {
		return nil
	}
	if name == "" {
		name = saveNameFromOutput(outputArchive)
	}
	if name == "" {
		return fmt.Errorf("restore save: no multidata in the output to name it after")
	}
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{Name: "output/" + path.Base(name), Mode: 0644, Size: int64(len(data))}); err != nil {
		return err
	}
	if _, err := tw.Write(data); err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}
	if err := s.volumes.PutDataToVolume(ctx, sessionID, &buf); err != nil {
		return fmt.Errorf("restore save: %w", err)
	}
	return nil
}

// saveNameFromOutput is MultiServer's default save name: the multidata's, with .apsave.
func saveNameFromOutput(outputArchive []byte) string {
	zr, err := zip.NewReader(bytes.NewReader(outputArchive), int64(len(outputArchive)))
	if err != nil {
		return ""
	}
	for _, f := range zr.File {
		base := path.Base(f.Name)
		if strings.HasSuffix(strings.ToLower(base), ".archipelago") {
			return strings.TrimSuffix(base, path.Ext(base)) + ".apsave"
		}
	}
	return ""
}

// collectVolumes removes the volumes nothing needs on the host any more: a stopped session's past the retention,
// and an orphan's (no session record). Each save is copied to storage first; a volume whose save cannot be copied
// stays. A volume a container still mounts (a crashed session keeps its containers) is refused by Docker and stays.
func (s *Service) collectVolumes(ctx context.Context, now time.Time) {
	if s.volumes == nil || s.saves == nil || s.cfg.SessionVolumeRetention <= 0 {
		return
	}
	ids, err := s.volumes.ListSessionVolumes(ctx)
	if err != nil {
		s.log.Error("volumes: list failed", "err", err)
		return
	}
	removed := 0
	for _, id := range ids {
		if removed >= volumeCollectBatch {
			return
		}
		sess, err := s.db.GetSession(id)
		if err != nil {
			s.log.Error("volumes: get session failed", "session_id", id, "err", err)
			continue
		}
		if sess != nil && (sess.Status != "stopped" || now.Sub(sess.UpdatedAt) < s.cfg.SessionVolumeRetention) {
			continue
		}
		if _, err := s.archiveSave(ctx, id); err != nil {
			s.log.Warn("volumes: save not archived, volume kept", "session_id", id, "err", err)
			continue
		}
		if err := s.volumes.RemoveVolume(ctx, id); err != nil {
			s.log.Warn("volumes: remove failed", "session_id", id, "err", err)
			continue
		}
		removed++
		s.log.Info("volumes: removed", "session_id", id, "orphan", sess == nil)
	}
}

// collectVolumesIfDue runs collectVolumes at most once per VolumeCollectInterval, from the sweeper.
func (s *Service) collectVolumesIfDue(ctx context.Context, now time.Time) {
	if !s.lastVolumeCollect.IsZero() && now.Sub(s.lastVolumeCollect) < s.cfg.VolumeCollectInterval {
		return
	}
	s.lastVolumeCollect = now
	s.collectVolumes(ctx, now)
}
