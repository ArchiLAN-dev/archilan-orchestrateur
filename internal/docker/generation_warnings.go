package docker

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strings"
)

// generationWarningSentinel opens the line the generator prints on stderr for each warning of a
// generation that succeeded anyway (story 38.12: accessibility not met, as the official Launcher allows).
const generationWarningSentinel = "###ARCHILAN-WARNING###"

// generationWarnings reads the warnings of a successful generation from its stderr: the message of each
// sentinel line, one per line. A line whose record cannot be read is kept as text rather than lost.
func generationWarnings(stderr []byte) string {
	var messages []string
	scanner := bufio.NewScanner(bytes.NewReader(stderr))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		rest, found := strings.CutPrefix(line, generationWarningSentinel)
		if !found {
			continue
		}
		rest = strings.TrimSpace(rest)
		var record struct {
			Message string `json:"message"`
		}
		if err := json.Unmarshal([]byte(rest), &record); err == nil && record.Message != "" {
			messages = append(messages, record.Message)
		} else if rest != "" {
			messages = append(messages, rest)
		}
	}
	return strings.Join(messages, "\n")
}
