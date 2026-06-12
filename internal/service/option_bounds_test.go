package service

import (
	"encoding/json"
	"testing"
)

// Story 9.25: the introspection types.json now carries authoritative range
// bounds (min/max/default); OptionTypeOverride must parse them so the options
// endpoint can override the template-parsed values.
func TestOptionTypeOverride_ParsesRangeBounds(t *testing.T) {
	raw := []byte(`{"options":{
		"song_difficulty_min":{"type":"range","min":1,"max":11,"default":4},
		"grade_needed":{"type":"choice"},
		"start_inventory":{"type":"weights","defaultWeights":{"Foo":1}}
	}}`)

	var parsed struct {
		Options map[string]OptionTypeOverride `json:"options"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	rng := parsed.Options["song_difficulty_min"]
	if rng.Min == nil || *rng.Min != 1 {
		t.Errorf("min = %v, want 1", rng.Min)
	}
	if rng.Max == nil || *rng.Max != 11 {
		t.Errorf("max = %v, want 11", rng.Max)
	}
	if rng.Default == nil || *rng.Default != 4 {
		t.Errorf("default = %v, want 4", rng.Default)
	}

	// Non-range options carry no bounds.
	if c := parsed.Options["grade_needed"]; c.Min != nil || c.Max != nil || c.Default != nil {
		t.Errorf("choice option should have no bounds, got min=%v max=%v default=%v", c.Min, c.Max, c.Default)
	}
}
