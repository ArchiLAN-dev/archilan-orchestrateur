package service

import (
	"bytes"
	"context"
)

// introspectionJSON splits an introspection sidecar into its JSON object and the text written in front of it.
//
// The image stores the introspection's stdout as-is, and a world that wrote to stdout at import (Cookie Clicker's
// "does not include a manifest file", "Flatpak Dolphin Tool Installation NOT detected"...) put lines in front of the
// JSON: every read of the sidecar then failed, on each call, since the stored object never changes. The JSON starts
// on the first line that opens with "{". Without such a line the input comes back unchanged and the parse fails as
// before.
func introspectionJSON(raw []byte) (object []byte, prefix []byte) {
	if bytes.HasPrefix(bytes.TrimLeft(raw, " \t\r\n"), []byte("{")) {
		return raw, nil
	}
	i := bytes.Index(raw, []byte("\n{"))
	if i < 0 {
		return raw, nil
	}
	return raw[i+1:], bytes.TrimSpace(raw[:i])
}

// cleanIntrospection drops the text in front of an introspection's JSON, logging it so chatty worlds stay visible.
func (s *Service) cleanIntrospection(hash string, raw []byte) []byte {
	object, prefix := introspectionJSON(raw)
	if len(prefix) > 0 {
		s.log.Warn("introspection had text before its JSON, ignored", "hash", hash, "text", string(prefix))
	}
	return object
}

// downloadIntrospection reads an apworld's introspection sidecar, without any text in front of its JSON.
func (s *Service) downloadIntrospection(ctx context.Context, hash string) ([]byte, bool, error) {
	raw, found, err := s.storage.DownloadApworldOptionTypes(ctx, hash)
	if err != nil || !found {
		return raw, found, err
	}
	return s.cleanIntrospection(hash, raw), true, nil
}
