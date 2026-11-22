package service

import "testing"

// Story 56.6: the cap is off when absent, and a factor that caps nothing or everything is refused.
func TestValidateGenerationOptionsBoundsTheProgressionCap(t *testing.T) {
	for k, want := range map[float64]bool{0.5: false, 1: false, 1.5: true, 2: true, 10: true, 10.5: false} {
		v := k
		if got := validateGenerationOptions(GenerateRequest{ProgressionCap: &v}); got != want {
			t.Errorf("cap %v: valid = %v, want %v", k, got, want)
		}
	}
	if !validateGenerationOptions(GenerateRequest{}) {
		t.Error("no cap must be valid")
	}
}
