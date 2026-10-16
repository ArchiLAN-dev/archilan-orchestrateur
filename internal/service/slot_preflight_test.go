package service

import (
	"errors"
	"testing"
	"time"
)

func newTestServiceForSlotPreflight() *Service {
	return &Service{
		preflightSem:   make(chan struct{}, 1),
		slotPreflights: map[string]*SlotPreflight{},
		requiredFiles:  newRequiredFileStore(1 << 20),
	}
}

func TestGetSlotPreflight_unknownID(t *testing.T) {
	s := newTestServiceForSlotPreflight()
	if _, ok := s.GetSlotPreflight("nope"); ok {
		t.Error("expected ok=false for unknown id")
	}
}

func TestFinishSlotPreflight_updatesJob(t *testing.T) {
	s := newTestServiceForSlotPreflight()
	s.slotPreflights["job1"] = &SlotPreflight{Status: SlotPreflightPending, CreatedAt: time.Now()}

	s.finishSlotPreflight("job1", SlotPreflightFailed, "Exception: boom")

	job, ok := s.GetSlotPreflight("job1")
	if !ok {
		t.Fatal("expected job to exist")
	}
	if job.Status != SlotPreflightFailed || job.Error != "Exception: boom" {
		t.Errorf("unexpected job state: %+v", job)
	}
}

func TestFinishSlotPreflight_unknownIDIsNoop(t *testing.T) {
	s := newTestServiceForSlotPreflight()
	s.finishSlotPreflight("ghost", SlotPreflightPassed, "")
	if len(s.slotPreflights) != 0 {
		t.Error("finishing an unknown job must not create it")
	}
}

func TestPruneSlotPreflights_dropsExpiredKeepsFresh(t *testing.T) {
	s := newTestServiceForSlotPreflight()
	now := time.Now()
	s.slotPreflights["old"] = &SlotPreflight{Status: SlotPreflightPassed, CreatedAt: now.Add(-slotPreflightTTL - time.Minute)}
	s.slotPreflights["fresh"] = &SlotPreflight{Status: SlotPreflightPending, CreatedAt: now}

	s.slotPreflightMu.Lock()
	s.pruneSlotPreflightsLocked(now)
	s.slotPreflightMu.Unlock()

	if _, ok := s.slotPreflights["old"]; ok {
		t.Error("expired job should be pruned")
	}
	if _, ok := s.slotPreflights["fresh"]; !ok {
		t.Error("fresh job should be kept")
	}
}

// Story 44.6: an admin test with a file that is no longer held is refused before any job exists, so the
// API can say "send it again" instead of reporting a failure about a missing ROM.
func TestStartSlotPreflight_refusesAnUnknownFile(t *testing.T) {
	s := newTestServiceForSlotPreflight()

	_, err := s.StartSlotPreflight([]byte("game: X"), "", []string{"gone"})

	if !errors.Is(err, ErrRequiredFileExpired) {
		t.Fatalf("err = %v, want ErrRequiredFileExpired", err)
	}
	if len(s.slotPreflights) != 0 {
		t.Error("a refused test must not create a job")
	}
}

func TestTakeRequiredFiles_consumesTheStagedFiles(t *testing.T) {
	s := newTestServiceForSlotPreflight()
	id, _, err := s.StageRequiredFile("DK3Settings.rom_file", "dkc3.sfc", []byte("rom"))
	if err != nil {
		t.Fatalf("stage: %v", err)
	}

	files, _, err := s.takeRequiredFiles([]string{id})
	if err != nil || len(files) != 1 || files[0].Setting != "DK3Settings.rom_file" {
		t.Fatalf("files = %+v, err = %v", files, err)
	}
	if _, _, err := s.takeRequiredFiles([]string{id}); !errors.Is(err, ErrRequiredFileExpired) {
		t.Errorf("a file is used once, got err = %v", err)
	}
}

func TestTakeRequiredFiles_withoutIDsTakesNothing(t *testing.T) {
	s := newTestServiceForSlotPreflight()

	files, _, err := s.takeRequiredFiles(nil)

	if err != nil || files != nil {
		t.Errorf("files = %+v, err = %v", files, err)
	}
}

// Review of story 44.6: a file taken for a test keeps counting against the store's cap until the test
// is over, so tests waiting for a container cannot pile files up in memory past the cap.
func TestATakenFileCountsUntilItsTestIsOver(t *testing.T) {
	s := newTestServiceForSlotPreflight()
	s.requiredFiles = newRequiredFileStore(10)
	id, _, err := s.StageRequiredFile("DK3Settings.rom_file", "dkc3.sfc", []byte("12345678"))
	if err != nil {
		t.Fatalf("stage: %v", err)
	}

	_, done, err := s.takeRequiredFiles([]string{id})
	if err != nil {
		t.Fatalf("take: %v", err)
	}
	if _, _, err := s.StageRequiredFile("DK3Settings.rom_file", "dkc3.sfc", []byte("12345678")); !errors.Is(err, ErrRequiredFilesFull) {
		t.Fatalf("while the test runs the cap still holds, got %v", err)
	}

	done()
	if _, _, err := s.StageRequiredFile("DK3Settings.rom_file", "dkc3.sfc", []byte("12345678")); err != nil {
		t.Fatalf("once the test is over the room is back, got %v", err)
	}
}

func TestTheSameFileTwiceIsAnInvalidRequest(t *testing.T) {
	s := newTestServiceForSlotPreflight()
	id, _, _ := s.StageRequiredFile("DK3Settings.rom_file", "dkc3.sfc", []byte("rom"))

	if _, _, err := s.takeRequiredFiles([]string{id, id}); !errors.Is(err, ErrDuplicateRequiredFile) {
		t.Fatalf("err = %v, want ErrDuplicateRequiredFile", err)
	}
	if _, _, err := s.takeRequiredFiles([]string{id}); err != nil {
		t.Errorf("a refused request must not consume the file, got %v", err)
	}
}
