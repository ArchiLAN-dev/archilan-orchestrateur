package docker

import (
	"encoding/json"
	"strings"
	"testing"
)

// Epic 37: with the reverse proxy in front, the AP server must not publish anything on the host.
// Traefik reaches it at ap-server-{sessionId}:38281 over the shared network instead.
func TestAPServerPortBindingsAreNilWhenProxied(t *testing.T) {
	bindings := apServerPortBindings(APServerCreateConfig{
		SessionID:       "sess-1",
		APPort:          35042,
		PublishHostPort: false,
	})

	if bindings != nil {
		t.Fatalf("expected no host binding when the run is proxied, got %v", bindings)
	}
}

func TestAPServerPortBindingsPublishTheAllocatedPort(t *testing.T) {
	bindings := apServerPortBindings(APServerCreateConfig{
		SessionID:       "sess-1",
		APPort:          35042,
		PublishHostPort: true,
	})

	binding, ok := bindings["38281/tcp"]
	if !ok {
		t.Fatalf("expected a binding for the container port, got %v", bindings)
	}
	if len(binding) != 1 {
		t.Fatalf("expected exactly one binding, got %d", len(binding))
	}
	if binding[0].HostPort != "35042" {
		t.Errorf("expected host port 35042, got %q", binding[0].HostPort)
	}
	if binding[0].HostIP != "0.0.0.0" {
		t.Errorf("expected 0.0.0.0, got %q", binding[0].HostIP)
	}
}

// Story 37.7 : le port REST du bridge cesse d'etre publie quand l'API le joint par le reseau
// interne. C'etait la derniere socket publique d'une run.
func TestBridgePortBindingsAreNilWhenTheApiUsesTheInternalPath(t *testing.T) {
	bindings := bridgePortBindings(CreateConfig{SessionID: "sess-1", Port: 25042}, false)

	if bindings != nil {
		t.Fatalf("expected no host binding when the API joins the bridge internally, got %v", bindings)
	}
}

func TestBridgePortBindingsPublishTheAllocatedPortWhenAsked(t *testing.T) {
	bindings := bridgePortBindings(CreateConfig{SessionID: "sess-1", Port: 25042}, true)

	binding, ok := bindings["5000/tcp"]
	if !ok {
		t.Fatalf("expected a binding for the REST port, got %v", bindings)
	}
	if len(binding) != 1 || binding[0].HostPort != "25042" {
		t.Fatalf("expected host port 25042, got %v", binding)
	}
}

// A session container runs code that can come from members: no capability it does not need.
func TestSessionContainersDropTheirCapabilities(t *testing.T) {
	const memory = 512 << 20
	for name, limits := range map[string]containerLimits{
		"bridge":    bridgeLimits(memory),
		"ap server": apServerLimits(memory),
		"one-shot":  oneShotLimits(memory),
	} {
		if len(limits.CapDrop) != 1 || limits.CapDrop[0] != "ALL" {
			t.Errorf("%s: CapDrop = %v, want ALL", name, limits.CapDrop)
		}
		if len(limits.SecurityOpt) != 1 || limits.SecurityOpt[0] != "no-new-privileges:true" {
			t.Errorf("%s: SecurityOpt = %v", name, limits.SecurityOpt)
		}
		if limits.PidsLimit <= 0 {
			t.Errorf("%s: no pids limit", name)
		}
		if limits.Memory != memory {
			t.Errorf("%s: Memory = %d, want %d", name, limits.Memory, memory)
		}
	}
	if len(bridgeLimits(0).CapAdd) != 0 {
		t.Errorf("bridge: CapAdd = %v, want none", bridgeLimits(0).CapAdd)
	}
}

// Whoever controls a bridge must not reach the host through it: the session volume is all it gets.
func TestABridgeMountsOnlyItsSessionVolume(t *testing.T) {
	binds := bridgeBinds("sess-1")

	if len(binds) != 1 || binds[0] != "archilan_session_sess-1:/data" {
		t.Fatalf("binds = %v", binds)
	}
}

// Zero must stay absent from the request: Docker reads a present 0 the same way, the limit is off.
func TestNoMemoryLimitByDefault(t *testing.T) {
	raw, err := json.Marshal(oneShotLimits(0))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "Memory") {
		t.Errorf("limits = %s", raw)
	}
}
