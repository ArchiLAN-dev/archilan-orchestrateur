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

// Story 9.51: an OptionDict whose world declares a `schema` also carries, per sub-setting,
// the values it accepts. A world that declares none carries nothing - the absence is the
// signal the editor needs to keep offering a free text field.
func TestOptionTypeOverride_ParsesDictSubOptionValues(t *testing.T) {
	raw := []byte(`{"options":{
		"game_options":{"type":"dict","validKeys":["battle_style","player_name"],
			"keys":{"battle_style":{"values":["shift","set"]}}},
		"other_options":{"type":"dict","validKeys":["a"]}
	}}`)

	var parsed struct {
		Options map[string]OptionTypeOverride `json:"options"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	declared := parsed.Options["game_options"]
	sub, ok := declared.Keys["battle_style"]
	if !ok {
		t.Fatalf("battle_style missing from keys: %+v", declared.Keys)
	}
	if got := sub.Values; len(got) != 2 || got[0] != "shift" || got[1] != "set" {
		t.Errorf("values = %v, want [shift set]", got)
	}
	// A sub-setting the schema says nothing about must not appear at all: an empty entry
	// would read as "declared, and empty", which is a different claim.
	if _, ok := declared.Keys["player_name"]; ok {
		t.Errorf("player_name should be absent, got %+v", declared.Keys)
	}

	if undeclared := parsed.Options["other_options"]; undeclared.Keys != nil {
		t.Errorf("a dict without a schema should carry no keys, got %+v", undeclared.Keys)
	}
}
