package docker

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// redirectTransport sends every Docker API call to a test server, whatever the host the client
// believes it talks to (the real client dials the Docker socket).
type redirectTransport struct{ target *url.URL }

func (t redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Scheme = t.target.Scheme
	req.URL.Host = t.target.Host
	return http.DefaultTransport.RoundTrip(req)
}

func testClient(t *testing.T, handler http.HandlerFunc) (*Client, *int) {
	t.Helper()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	target, _ := url.Parse(srv.URL)

	return &Client{
		http: &http.Client{Transport: redirectTransport{target: target}},
		log:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	}, &calls
}

// Story 38.8: a verdict records the id of the image that produced it, which also tells a
// re-push on the same tag apart.
func TestImageIDInspectsTheImage(t *testing.T) {
	var path string
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_, _ = w.Write([]byte(`{"Id":"sha256:abc123","RepoTags":["ghcr.io/archilan-dev/archipelago:0.16.1"]}`))
	})

	id, err := c.ImageID(context.Background(), "ghcr.io/archilan-dev/archipelago:0.16.1")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "sha256:abc123" {
		t.Errorf("expected sha256:abc123, got %q", id)
	}
	if !strings.HasSuffix(path, "/images/ghcr.io/archilan-dev/archipelago:0.16.1/json") {
		t.Errorf("unexpected inspect path %q", path)
	}
}

func TestImageIDIsCachedForTheSameReference(t *testing.T) {
	c, calls := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"Id":"sha256:abc123"}`))
	})

	_, _ = c.ImageID(context.Background(), "archipelago:latest")
	_, _ = c.ImageID(context.Background(), "archipelago:latest")

	if *calls != 1 {
		t.Errorf("expected one inspection, got %d", *calls)
	}
}

func TestImageIDIsInspectedAgainForAnotherReferenceOrOnceStale(t *testing.T) {
	now := time.Date(2026, 9, 26, 4, 0, 0, 0, time.UTC)
	c, calls := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"Id":"sha256:abc123"}`))
	})
	c.now = func() time.Time { return now }

	_, _ = c.ImageID(context.Background(), "archipelago:0.16.0")
	_, _ = c.ImageID(context.Background(), "archipelago:0.16.1")
	now = now.Add(imageIDTTL + time.Second)
	_, _ = c.ImageID(context.Background(), "archipelago:0.16.1")

	if *calls != 3 {
		t.Errorf("expected three inspections, got %d", *calls)
	}
}

func TestImageIDFailsWhenTheImageIsUnknown(t *testing.T) {
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"No such image"}`))
	})

	id, err := c.ImageID(context.Background(), "archipelago:gone")

	if err == nil || id != "" {
		t.Errorf("expected an error and no id, got %q, %v", id, err)
	}
}
