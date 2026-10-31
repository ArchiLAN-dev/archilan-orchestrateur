package docker

import (
	"archive/tar"
	"io"
	"testing"
)

// Story 38.30: a test generation with a partner finds a second player YAML beside the tested one; without
// one, the container is given exactly what it was before.

func preflightTarFiles(t *testing.T, apworld []byte, playerYaml, partnerYaml []byte) map[string]string {
	t.Helper()
	files := map[string]string{}
	tr := tar.NewReader(preflightTar(apworld, "abc", playerYaml, partnerYaml))
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return files
		}
		if err != nil {
			t.Fatalf("read tar: %v", err)
		}
		if hdr.Typeflag == tar.TypeDir {
			continue
		}
		content, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("read %s: %v", hdr.Name, err)
		}
		files[hdr.Name] = string(content)
	}
}

func TestPreflightTar_addsThePartnerBesideTheTestedPlayer(t *testing.T) {
	files := preflightTarFiles(t, []byte("zip"), []byte("game: Tombola\n"), []byte("game: A Short Hike\n"))

	if files["yamls/preflight.yaml"] != "game: Tombola\n" || files["yamls/partner.yaml"] != "game: A Short Hike\n" {
		t.Fatalf("files = %v", files)
	}
	if files["worlds/abc.apworld"] != "zip" {
		t.Fatalf("the apworld is missing: %v", files)
	}
}

func TestPreflightTar_withoutAPartnerHoldsOnlyTheTestedPlayer(t *testing.T) {
	files := preflightTarFiles(t, nil, []byte("game: Tombola\n"), nil)

	if len(files) != 1 || files["yamls/preflight.yaml"] != "game: Tombola\n" {
		t.Fatalf("files = %v", files)
	}
}
