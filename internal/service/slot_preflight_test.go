package service

import (
	"testing"
	"time"
)

func newTestServiceForSlotPreflight() *Service {
	return &Service{
		preflightSem:   make(chan struct{}, 1),
		slotPreflights: map[string]*SlotPreflight{},
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
