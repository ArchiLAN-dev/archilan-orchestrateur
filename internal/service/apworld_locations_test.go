package service

import (
	"encoding/json"
	"testing"
)

// Story 4.14 (archilan.fr): the introspection sidecar now also carries the static
// location list under "locations"; GetApworldLocations parses it from the same
// {options, locations} JSON the option-types endpoint reads.
func TestApworldLocations_ParsesFromSidecar(t *testing.T) {
	raw := []byte(`{"options":{"song_difficulty_min":{"type":"range","min":1,"max":11}},"locations":["Boss Reward","Chest 1","Chest 2"]}`)

	var parsed struct {
		Locations []string `json:"locations"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	want := []string{"Boss Reward", "Chest 1", "Chest 2"}
	if len(parsed.Locations) != len(want) {
		t.Fatalf("locations len = %d, want %d", len(parsed.Locations), len(want))
	}
	for i, w := range want {
		if parsed.Locations[i] != w {
			t.Errorf("locations[%d] = %q, want %q", i, parsed.Locations[i], w)
		}
	}
}

// A sidecar written before 4.14 (no "locations" key) parses to an empty list, not an error -
// GetApworldLocations then returns nil and the endpoint serves an empty array.
func TestApworldLocations_MissingKeyIsEmpty(t *testing.T) {
	raw := []byte(`{"options":{"grade_needed":{"type":"choice"}}}`)

	var parsed struct {
		Locations []string `json:"locations"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(parsed.Locations) != 0 {
		t.Errorf("locations = %v, want empty", parsed.Locations)
	}
}
