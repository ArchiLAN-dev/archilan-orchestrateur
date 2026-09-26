package docker

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
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

func testClient(t *testing.T, handler http.HandlerFunc) (*Client, *atomic.Int32) {
	t.Helper()
	calls := &atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	target, _ := url.Parse(srv.URL)

	return &Client{
		http: &http.Client{Transport: redirectTransport{target: target}},
		log:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	}, calls
}

// Story 38.8: a verdict records the id of the image that produced it, which also tells a
// re-push on the same tag apart.
func TestImageIDInspectsTheImage(t *testing.T) {
	var path atomic.Value
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		path.Store(r.URL.Path)
		_, _ = w.Write([]byte(`{"Id":"sha256:abc123","RepoTags":["ghcr.io/archilan-dev/archipelago:0.16.1"]}`))
	})

	id, err := c.ImageID(context.Background(), "ghcr.io/archilan-dev/archipelago:0.16.1")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "sha256:abc123" {
		t.Errorf("expected sha256:abc123, got %q", id)
	}
	if p, _ := path.Load().(string); !strings.HasSuffix(p, "/images/ghcr.io/archilan-dev/archipelago:0.16.1/json") {
		t.Errorf("unexpected inspect path %q", p)
	}
}

func TestImageIDIsCachedForTheSameReference(t *testing.T) {
	c, calls := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"Id":"sha256:abc123"}`))
	})

	_, _ = c.ImageID(context.Background(), "archipelago:latest")
	_, _ = c.ImageID(context.Background(), "archipelago:latest")

	if calls.Load() != 1 {
		t.Errorf("expected one inspection, got %d", calls.Load())
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

	if calls.Load() != 3 {
		t.Errorf("expected three inspections, got %d", calls.Load())
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

// Story 38.8 review: a verdict names the image of the container that produced it, not whatever the
// tag points to when the test ends.
func TestContainerImageIDReadsTheImageTheContainerWasCreatedFrom(t *testing.T) {
	var path atomic.Value
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		path.Store(r.URL.Path)
		_, _ = w.Write([]byte(`{"Id":"c1","Image":"sha256:fromthecontainer","Config":{"Image":"archipelago:latest"}}`))
	})

	id, err := c.containerImageID(context.Background(), "c1")

	if err != nil || id != "sha256:fromthecontainer" {
		t.Errorf("expected the container's image, got %q, %v", id, err)
	}
	if p, _ := path.Load().(string); !strings.HasSuffix(p, "/containers/c1/json") {
		t.Errorf("unexpected inspect path %q", p)
	}
}

// A hung Docker socket must not hang GET /runtime: the inspection has its own deadline.
func TestImageIDGivesUpOnAHungDaemon(t *testing.T) {
	release := make(chan struct{})
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		<-release
	})
	defer close(release)
	c.inspectTimeout = 50 * time.Millisecond

	start := time.Now()
	_, err := c.ImageID(context.Background(), "archipelago:latest")

	if err == nil {
		t.Fatal("expected a timeout error")
	}
	if time.Since(start) > 2*time.Second {
		t.Errorf("the inspection did not honour its deadline")
	}
}
