package service

import "testing"

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
