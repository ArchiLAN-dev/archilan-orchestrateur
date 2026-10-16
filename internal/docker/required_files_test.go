package docker

import (
	"archive/tar"
	"io"
	"slices"
	"testing"
)

// Story 44.2: the files the launcher provides go to the container's writable layer, never /data, and
// generate_multiworld.py is told where each one is.

func TestGenerateCommand_pointsEachSettingAtItsFile(t *testing.T) {
	cmd := generateCommand("", GenerateOptions{RequiredFiles: []RequiredFile{
		{Setting: "DK3Settings.rom_file", Filename: "dkc3.sfc"},
		{Setting: "OOTSettings.rom_file", Filename: "dkc3.sfc"},
	}})

	want := []string{
		"--required_file", "DK3Settings.rom_file=/tmp/required-files/0/dkc3.sfc",
		"--required_file", "OOTSettings.rom_file=/tmp/required-files/1/dkc3.sfc",
	}
	if !slices.Equal(cmd[len(cmd)-4:], want) {
		t.Fatalf("cmd tail = %v, want %v", cmd[len(cmd)-4:], want)
	}
}

func TestGenerateCommand_withoutFilesIsUnchanged(t *testing.T) {
	cmd := generateCommand("42", GenerateOptions{})

	want := []string{
		"python3", "/usr/local/bin/generate_multiworld.py",
		"--player_files_path", "/data/yamls",
		"--outputpath", "/data/output",
		"--world_directory", "/data/worlds",
		"--seed", "42",
	}
	if !slices.Equal(cmd, want) {
		t.Fatalf("cmd = %v, want %v", cmd, want)
	}
}

func TestRequiredFilesTar_extractsUnderTmpRequiredFiles(t *testing.T) {
	buf, err := requiredFilesTar([]RequiredFile{{Setting: "DK3Settings.rom_file", Filename: "dkc3.sfc", Data: []byte("rom")}})
	if err != nil {
		t.Fatalf("tar: %v", err)
	}

	tr := tar.NewReader(buf)
	var names []string
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read tar: %v", err)
		}
		names = append(names, h.Name)
		if h.Name == "required-files/0/dkc3.sfc" {
			data, _ := io.ReadAll(tr)
			if string(data) != "rom" {
				t.Errorf("file content = %q", data)
			}
		}
	}
	want := []string{"required-files/", "required-files/0/", "required-files/0/dkc3.sfc"}
	if !slices.Equal(names, want) {
		t.Fatalf("entries = %v, want %v (extracted at /tmp, matching the command's paths)", names, want)
	}
}

// Story 44.6: the admin's test generations read the file the same way a session generation does.
func TestPreflightCommand_pointsEachSettingAtItsFile(t *testing.T) {
	cmd := preflightCommand([]RequiredFile{{Setting: "DK3Settings.rom_file", Filename: "dkc3.sfc"}})

	want := []string{
		"python3", "/usr/local/bin/generate_multiworld.py",
		"--player_files_path", "/tmp/yamls",
		"--outputpath", "/tmp/output",
		"--world_directory", "/tmp/worlds",
		"--required_file", "DK3Settings.rom_file=/tmp/required-files/0/dkc3.sfc",
	}
	if !slices.Equal(cmd, want) {
		t.Fatalf("cmd = %v, want %v", cmd, want)
	}
}

func TestPreflightCommand_withoutFilesIsUnchanged(t *testing.T) {
	cmd := preflightCommand(nil)

	if len(cmd) != 8 || slices.Contains(cmd, "--required_file") {
		t.Fatalf("cmd = %v, want the plain preflight command", cmd)
	}
}
