package service

import (
	"errors"
	"reflect"
	"testing"
)

// Story 4.14 (archilan.fr): the introspection sidecar also carries the static location list under "locations".
func TestApworldLocations_ParsesFromSidecar(t *testing.T) {
	got, err := parseApworldLocations([]byte(`{"options":{"song_difficulty_min":{"type":"range","min":1,"max":11}},"locations":["Boss Reward","Chest 1","Chest 2"]}`))

	if err != nil || !reflect.DeepEqual(got, []string{"Boss Reward", "Chest 1", "Chest 2"}) {
		t.Fatalf("got %v, %v", got, err)
	}
}

// A sidecar written before 4.14 (no "locations" key), or a world that declares none (Minecraft Modded, story
// 38.31): an empty list, not an error.
func TestApworldLocations_MissingKeyIsEmpty(t *testing.T) {
	got, err := parseApworldLocations([]byte(`{"options":{"grade_needed":{"type":"choice"}}}`))

	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("got %#v, %v", got, err)
	}
}

// Story 38.31: an unreadable sidecar is an error, never a world without locations.
func TestApworldLocations_UnreadableSidecarIsAnError(t *testing.T) {
	_, err := parseApworldLocations([]byte("C compression module not found.\n{\"options\":{}}"))

	if !errors.Is(err, ErrIntrospectionUnreadable) {
		t.Fatalf("err = %v, want ErrIntrospectionUnreadable", err)
	}
}
