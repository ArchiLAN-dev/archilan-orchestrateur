package docker

import "testing"

// Story 56.6: the progression cap reaches the generation container as an environment variable, only when asked.
func TestGenerateEnvCarriesTheProgressionCapOnlyWhenSet(t *testing.T) {
	if env := generateEnv(GenerateOptions{}); env != nil {
		t.Fatalf("env = %v, want nil", env)
	}
	k := 2.0
	env := generateEnv(GenerateOptions{ProgressionCap: &k})
	if len(env) != 1 || env[0] != "ARCHILAN_PROGRESSION_CAP=2" {
		t.Fatalf("env = %v", env)
	}
	k = 2.5
	if env := generateEnv(GenerateOptions{ProgressionCap: &k}); env[0] != "ARCHILAN_PROGRESSION_CAP=2.5" {
		t.Fatalf("env = %v", env)
	}
}
