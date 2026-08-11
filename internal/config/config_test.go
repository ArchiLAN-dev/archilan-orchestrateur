package config

import "testing"

// A run is reachable either because its port is published on the host, or because the reverse
// proxy shares a network with its container. Neither means every session this instance launches
// would look healthy and accept nobody - the failure has no symptom until a player complains.
func TestValidateRejectsUnreachableCombination(t *testing.T) {
	cfg := &Config{PublishAPPort: false, ProxyNetwork: ""}

	if err := cfg.Validate(); err == nil {
		t.Fatal("expected an error when the AP port is neither published nor proxied")
	}
}

func TestValidateAcceptsProxiedRuns(t *testing.T) {
	cfg := &Config{PublishAPPort: false, ProxyNetwork: "archilan-proxy"}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected proxied runs to be valid, got %v", err)
	}
}

func TestValidateAcceptsPublishedRuns(t *testing.T) {
	// Local development: no reverse proxy, the desktop client connects to the host port.
	cfg := &Config{PublishAPPort: true, ProxyNetwork: ""}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected published runs to be valid, got %v", err)
	}
}

func TestLoadDefaultsToPublishingTheAPPort(t *testing.T) {
	t.Setenv("API_KEY", "k")
	t.Setenv("BRIDGE_TOKEN", "t")

	cfg := Load()

	if !cfg.PublishAPPort {
		t.Error("AP_PUBLISH_HOST_PORT should default to true so existing deployments keep working")
	}
	if cfg.ProxyNetwork != "" {
		t.Errorf("PROXY_NETWORK should default to empty, got %q", cfg.ProxyNetwork)
	}
}

func TestLoadReadsTheProxySettings(t *testing.T) {
	t.Setenv("API_KEY", "k")
	t.Setenv("BRIDGE_TOKEN", "t")
	t.Setenv("AP_PUBLISH_HOST_PORT", "false")
	t.Setenv("PROXY_NETWORK", "archilan-proxy")

	cfg := Load()

	if cfg.PublishAPPort {
		t.Error("AP_PUBLISH_HOST_PORT=false should disable the host binding")
	}
	if cfg.ProxyNetwork != "archilan-proxy" {
		t.Errorf("PROXY_NETWORK not read, got %q", cfg.ProxyNetwork)
	}
}
