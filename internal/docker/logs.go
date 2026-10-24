package docker

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// Logs returns the last tail lines of a container's stdout and stderr, in the order they were written, each
// prefixed with Docker's timestamp (story 17.33). found is false when no container has that name.
func (c *Client) Logs(ctx context.Context, name string, tail int) (string, bool, error) {
	path := fmt.Sprintf("/containers/%s/logs?follow=false&stdout=true&stderr=true&timestamps=true&tail=%d", url.PathEscape(name), tail)
	resp, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return "", false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", false, nil
	}
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return "", false, fmt.Errorf("logs: status %d: %s", resp.StatusCode, raw)
	}
	out, err := demuxLogs(resp.Body)
	if err != nil {
		return "", false, err
	}
	return string(out), true, nil
}

// demuxLogs flattens Docker's multiplexed log stream, keeping both stdout and stderr in order. Each frame is a
// 1-byte stream type (0 stdin, 1 stdout, 2 stderr), 3 zero bytes, a 4-byte big-endian length, then the data. A
// container started with a TTY sends raw text instead: anything not shaped like a frame header is kept as is.
func demuxLogs(r io.Reader) ([]byte, error) {
	var out bytes.Buffer
	hdr := make([]byte, 8)
	for {
		n, err := io.ReadFull(r, hdr)
		if err == io.EOF {
			return out.Bytes(), nil
		}
		if err == io.ErrUnexpectedEOF || (err == nil && !isFrameHeader(hdr)) {
			out.Write(hdr[:n])
			if _, err := io.Copy(&out, r); err != nil {
				return nil, err
			}
			return out.Bytes(), nil
		}
		if err != nil {
			return nil, err
		}
		if _, err := io.CopyN(&out, r, int64(binary.BigEndian.Uint32(hdr[4:8]))); err != nil {
			if err == io.EOF {
				return out.Bytes(), nil
			}
			return nil, err
		}
	}
}

func isFrameHeader(hdr []byte) bool {
	return hdr[0] <= 2 && hdr[1] == 0 && hdr[2] == 0 && hdr[3] == 0
}
