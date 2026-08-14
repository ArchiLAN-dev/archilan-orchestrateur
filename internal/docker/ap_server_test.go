package docker

import "testing"

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
