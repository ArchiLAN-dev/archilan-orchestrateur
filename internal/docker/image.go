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

type imageInspectResponse struct {
	ID string `json:"Id"`
}

// ImageID returns the id (`sha256:...`) of the local image ref points to, the one a verdict
// was produced with (story 38.8). Cached for one reference at a time, for imageIDTTL.
func (c *Client) ImageID(ctx context.Context, ref string) (string, error) {
	c.imageMu.Lock()
	defer c.imageMu.Unlock()

	now := time.Now()
	if c.now != nil {
		now = c.now()
	}
	if ref == c.imageRef && c.imageID != "" && now.Sub(c.imageSeenAt) < imageIDTTL {
		return c.imageID, nil
	}

	resp, err := c.do(ctx, http.MethodGet, fmt.Sprintf("/images/%s/json", ref), nil)
	if err != nil {
		return "", fmt.Errorf("inspect image %s: %w", ref, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("inspect image %s: status %d: %s", ref, resp.StatusCode, raw)
	}
	var ir imageInspectResponse
	if err := json.NewDecoder(resp.Body).Decode(&ir); err != nil {
		return "", fmt.Errorf("inspect image %s decode: %w", ref, err)
	}

	c.imageRef, c.imageID, c.imageSeenAt = ref, ir.ID, now
	return ir.ID, nil
}
