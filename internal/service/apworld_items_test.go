package service

import (
	"reflect"
	"testing"
)

// Story 9.58 (archilan.fr): the introspection sidecar also carries the item names and groups.
func TestApworldItems_ParsesItemsAndGroupsFromSidecar(t *testing.T) {
	raw := []byte(`{"options":{},"locations":["Chest"],"items":["Dream_Nail","Mantis_Claw"],"itemGroups":{"Skills":["Dream_Nail","Mantis_Claw"]}}`)

	got := parseApworldItems(raw)

	want := ApworldItems{Items: []string{"Dream_Nail", "Mantis_Claw"}, Groups: map[string][]string{"Skills": {"Dream_Nail", "Mantis_Claw"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("items = %+v, want %+v", got, want)
	}
}

// A sidecar written by an image before 9.58 has neither key: empty lists, never nil, so the
// endpoint serves [] and {} rather than null.
func TestApworldItems_OldSidecarIsEmptyNotNil(t *testing.T) {
	got := parseApworldItems([]byte(`{"options":{"grade_needed":{"type":"choice"}},"locations":["Chest"]}`))

	if got.Items == nil || len(got.Items) != 0 || got.Groups == nil || len(got.Groups) != 0 {
		t.Fatalf("items = %+v, want empty non-nil", got)
	}
}

func TestApworldItems_GarbageIsEmpty(t *testing.T) {
	got := parseApworldItems([]byte(`not json`))

	if got.Items == nil || got.Groups == nil {
		t.Fatalf("items = %+v, want empty non-nil", got)
	}
}
