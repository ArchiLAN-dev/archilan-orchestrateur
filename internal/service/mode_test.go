package service

import "testing"

func ptr[T any](v T) *T { return &v }

func TestValidateServerOptions(t *testing.T) {
	// All-empty/nil request is valid (everything falls back to launch-script defaults).
	if !validateServerOptions(LaunchRequest{}) {
		t.Errorf("empty LaunchRequest should be valid")
	}

	valid := []LaunchRequest{
		{ReleaseMode: "goal", CollectMode: "disabled"},
		{RemainingMode: "goal", CountdownMode: "auto"},
		{HintCost: ptr(0)}, {HintCost: ptr(100)},
		{LocationCheckPoints: ptr(0)}, {AutoShutdown: ptr(3600)},
		{Compatibility: ptr(0)}, {Compatibility: ptr(2)},
		{DisableItemCheat: ptr(true)},
	}
	for i, req := range valid {
		if !validateServerOptions(req) {
			t.Errorf("valid[%d] %+v should be valid", i, req)
		}
	}

	invalid := []LaunchRequest{
		{ReleaseMode: "nope"},
		{RemainingMode: "auto"}, // not a remaining value
		{CountdownMode: "goal"}, // not a countdown value
		{HintCost: ptr(-1)}, {HintCost: ptr(101)},
		{LocationCheckPoints: ptr(-1)}, {AutoShutdown: ptr(-1)},
		{Compatibility: ptr(3)}, {Compatibility: ptr(-1)},
	}
	for i, req := range invalid {
		if validateServerOptions(req) {
			t.Errorf("invalid[%d] %+v should be rejected", i, req)
		}
	}
}

func TestValidateGenerationOptions(t *testing.T) {
	valid := []GenerateRequest{
		{},
		{PlandoOptions: []string{"bosses", "items", "texts", "connections"}},
		{Spoiler: ptr(0)}, {Spoiler: ptr(3)},
		{Race: ptr(true)},
	}
	for i, req := range valid {
		if !validateGenerationOptions(req) {
			t.Errorf("valid[%d] %+v should be valid", i, req)
		}
	}

	invalid := []GenerateRequest{
		{PlandoOptions: []string{"bosses", "nope"}},
		{Spoiler: ptr(-1)}, {Spoiler: ptr(4)},
	}
	for i, req := range invalid {
		if validateGenerationOptions(req) {
			t.Errorf("invalid[%d] %+v should be rejected", i, req)
		}
	}
}

func TestValidAPMode(t *testing.T) {
	valid := []string{"", "disabled", "enabled", "goal", "auto", "auto-enabled"}
	for _, m := range valid {
		if !validAPMode(m) {
			t.Errorf("validAPMode(%q) = false, want true", m)
		}
	}

	invalid := []string{"Disabled", "off", "on", "auto_enabled", "release", "true", "0"}
	for _, m := range invalid {
		if validAPMode(m) {
			t.Errorf("validAPMode(%q) = true, want false", m)
		}
	}
}
