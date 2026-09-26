package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// imageIDTTL bounds how long an inspected image id is trusted. The reference alone does not
// pin an image - `archipelago:latest` locally, or a tag re-pushed - so the id is inspected
// again from time to time rather than for the whole life of the process (story 38.8).
const imageIDTTL = 5 * time.Minute

// defaultInspectTimeout bounds one inspection: a hung Docker socket must not hang the caller.
const defaultInspectTimeout = 5 * time.Second

type imageInspectResponse struct {
	ID string `json:"Id"`
}

type containerImageResponse struct {
	Image string `json:"Image"`
}

// ImageID returns the id (`sha256:...`) of the local image ref points to, for GET /runtime
// (story 38.8). Cached for one reference at a time, for imageIDTTL. The lock only guards the
// cache: the inspection itself runs outside it, under its own deadline.
func (c *Client) ImageID(ctx context.Context, ref string) (string, error) {
	now := time.Now()
	if c.now != nil {
		now = c.now()
	}

	c.imageMu.Lock()
	if ref == c.imageRef && c.imageID != "" && now.Sub(c.imageSeenAt) < imageIDTTL {
		id := c.imageID
		c.imageMu.Unlock()
		return id, nil
	}
	c.imageMu.Unlock()

	var ir imageInspectResponse
	if err := c.inspect(ctx, fmt.Sprintf("/images/%s/json", ref), &ir); err != nil {
		return "", fmt.Errorf("inspect image %s: %w", ref, err)
	}

	c.imageMu.Lock()
	c.imageRef, c.imageID, c.imageSeenAt = ref, ir.ID, now
	c.imageMu.Unlock()
	return ir.ID, nil
}

// containerImageID returns the id of the image a container was created from (story 38.8
// review): the image that actually ran, whatever the tag points to by the time it ends.
func (c *Client) containerImageID(ctx context.Context, containerID string) (string, error) {
	var cr containerImageResponse
	if err := c.inspect(ctx, fmt.Sprintf("/containers/%s/json", containerID), &cr); err != nil {
		return "", fmt.Errorf("inspect container %s: %w", containerID, err)
	}
	return cr.Image, nil
}

// inspect GETs a Docker inspection endpoint under the inspection deadline and decodes it.
func (c *Client) inspect(ctx context.Context, path string, into any) error {
	timeout := c.inspectTimeout
	if timeout <= 0 {
		timeout = defaultInspectTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("status %d: %s", resp.StatusCode, raw)
	}
	if err := json.NewDecoder(resp.Body).Decode(into); err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	return nil
}
