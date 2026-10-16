package service

import (
	"bytes"
	"errors"
	"testing"
	"time"
)

// Story 44.2: the files a world reads at generation are held in memory only, used once, and dropped
// after 30 minutes.

func TestRequiredFileStore_aStagedFileIsTakenOnceThenGone(t *testing.T) {
	st := newRequiredFileStore(1 << 20)
	now := time.Now()
	id, expiresAt, err := st.stage("DK3Settings.rom_file", "dkc3.sfc", []byte("rom"), now)
	if err != nil {
		t.Fatalf("stage: %v", err)
	}
	if !expiresAt.Equal(now.Add(requiredFileTTL)) {
		t.Errorf("expiresAt = %v, want now + 30 min", expiresAt)
	}

	files, err := st.take([]string{id}, now)
	if err != nil {
		t.Fatalf("take: %v", err)
	}
	if len(files) != 1 || files[0].Setting != "DK3Settings.rom_file" || files[0].Filename != "dkc3.sfc" || !bytes.Equal(files[0].Data, []byte("rom")) {
		t.Fatalf("unexpected files %+v", files)
	}
	if _, err := st.take([]string{id}, now); !errors.Is(err, ErrRequiredFileExpired) {
		t.Fatalf("second take: err = %v, want ErrRequiredFileExpired", err)
	}
	// The room stays counted until its run is over (review of story 44.6).
	if st.size != 3 {
		t.Errorf("size = %d after take, want 3 until free", st.size)
	}
	st.free(files)
	if st.size != 0 {
		t.Errorf("size = %d after free, want 0", st.size)
	}
}

func TestRequiredFileStore_takesAllOrNone(t *testing.T) {
	st := newRequiredFileStore(1 << 20)
	now := time.Now()
	id, _, _ := st.stage("DK3Settings.rom_file", "dkc3.sfc", []byte("rom"), now)

	if _, err := st.take([]string{id, "unknown"}, now); !errors.Is(err, ErrRequiredFileExpired) {
		t.Fatalf("err = %v, want ErrRequiredFileExpired", err)
	}
	if !st.has([]string{id}, now) {
		t.Fatal("a failed take consumed the valid file")
	}
	if _, err := st.take([]string{id, id}, now); !errors.Is(err, ErrDuplicateRequiredFile) {
		t.Fatalf("the same id twice: err = %v, want ErrDuplicateRequiredFile", err)
	}
}

func TestRequiredFileStore_anExpiredFileIsDropped(t *testing.T) {
	st := newRequiredFileStore(1 << 20)
	now := time.Now()
	id, _, _ := st.stage("DK3Settings.rom_file", "dkc3.sfc", []byte("rom"), now)

	st.prune(now.Add(requiredFileTTL))

	if st.has([]string{id}, now) || st.size != 0 {
		t.Fatalf("expired file still held (size %d)", st.size)
	}
}

func TestRequiredFileStore_refusesBeyondItsTotalCap(t *testing.T) {
	st := newRequiredFileStore(5)
	now := time.Now()
	if _, _, err := st.stage("A.rom", "a", []byte("abc"), now); err != nil {
		t.Fatalf("stage: %v", err)
	}
	if _, _, err := st.stage("B.rom", "b", []byte("abc"), now); !errors.Is(err, ErrRequiredFilesFull) {
		t.Fatalf("err = %v, want ErrRequiredFilesFull", err)
	}
	if st.size != 3 {
		t.Errorf("size = %d, want 3: a refused file is not held", st.size)
	}
}

func TestRequiredFileStore_releaseDropsTheFile(t *testing.T) {
	st := newRequiredFileStore(1 << 20)
	now := time.Now()
	id, _, _ := st.stage("A.rom", "a", []byte("abc"), now)

	st.release(id)
	st.release(id)

	if st.has([]string{id}, now) || st.size != 0 {
		t.Fatal("released file still held")
	}
}

func TestRequiredFileStore_validatesTheSettingAndTheSize(t *testing.T) {
	st := newRequiredFileStore(1 << 30)
	now := time.Now()
	for _, setting := range []string{"", "rom_file", "A.b.c", "A.b=/x"} {
		if _, _, err := st.stage(setting, "a", []byte("x"), now); !errors.Is(err, ErrInvalidRequiredFile) {
			t.Errorf("setting %q: err = %v, want ErrInvalidRequiredFile", setting, err)
		}
	}
	if _, _, err := st.stage("A.rom", "a", nil, now); !errors.Is(err, ErrInvalidRequiredFile) {
		t.Errorf("empty file: err = %v, want ErrInvalidRequiredFile", err)
	}
	if _, _, err := st.stage("A.rom", "a", make([]byte, RequiredFileMaxBytes+1), now); !errors.Is(err, ErrInvalidRequiredFile) {
		t.Errorf("oversized file: err = %v, want ErrInvalidRequiredFile", err)
	}
	if _, err := st.take(make([]string, MaxRequiredFilesPerGeneration+1), now); !errors.Is(err, ErrTooManyRequiredFiles) {
		t.Errorf("too many ids: err = %v, want ErrTooManyRequiredFiles", err)
	}
}

func TestSanitizeRequiredFilename(t *testing.T) {
	cases := map[string]string{
		"Donkey Kong Country 3 (USA) (En,Fr).sfc": "Donkey Kong Country 3 (USA) (En_Fr).sfc",
		"../../etc/passwd":                        "passwd",
		`C:\roms\oot.z64`:                         "oot.z64",
		"":                                        "file",
		"..":                                      "file",
	}
	for in, want := range cases {
		if got := sanitizeRequiredFilename(in); got != want {
			t.Errorf("sanitize(%q) = %q, want %q", in, got, want)
		}
	}
}
