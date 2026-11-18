package docker

import (
	"reflect"
	"testing"
)

func TestBridgeErrorReportingEnvIsEmptyWithoutADSN(t *testing.T) {
	if env := bridgeErrorReportingEnv("", "ghcr.io/archilan-dev/bridge:0.18.0"); env != nil {
		t.Fatalf("no DSN must add nothing, got %v", env)
	}
	if env := bridgeErrorReportingEnv("  ", "bridge:1"); env != nil {
		t.Fatalf("a blank DSN must add nothing, got %v", env)
	}
}

func TestBridgeErrorReportingEnvCarriesTheDSNAndTheImageTag(t *testing.T) {
	cases := map[string][]string{
		"ghcr.io/archilan-dev/bridge:0.18.0": {"SENTRY_DSN=http://k@bugsink:8000/3", "SENTRY_RELEASE=0.18.0"},
		"localhost:5000/bridge":              {"SENTRY_DSN=http://k@bugsink:8000/3"},
		"archilan-bridge":                    {"SENTRY_DSN=http://k@bugsink:8000/3"},
	}
	for image, want := range cases {
		if got := bridgeErrorReportingEnv("http://k@bugsink:8000/3", image); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: got %v, want %v", image, got, want)
		}
	}
}
