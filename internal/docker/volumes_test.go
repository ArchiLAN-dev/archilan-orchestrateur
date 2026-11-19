package docker

import (
	"slices"
	"testing"
)

// Docker's name filter matches a substring: only volumes named exactly archilan_session_<id> are sessions'.
func TestSessionIDsFromVolumes_keepsOnlySessionVolumes(t *testing.T) {
	volumes := []struct {
		Name string `json:"Name"`
	}{
		{"archilan_session_0123abcd"},
		{"archilan_session_9f1c2e3d-aaaa-bbbb-cccc-111122223333"},
		{"archilan_session_"},
		{"archilan_session_x_backup"},
		{"archilan-prod_session_42"},
		{"other_archilan_session_1"},
	}
	got := sessionIDsFromVolumes(volumes)
	want := []string{"0123abcd", "9f1c2e3d-aaaa-bbbb-cccc-111122223333"}
	if !slices.Equal(got, want) {
		t.Fatalf("ids = %v, want %v", got, want)
	}
}
