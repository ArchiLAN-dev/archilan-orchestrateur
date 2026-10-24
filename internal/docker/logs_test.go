package docker

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func frame(stream byte, text string) []byte {
	hdr := []byte{stream, 0, 0, 0, 0, 0, 0, 0}
	binary.BigEndian.PutUint32(hdr[4:], uint32(len(text)))
	return append(hdr, text...)
}

// Story 17.33: stdout and stderr both reach the admin, in the order the container wrote them.
func TestDemuxLogsKeepsBothStreamsInOrder(t *testing.T) {
	var stream bytes.Buffer
	stream.Write(frame(1, "server up\n"))
	stream.Write(frame(2, "warning: slow\n"))
	stream.Write(frame(1, "player joined\n"))

	got, err := demuxLogs(&stream)
	if err != nil {
		t.Fatalf("demux: %v", err)
	}
	if string(got) != "server up\nwarning: slow\nplayer joined\n" {
		t.Errorf("unexpected logs %q", got)
	}
}

// A TTY container sends raw text, with no frame headers: it is kept untouched.
func TestDemuxLogsKeepsRawTextOfATTYContainer(t *testing.T) {
	got, err := demuxLogs(bytes.NewBufferString("2026-10-24T10:00:00Z hello from a tty\n"))
	if err != nil {
		t.Fatalf("demux: %v", err)
	}
	if string(got) != "2026-10-24T10:00:00Z hello from a tty\n" {
		t.Errorf("unexpected logs %q", got)
	}
}

func TestDemuxLogsOfAnEmptyStreamIsEmpty(t *testing.T) {
	got, err := demuxLogs(&bytes.Buffer{})
	if err != nil || len(got) != 0 {
		t.Errorf("expected nothing, got %q (%v)", got, err)
	}
}

// A stream cut mid-frame keeps what arrived instead of failing the whole read.
func TestDemuxLogsKeepsATruncatedLastFrame(t *testing.T) {
	full := frame(1, "complete line\n")
	partial := frame(1, "cut short\n")
	stream := bytes.NewBuffer(append(full, partial[:12]...))

	got, err := demuxLogs(stream)
	if err != nil {
		t.Fatalf("demux: %v", err)
	}
	if string(got) != "complete line\ncut " {
		t.Errorf("unexpected logs %q", got)
	}
}
