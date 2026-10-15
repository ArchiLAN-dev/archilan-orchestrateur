package service

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"

	"archilan.fr/orchestrateur/internal/docker"
)

// Files a world reads at generation (story 44.2), almost always a ROM. ArchiLAN never stores one: the
// launcher provides it before each generation, it is held here in memory only, consumed by the next
// generation of its launcher and copied into that container's own writable layer, never into the
// persistent session volume nor MinIO. An orchestrateur restart loses them: the launcher sends again.

// RequiredFileMaxBytes caps one provided file. The ROMs of the worlds that need one are cartridges,
// all well under it.
const RequiredFileMaxBytes = 64 << 20

// MaxRequiredFilesPerGeneration bounds the files one generation can take.
const MaxRequiredFilesPerGeneration = 4

// requiredFileTTL bounds how long a staged file waits for its generation: a personal run goes through
// the API's queue and the session configuration first, minutes at most.
const requiredFileTTL = 30 * time.Minute

var (
	ErrRequiredFileExpired  = errors.New("required file unknown or expired")
	ErrRequiredFilesFull    = errors.New("required files storage full")
	ErrInvalidRequiredFile  = errors.New("invalid required file")
	ErrTooManyRequiredFiles = errors.New("too many required files")
)

// requiredFileSetting is the host.yaml setting a failed generation names (story 44.1): Group.key.
var requiredFileSetting = regexp.MustCompile(`^\w+\.\w+$`)

// RequiredFile is one provided file, ready to be copied into a generation container.
type RequiredFile struct {
	Setting  string // e.g. DK3Settings.rom_file
	Filename string // sanitized base name, the extension kept
	Data     []byte
}

type stagedRequiredFile struct {
	file      RequiredFile
	expiresAt time.Time
}

// requiredFileStore holds the staged files, in memory and under a total size cap.
type requiredFileStore struct {
	mu       sync.Mutex
	files    map[string]*stagedRequiredFile
	size     int64
	maxBytes int64
}

func newRequiredFileStore(maxBytes int64) *requiredFileStore {
	return &requiredFileStore{files: map[string]*stagedRequiredFile{}, maxBytes: maxBytes}
}

// stage keeps a copy-free reference to data and returns the one-use id of the file.
func (st *requiredFileStore) stage(setting, filename string, data []byte, now time.Time) (string, time.Time, error) {
	if !requiredFileSetting.MatchString(setting) || len(data) == 0 || len(data) > RequiredFileMaxBytes {
		return "", time.Time{}, ErrInvalidRequiredFile
	}
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", time.Time{}, fmt.Errorf("generate required file id: %w", err)
	}
	id := hex.EncodeToString(buf)
	expiresAt := now.Add(requiredFileTTL)

	st.mu.Lock()
	defer st.mu.Unlock()
	st.pruneLocked(now)
	if st.size+int64(len(data)) > st.maxBytes {
		return "", time.Time{}, ErrRequiredFilesFull
	}
	st.files[id] = &stagedRequiredFile{
		file:      RequiredFile{Setting: setting, Filename: sanitizeRequiredFilename(filename), Data: data},
		expiresAt: expiresAt,
	}
	st.size += int64(len(data))
	return id, expiresAt, nil
}

// has reports whether every id is staged and alive, without consuming any.
func (st *requiredFileStore) has(ids []string, now time.Time) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.pruneLocked(now)
	for _, id := range ids {
		if _, ok := st.files[id]; !ok {
			return false
		}
	}
	return true
}

// take consumes the files: all of them, or none when one is unknown or expired.
func (st *requiredFileStore) take(ids []string, now time.Time) ([]RequiredFile, error) {
	if len(ids) > MaxRequiredFilesPerGeneration {
		return nil, ErrTooManyRequiredFiles
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	st.pruneLocked(now)
	seen := map[string]bool{}
	for _, id := range ids {
		if _, ok := st.files[id]; !ok || seen[id] {
			return nil, ErrRequiredFileExpired
		}
		seen[id] = true
	}
	files := make([]RequiredFile, 0, len(ids))
	for _, id := range ids {
		files = append(files, st.files[id].file)
		st.removeLocked(id)
	}
	return files, nil
}

// release drops a staged file; an unknown id is not an error.
func (st *requiredFileStore) release(id string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.removeLocked(id)
}

func (st *requiredFileStore) prune(now time.Time) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.pruneLocked(now)
}

func (st *requiredFileStore) pruneLocked(now time.Time) {
	for id, f := range st.files {
		if !now.Before(f.expiresAt) {
			st.removeLocked(id)
		}
	}
}

func (st *requiredFileStore) removeLocked(id string) {
	if f, ok := st.files[id]; ok {
		st.size -= int64(len(f.file.Data))
		delete(st.files, id)
	}
}

var unsafeFilenameChars = regexp.MustCompile(`[^A-Za-z0-9._()\- ]`)

// sanitizeRequiredFilename keeps a base name safe to write in the container, extension included (some
// worlds check it).
func sanitizeRequiredFilename(name string) string {
	name = path.Base(strings.ReplaceAll(name, `\`, "/"))
	name = strings.TrimSpace(unsafeFilenameChars.ReplaceAllString(name, "_"))
	if name == "" || name == "." || name == ".." {
		return "file"
	}
	if len(name) > 120 {
		ext := path.Ext(name)
		if len(ext) > 16 {
			ext = ""
		}
		name = name[:120-len(ext)] + ext
	}
	return name
}

// StageRequiredFile holds a file for the next generation that names its id (story 44.2).
func (s *Service) StageRequiredFile(setting, filename string, data []byte) (string, time.Time, error) {
	return s.requiredFiles.stage(setting, filename, data, time.Now())
}

// ReleaseRequiredFile drops a staged file before its generation (the launch was abandoned).
func (s *Service) ReleaseRequiredFile(id string) {
	s.requiredFiles.release(id)
}

func dockerRequiredFiles(files []RequiredFile) []docker.RequiredFile {
	out := make([]docker.RequiredFile, 0, len(files))
	for _, f := range files {
		out = append(out, docker.RequiredFile{Setting: f.Setting, Filename: f.Filename, Data: f.Data})
	}
	return out
}
