package service

import (
	"testing"
)

// Story 9.56: the bridge attaches to the first real slot of the multiworld, read from the generated
// archive - in slot order, item-link groups left out (nothing can connect as a group).
func TestRosterFromMultidata_slotOrderWithoutGroups(t *testing.T) {
	raw := []byte(`{"seedName":"s","slots":[
		{"slot":3,"name":"Group","game":"A Link to the Past","type":2},
		{"slot":2,"name":"Bob","game":"TUNIC","type":1},
		{"slot":1,"name":"Alice","game":"Minecraft","type":1}]}`)

	roster, err := rosterFromMultidata(raw)

	if err != nil {
		t.Fatalf("roster: %v", err)
	}
	want := []SlotName{{Name: "Alice", Game: "Minecraft"}, {Name: "Bob", Game: "TUNIC"}}
	if len(roster) != len(want) || roster[0] != want[0] || roster[1] != want[1] {
		t.Fatalf("roster = %+v, want %+v", roster, want)
	}
}

// A session generated before story 9.56 still has its injected observer as slot 1: the bridge keeps
// attaching to it (AC6).
func TestRosterFromMultidata_aSessionGeneratedBeforeKeepsItsObserverFirst(t *testing.T) {
	raw := []byte(`{"seedName":"s","slots":[
		{"slot":1,"name":"Bridge","game":"Archipelago","type":0},
		{"slot":2,"name":"Alice","game":"Minecraft","type":1}]}`)

	roster, _ := rosterFromMultidata(raw)

	if len(roster) != 2 || roster[0].Name != "Bridge" {
		t.Fatalf("roster = %+v, want the observer first", roster)
	}
}

func TestRosterFromMultidata_refusesAnUnreadableTable(t *testing.T) {
	if _, err := rosterFromMultidata([]byte(`{"slots":[]}`)); err == nil {
		t.Error("an empty roster must be an error, not a silent fallback")
	}
	if _, err := rosterFromMultidata([]byte(`nope`)); err == nil {
		t.Error("an unreadable table must be an error")
	}
}

// A launch from an archive keeps staging the observer yaml only for a multiworld generated with it.
func TestHasObserverSlot(t *testing.T) {
	if !hasObserverSlot([]SlotName{{Name: "Bridge", Game: "Archipelago"}, {Name: "Alice", Game: "Minecraft"}}) {
		t.Error("a multiworld generated before story 9.56 has its observer first")
	}
	if hasObserverSlot([]SlotName{{Name: "Alice", Game: "Minecraft"}}) {
		t.Error("a multiworld generated after has none")
	}
	if hasObserverSlot([]SlotName{{Name: "Bridge", Game: "Minecraft"}}) {
		t.Error("a player named Bridge is a player")
	}
}
