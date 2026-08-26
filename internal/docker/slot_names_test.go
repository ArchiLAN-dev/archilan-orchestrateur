package docker

import "testing"

// Story 16.18: our own generated seeds carry an injected observer slot the bridge attaches to by
// name. A seed generated elsewhere has none, so the bridge is told the roster instead of assuming
// one is waiting for it - and an empty roster has to keep the historical behaviour, not break it.
func TestMarshalSlotNames(t *testing.T) {
	if got := marshalSlotNames(nil); got != "[]" {
		t.Fatalf("nil roster: got %q, want []", got)
	}
	if got := marshalSlotNames([]SlotName{}); got != "[]" {
		t.Fatalf("empty roster: got %q, want []", got)
	}

	got := marshalSlotNames([]SlotName{
		{Name: "Bridge", Game: "Archipelago"},
		{Name: "masterkafey_SWH", Game: "Sayonara Wild Hearts"},
	})
	want := `[{"name":"Bridge","game":"Archipelago"},{"name":"masterkafey_SWH","game":"Sayonara Wild Hearts"}]`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
