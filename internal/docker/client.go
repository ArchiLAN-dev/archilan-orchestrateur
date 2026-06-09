package docker

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"archilan.fr/orchestrateur/internal/config"
)

// doRaw sends a request with an arbitrary body (not JSON-encoded).
func (c *Client) doRaw(ctx context.Context, method, path, contentType string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.url(path), body)
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return c.http.Do(req)
}

const apiVersion = "v1.47"
const managedLabel = "archilan.managed"
const sessionLabel = "archilan.session_id"

type Client struct {
	http *http.Client
	cfg  *config.Config
	log  *slog.Logger
}

func New(cfg *config.Config, log *slog.Logger) (*Client, error) {
	socketPath := strings.TrimPrefix(cfg.DockerHost, "unix://")
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
		},
	}
	return &Client{
		http: &http.Client{Transport: transport},
		cfg:  cfg,
		log:  log,
	}, nil
}

func (c *Client) url(path string) string {
	return fmt.Sprintf("http://localhost/%s%s", apiVersion, path)
}

func (c *Client) do(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.url(path), r)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.http.Do(req)
}

// ---------------------------------------------------------------------------
// Container lifecycle
// ---------------------------------------------------------------------------

type createBody struct {
	Image            string              `json:"Image"`
	Env              []string            `json:"Env"`
	Labels           map[string]string   `json:"Labels"`
	ExposedPorts     map[string]struct{} `json:"ExposedPorts"`
	HostConfig       hostConfig          `json:"HostConfig"`
	NetworkingConfig networkingConfig    `json:"NetworkingConfig"`
}

type hostConfig struct {
	PortBindings  map[string][]portBinding `json:"PortBindings"`
	RestartPolicy restartPolicy            `json:"RestartPolicy"`
	Binds         []string                 `json:"Binds"`
	GroupAdd      []string                 `json:"GroupAdd,omitempty"`
}

type portBinding struct {
	HostIP   string `json:"HostIp"`
	HostPort string `json:"HostPort"`
}

type restartPolicy struct {
	Name string `json:"Name"`
}

type networkingConfig struct {
	EndpointsConfig map[string]struct{} `json:"EndpointsConfig"`
}

type createResponse struct {
	ID string `json:"Id"`
}

type CreateConfig struct {
	SessionID        string
	Port             int
	BridgeToken      string
	APImage          string
	ServerPassword   string
	AdminPassword    string
	CentralAPIURL    string
	CentralAPISecret string
}

// Create creates a container but does not start it.
func (c *Client) Create(ctx context.Context, cfg CreateConfig) (string, error) {
	body := createBody{
		Image: c.cfg.BridgeImage,
		Env: []string{
			fmt.Sprintf("SESSION_ID=%s", cfg.SessionID),
			fmt.Sprintf("INTERNAL_TOKEN=%s", cfg.BridgeToken),
			fmt.Sprintf("AP_WS_URL=ws://ap-server-%s:38281", cfg.SessionID),
			fmt.Sprintf("AP_RUNTIME=docker"),
			fmt.Sprintf("AP_IMAGE=%s", cfg.APImage),
			fmt.Sprintf("AP_NETWORK=%s", c.cfg.BridgeNetwork),
			fmt.Sprintf("AP_SERVER_HOST_PORT=%d", cfg.Port+c.cfg.APServerPortOffset),
			fmt.Sprintf("AP_SERVER_PASSWORD=%s", cfg.ServerPassword),
			fmt.Sprintf("AP_ADMIN_PASSWORD=%s", cfg.AdminPassword),
			fmt.Sprintf("CENTRAL_API_URL=%s", cfg.CentralAPIURL),
			fmt.Sprintf("CENTRAL_API_SECRET=%s", cfg.CentralAPISecret),
			"REST_PORT=5000",
			"SAVE_DIR=/data/output",
			"AP_WORLDS_DIR=/data/worlds",
			"AP_YAMLS_DIR=/data/yamls",
			"AP_OUTPUT_DIR=/data/output",
		},
		Labels: map[string]string{
			managedLabel: "true",
			sessionLabel: cfg.SessionID,
		},
		ExposedPorts: map[string]struct{}{"5000/tcp": {}},
		HostConfig: hostConfig{
			PortBindings: map[string][]portBinding{
				"5000/tcp": {{HostIP: "0.0.0.0", HostPort: fmt.Sprintf("%d", cfg.Port)}},
			},
			RestartPolicy: restartPolicy{Name: "unless-stopped"},
			Binds: []string{
				fmt.Sprintf("archilan_session_%s:/data", cfg.SessionID),
				"/var/run/docker.sock:/var/run/docker.sock",
			},
			GroupAdd: c.dockerGroupAdd(),
		},
		NetworkingConfig: networkingConfig{
			EndpointsConfig: map[string]struct{}{
				c.cfg.BridgeNetwork: {},
			},
		},
	}

	name := fmt.Sprintf("archilan-bridge-%s", cfg.SessionID)
	resp, err := c.do(ctx, http.MethodPost, fmt.Sprintf("/containers/create?name=%s", name), body)
	if err != nil {
		return "", fmt.Errorf("create: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("create: status %d: %s", resp.StatusCode, raw)
	}

	var created createResponse
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return "", fmt.Errorf("create decode: %w", err)
	}

	return created.ID, nil
}

// PutArchive uploads a tar archive into the container at /data.
func (c *Client) PutArchive(ctx context.Context, containerID string, tarData io.Reader) error {
	resp, err := c.doRaw(ctx, http.MethodPut,
		fmt.Sprintf("/containers/%s/archive?path=/data", containerID),
		"application/x-tar", tarData)
	if err != nil {
		return fmt.Errorf("put archive: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("put archive: status %d: %s", resp.StatusCode, raw)
	}
	return nil
}

// Start starts a previously created container.
func (c *Client) Start(ctx context.Context, containerID string, sessionID string, port int) error {
	resp, err := c.do(ctx, http.MethodPost, fmt.Sprintf("/containers/%s/start", containerID), nil)
	if err != nil {
		return fmt.Errorf("start: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("start: status %d: %s", resp.StatusCode, raw)
	}

	c.log.Info("container started", "session_id", sessionID, "container_id", containerID, "port", port)
	return nil
}

// ContainerStatus is the live state returned by Docker.
type ContainerStatus struct {
	Running  bool
	ExitCode int
	Status   string // "created" | "running" | "paused" | "restarting" | "exited" | "dead"
}

type inspectResponse struct {
	State struct {
		Running  bool   `json:"Running"`
		ExitCode int    `json:"ExitCode"`
		Status   string `json:"Status"`
	} `json:"State"`
}

type listResponse struct {
	ID    string `json:"Id"`
	State string `json:"State"`
	// ExitCode is not in the list response — use Inspect for that.
}

// Inspect returns the live status of a single container. Returns nil if not found.
func (c *Client) Inspect(ctx context.Context, containerID string) (*ContainerStatus, error) {
	resp, err := c.do(ctx, http.MethodGet, fmt.Sprintf("/containers/%s/json", containerID), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("inspect: status %d: %s", resp.StatusCode, raw)
	}
	var ir inspectResponse
	if err := json.NewDecoder(resp.Body).Decode(&ir); err != nil {
		return nil, fmt.Errorf("inspect decode: %w", err)
	}
	return &ContainerStatus{
		Running:  ir.State.Running,
		ExitCode: ir.State.ExitCode,
		Status:   ir.State.Status,
	}, nil
}

func (c *Client) Stop(ctx context.Context, containerID string) error {
	resp, err := c.do(ctx, http.MethodPost, fmt.Sprintf("/containers/%s/stop?t=10", containerID), nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func (c *Client) Remove(ctx context.Context, containerID string) error {
	resp, err := c.do(ctx, http.MethodDelete, fmt.Sprintf("/containers/%s?force=true", containerID), nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func (c *Client) Restart(ctx context.Context, containerID string) error {
	resp, err := c.do(ctx, http.MethodPost, fmt.Sprintf("/containers/%s/restart?t=10", containerID), nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

// ---------------------------------------------------------------------------
// One-shot template generation
// ---------------------------------------------------------------------------

type createOneShotBody struct {
	Image           string   `json:"Image"`
	Cmd             []string `json:"Cmd"`
	NetworkDisabled bool     `json:"NetworkDisabled"`
}

type waitResponse struct {
	StatusCode int `json:"StatusCode"`
}

func (c *Client) createOneShot(ctx context.Context, image string, cmd []string) (string, error) {
	body := createOneShotBody{Image: image, Cmd: cmd, NetworkDisabled: true}
	resp, err := c.do(ctx, http.MethodPost, "/containers/create", body)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("create one-shot: status %d: %s", resp.StatusCode, raw)
	}
	var created createResponse
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return "", err
	}
	return created.ID, nil
}

func (c *Client) putArchiveTo(ctx context.Context, containerID, path string, tarData io.Reader) error {
	resp, err := c.doRaw(ctx, http.MethodPut,
		fmt.Sprintf("/containers/%s/archive?path=%s", containerID, path),
		"application/x-tar", tarData)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("put archive: status %d: %s", resp.StatusCode, raw)
	}
	return nil
}

func (c *Client) startContainer(ctx context.Context, containerID string) error {
	resp, err := c.do(ctx, http.MethodPost, fmt.Sprintf("/containers/%s/start", containerID), nil)
	if err != nil {
		return fmt.Errorf("start: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("start: status %d: %s", resp.StatusCode, raw)
	}
	return nil
}

func (c *Client) waitContainer(ctx context.Context, containerID string) (int, error) {
	resp, err := c.do(ctx, http.MethodPost, fmt.Sprintf("/containers/%s/wait", containerID), nil)
	if err != nil {
		return -1, err
	}
	defer resp.Body.Close()
	var wr waitResponse
	if err := json.NewDecoder(resp.Body).Decode(&wr); err != nil {
		return -1, err
	}
	return wr.StatusCode, nil
}

// containerLogs returns stdout or stderr from a stopped container.
// Docker log frames: 1-byte stream type (1=stdout,2=stderr), 3 bytes padding, 4-byte big-endian length, then data.
func (c *Client) containerLogs(ctx context.Context, containerID string, stdout, stderr bool) ([]byte, error) {
	path := fmt.Sprintf("/containers/%s/logs?follow=false&stdout=%v&stderr=%v", containerID, stdout, stderr)
	resp, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var out bytes.Buffer
	hdr := make([]byte, 8)
	for {
		if _, err := io.ReadFull(resp.Body, hdr); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				break
			}
			return nil, err
		}
		streamType := hdr[0]
		size := binary.BigEndian.Uint32(hdr[4:8])
		data := make([]byte, size)
		if _, err := io.ReadFull(resp.Body, data); err != nil {
			return nil, err
		}
		wantType := uint8(2)
		if stdout {
			wantType = 1
		}
		if streamType == wantType {
			out.Write(data)
		}
	}
	return out.Bytes(), nil
}

// IntrospectOptions runs a one-shot Archipelago container to classify option types for
// the apworld. Returns the raw JSON bytes: {"options": {"key": {"type": "...", ...}}}.
func (c *Client) IntrospectOptions(ctx context.Context, apworldData []byte, hash string) ([]byte, error) {
	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)
	_ = tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: "worlds/", Mode: 0755})
	filename := hash + ".apworld"
	_ = tw.WriteHeader(&tar.Header{Name: "worlds/" + filename, Mode: 0644, Size: int64(len(apworldData))})
	_, _ = tw.Write(apworldData)
	_ = tw.Close()

	cmd := []string{
		"python3", "/usr/local/bin/introspect_options.py",
		"--world_directory", "/tmp/worlds",
	}
	containerID, err := c.createOneShot(ctx, c.cfg.APImage, cmd)
	if err != nil {
		return nil, fmt.Errorf("create introspect container: %w", err)
	}
	defer func() { _ = c.Remove(ctx, containerID) }()

	if err := c.putArchiveTo(ctx, containerID, "/tmp", &tarBuf); err != nil {
		return nil, fmt.Errorf("copy apworld to introspect container: %w", err)
	}

	if err := c.startContainer(ctx, containerID); err != nil {
		return nil, fmt.Errorf("start introspect container: %w", err)
	}

	exitCode, err := c.waitContainer(ctx, containerID)
	if err != nil {
		return nil, fmt.Errorf("wait for introspect container: %w", err)
	}

	if exitCode != 0 {
		stderr, _ := c.containerLogs(ctx, containerID, false, true)
		return nil, fmt.Errorf("introspect_options exited %d: %s", exitCode, bytes.TrimSpace(stderr))
	}

	out, err := c.containerLogs(ctx, containerID, true, false)
	if err != nil {
		return nil, fmt.Errorf("read introspect output: %w", err)
	}
	return bytes.TrimSpace(out), nil
}

// GenerateTemplate runs a one-shot Archipelago container to produce the default YAML
// template for the apworld. The game name is auto-detected by generate_template.py.
func (c *Client) GenerateTemplate(ctx context.Context, apworldData []byte, hash string) ([]byte, error) {
	// Pack the apworld into a tar at worlds/{hash}.apworld so generate_template.py can find it.
	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)
	_ = tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: "worlds/", Mode: 0755})
	filename := hash + ".apworld"
	_ = tw.WriteHeader(&tar.Header{Name: "worlds/" + filename, Mode: 0644, Size: int64(len(apworldData))})
	_, _ = tw.Write(apworldData)
	_ = tw.Close()

	cmd := []string{
		"python3", "/usr/local/bin/generate_template.py",
		"--outputpath", "/tmp/out",
		"--world_directory", "/tmp/worlds",
	}
	containerID, err := c.createOneShot(ctx, c.cfg.APImage, cmd)
	if err != nil {
		return nil, fmt.Errorf("create template container: %w", err)
	}
	defer func() { _ = c.Remove(ctx, containerID) }()

	if err := c.putArchiveTo(ctx, containerID, "/tmp", &tarBuf); err != nil {
		return nil, fmt.Errorf("copy apworld to container: %w", err)
	}

	if err := c.startContainer(ctx, containerID); err != nil {
		return nil, fmt.Errorf("start template container: %w", err)
	}

	exitCode, err := c.waitContainer(ctx, containerID)
	if err != nil {
		return nil, fmt.Errorf("wait for template container: %w", err)
	}

	if exitCode != 0 {
		stderr, _ := c.containerLogs(ctx, containerID, false, true)
		return nil, fmt.Errorf("generate_template exited %d: %s", exitCode, bytes.TrimSpace(stderr))
	}

	yamlData, err := c.containerLogs(ctx, containerID, true, false)
	if err != nil {
		return nil, fmt.Errorf("read template output: %w", err)
	}
	return bytes.TrimSpace(yamlData), nil
}

// ---------------------------------------------------------------------------
// Event stream
// ---------------------------------------------------------------------------

type EventType string

const (
	EventStart EventType = "start"
	EventDie   EventType = "die"
)

type Event struct {
	Type        EventType
	SessionID   string
	ContainerID string
	ExitCode    string
}

type dockerEvent struct {
	Action string `json:"Action"`
	Actor  struct {
		ID         string            `json:"ID"`
		Attributes map[string]string `json:"Attributes"`
	} `json:"Actor"`
}

// ---------------------------------------------------------------------------
// Session generation container
// ---------------------------------------------------------------------------

type createSessionBody struct {
	Image      string            `json:"Image"`
	Cmd        []string          `json:"Cmd"`
	Labels     map[string]string `json:"Labels"`
	HostConfig sessionHostConfig `json:"HostConfig"`
}

type sessionHostConfig struct {
	Binds       []string `json:"Binds"`
	NetworkMode string   `json:"NetworkMode"`
}

// GenerateOptions carries the optional Archipelago generator options for a generation.
// They are passed straight to Generate.main() (via generate_multiworld.py); a nil/empty
// field is omitted so the generator's host.yaml default applies.
type GenerateOptions struct {
	PlandoOptions []string // --plando (comma-joined); subset of bosses/items/texts/connections
	Race          *bool    // --race (presence-only)
	Spoiler       *int     // --spoiler N (0..3)
}

// GenerateMultiworld runs a one-shot container to generate the multiworld.
// It uploads tarData into the container at /data, runs generate_multiworld.py,
// and returns the output filename (from stdout) and the container ID.
func (c *Client) GenerateMultiworld(ctx context.Context, sessionID, seed string, opts GenerateOptions, tarData io.Reader) (outputFile string, jobID string, err error) {
	cmd := []string{
		"python3", "/usr/local/bin/generate_multiworld.py",
		"--player_files_path", "/data/yamls",
		"--outputpath", "/data/output",
		"--world_directory", "/data/worlds",
	}
	if seed != "" {
		cmd = append(cmd, "--seed", seed)
	}
	// Generation options: forwarded to Generate.main(). Only emitted when set, so the
	// generator's own host.yaml default stands otherwise. (Generate.py accepts --spoiler
	// int, --race store_true, --plando string.)
	if opts.Spoiler != nil {
		cmd = append(cmd, "--spoiler", fmt.Sprintf("%d", *opts.Spoiler))
	}
	if opts.Race != nil && *opts.Race {
		cmd = append(cmd, "--race")
	}
	if len(opts.PlandoOptions) > 0 {
		cmd = append(cmd, "--plando", strings.Join(opts.PlandoOptions, ", "))
	}

	body := createSessionBody{
		Image: c.cfg.APImage,
		Cmd:   cmd,
		Labels: map[string]string{
			managedLabel: "false",
		},
		HostConfig: sessionHostConfig{
			Binds:       []string{fmt.Sprintf("archilan_session_%s:/data", sessionID)},
			NetworkMode: "none",
		},
	}

	name := fmt.Sprintf("archilan-gen-%s", sessionID)
	resp, err := c.do(ctx, http.MethodPost, fmt.Sprintf("/containers/create?name=%s", name), body)
	if err != nil {
		return "", "", fmt.Errorf("generate multiworld create: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		return "", "", fmt.Errorf("generate multiworld create: status %d: %s", resp.StatusCode, raw)
	}

	var created createResponse
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return "", "", fmt.Errorf("generate multiworld create decode: %w", err)
	}
	containerID := created.ID
	defer func() { _ = c.Remove(ctx, containerID) }()

	if err := c.putArchiveTo(ctx, containerID, "/data", tarData); err != nil {
		return "", containerID, fmt.Errorf("generate multiworld put archive: %w", err)
	}

	if err := c.startContainer(ctx, containerID); err != nil {
		return "", containerID, fmt.Errorf("generate multiworld start: %w", err)
	}

	exitCode, err := c.waitContainer(ctx, containerID)
	if err != nil {
		return "", containerID, fmt.Errorf("generate multiworld wait: %w", err)
	}

	if exitCode != 0 {
		stderrOut, _ := c.containerLogs(ctx, containerID, false, true)
		return "", containerID, fmt.Errorf("generate_multiworld.py exited %d: %s", exitCode, bytes.TrimSpace(stderrOut))
	}

	stdoutOut, err := c.containerLogs(ctx, containerID, true, false)
	if err != nil {
		return "", containerID, fmt.Errorf("generate multiworld read stdout: %w", err)
	}

	outFile := strings.TrimSpace(string(stdoutOut))
	if outFile == "" {
		stderrOut, _ := c.containerLogs(ctx, containerID, false, true)
		return "", containerID, fmt.Errorf("generate_multiworld.py produced no output: %s", bytes.TrimSpace(stderrOut))
	}

	return outFile, containerID, nil
}

// ---------------------------------------------------------------------------
// AP server container
// ---------------------------------------------------------------------------

type APServerCreateConfig struct {
	SessionID      string
	APPort         int
	ServerPassword string // AP join password (players use this)
	AdminPassword  string // AP server admin password (enables !admin commands)
	APImage        string
	BridgeNetwork  string
	// Optional AP server_options for this session. Empty string / nil pointer means
	// "don't pass it" — the launch script's own default stands. Modes are validated
	// upstream (service.Launch). See epic 27.
	ReleaseMode         string // !release policy; empty = launch-script default (disabled)
	CollectMode         string // !collect policy; empty = launch-script default (disabled)
	RemainingMode       string // !remaining policy
	CountdownMode       string // !countdown policy
	DisableItemCheat    *bool  // disable !getitem
	HintCost            *int   // % of checks per hint
	LocationCheckPoints *int   // points per check
	AutoShutdown        *int   // seconds of inactivity before shutdown (0 = never)
	Compatibility       *int   // 2 casual / 1 racing / 0 tournament
}

type createAPServerBody struct {
	Image            string              `json:"Image"`
	Cmd              []string            `json:"Cmd"`
	Env              []string            `json:"Env"`
	Labels           map[string]string   `json:"Labels"`
	ExposedPorts     map[string]struct{} `json:"ExposedPorts"`
	HostConfig       apServerHostConfig  `json:"HostConfig"`
	NetworkingConfig networkingConfig    `json:"NetworkingConfig"`
}

type apServerHostConfig struct {
	PortBindings  map[string][]portBinding `json:"PortBindings"`
	RestartPolicy restartPolicy            `json:"RestartPolicy"`
	Binds         []string                 `json:"Binds"`
}

// CreateAPServer creates (but does not start) an AP server container for the given session.
func (c *Client) CreateAPServer(ctx context.Context, cfg APServerCreateConfig) (string, error) {
	env := []string{
		fmt.Sprintf("PASSWORD=%s", cfg.ServerPassword),
		fmt.Sprintf("SERVER_PASSWORD=%s", cfg.AdminPassword),
		"ARCHIPELAGO_OUTPUT_DIR=/data/output",
	}
	// Only override the launch-script defaults when explicitly provided, so the
	// script's own defaults stand otherwise.
	for k, v := range map[string]string{
		"RELEASE_MODE":   cfg.ReleaseMode,
		"COLLECT_MODE":   cfg.CollectMode,
		"REMAINING_MODE": cfg.RemainingMode,
		"COUNTDOWN_MODE": cfg.CountdownMode,
	} {
		if v != "" {
			env = append(env, fmt.Sprintf("%s=%s", k, v))
		}
	}
	if cfg.DisableItemCheat != nil {
		env = append(env, fmt.Sprintf("DISABLE_ITEM_CHEAT=%t", *cfg.DisableItemCheat))
	}
	for k, v := range map[string]*int{
		"HINT_COST":             cfg.HintCost,
		"LOCATION_CHECK_POINTS": cfg.LocationCheckPoints,
		"AUTO_SHUTDOWN":         cfg.AutoShutdown,
		"COMPATIBILITY":         cfg.Compatibility,
	} {
		if v != nil {
			env = append(env, fmt.Sprintf("%s=%d", k, *v))
		}
	}

	body := createAPServerBody{
		Image: cfg.APImage,
		Cmd:   []string{"/ap_server.sh"},
		Env:   env,
		Labels: map[string]string{
			managedLabel:    "true",
			sessionLabel:    cfg.SessionID,
			"archilan.role": "ap-server",
		},
		ExposedPorts: map[string]struct{}{"38281/tcp": {}},
		HostConfig: apServerHostConfig{
			PortBindings: map[string][]portBinding{
				"38281/tcp": {{HostIP: "0.0.0.0", HostPort: fmt.Sprintf("%d", cfg.APPort)}},
			},
			RestartPolicy: restartPolicy{Name: "unless-stopped"},
			Binds:         []string{fmt.Sprintf("archilan_session_%s:/data", cfg.SessionID)},
		},
		NetworkingConfig: networkingConfig{
			EndpointsConfig: map[string]struct{}{
				cfg.BridgeNetwork: {},
			},
		},
	}

	name := fmt.Sprintf("ap-server-%s", cfg.SessionID)
	resp, err := c.do(ctx, http.MethodPost, fmt.Sprintf("/containers/create?name=%s", name), body)
	if err != nil {
		return "", fmt.Errorf("create ap server: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("create ap server: status %d: %s", resp.StatusCode, raw)
	}

	var created createResponse
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return "", fmt.Errorf("create ap server decode: %w", err)
	}
	return created.ID, nil
}

// RemoveAPServer force-removes the AP server container for a session. Idempotent (404 is ignored).
func (c *Client) RemoveAPServer(ctx context.Context, sessionID string) error {
	name := fmt.Sprintf("ap-server-%s", sessionID)
	resp, err := c.do(ctx, http.MethodDelete, fmt.Sprintf("/containers/%s?force=true", name), nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	raw, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("remove ap server: status %d: %s", resp.StatusCode, raw)
}

// RemoveVolume removes the session data volume. Idempotent (404 is ignored).
func (c *Client) RemoveVolume(ctx context.Context, sessionID string) error {
	name := fmt.Sprintf("archilan_session_%s", sessionID)
	resp, err := c.do(ctx, http.MethodDelete, fmt.Sprintf("/volumes/%s", name), nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	raw, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("remove volume: status %d: %s", resp.StatusCode, raw)
}

// InjectFileToVolume creates a stopped container mounting the session volume,
// uploads a single file into /data/output/{filename}, then removes the container.
func (c *Client) InjectFileToVolume(ctx context.Context, sessionID, filename string, data []byte) error {
	// Build a tar with the file at /data/output/{filename}
	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)
	_ = tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: "output/", Mode: 0755})
	_ = tw.WriteHeader(&tar.Header{Name: "output/" + filename, Mode: 0644, Size: int64(len(data))})
	_, _ = tw.Write(data)
	_ = tw.Close()

	return c.PutDataToVolume(ctx, sessionID, &tarBuf)
}

// PutDataToVolume uploads an arbitrary tar archive into the session volume at /data
// via a stopped one-shot container (Docker copy-to-container), then removes it.
func (c *Client) PutDataToVolume(ctx context.Context, sessionID string, tarData io.Reader) error {
	type injectBody struct {
		Image      string            `json:"Image"`
		Labels     map[string]string `json:"Labels"`
		HostConfig sessionHostConfig `json:"HostConfig"`
	}
	body := injectBody{
		Image:  c.cfg.APImage,
		Labels: map[string]string{managedLabel: "false"},
		HostConfig: sessionHostConfig{
			Binds:       []string{fmt.Sprintf("archilan_session_%s:/data", sessionID)},
			NetworkMode: "none",
		},
	}

	resp, err := c.do(ctx, http.MethodPost, "/containers/create", body)
	if err != nil {
		return fmt.Errorf("put data to volume create: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("put data to volume create: status %d: %s", resp.StatusCode, raw)
	}

	var created createResponse
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return fmt.Errorf("put data to volume decode: %w", err)
	}
	containerID := created.ID
	defer func() { _ = c.Remove(ctx, containerID) }()

	return c.putArchiveTo(ctx, containerID, "/data", tarData)
}

// CopyOutputDirFromVolume returns the whole /data/output directory of the session
// volume as a tar archive (entries prefixed with "output/"). Used to persist every
// generated file (multidata, per-player patches, spoiler), not just one.
func (c *Client) CopyOutputDirFromVolume(ctx context.Context, sessionID string) ([]byte, error) {
	type copyBody struct {
		Image      string            `json:"Image"`
		Labels     map[string]string `json:"Labels"`
		HostConfig sessionHostConfig `json:"HostConfig"`
	}
	body := copyBody{
		Image:  c.cfg.APImage,
		Labels: map[string]string{managedLabel: "false"},
		HostConfig: sessionHostConfig{
			Binds:       []string{fmt.Sprintf("archilan_session_%s:/data", sessionID)},
			NetworkMode: "none",
		},
	}

	resp, err := c.do(ctx, http.MethodPost, "/containers/create", body)
	if err != nil {
		return nil, fmt.Errorf("copy output dir create: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("copy output dir create: status %d: %s", resp.StatusCode, raw)
	}

	var created createResponse
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return nil, fmt.Errorf("copy output dir decode: %w", err)
	}
	containerID := created.ID
	defer func() { _ = c.Remove(ctx, containerID) }()

	archiveResp, err := c.do(ctx, http.MethodGet,
		fmt.Sprintf("/containers/%s/archive?path=%s", containerID, url.QueryEscape("/data/output")), nil)
	if err != nil {
		return nil, fmt.Errorf("copy output dir archive: %w", err)
	}
	defer archiveResp.Body.Close()
	if archiveResp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(archiveResp.Body)
		return nil, fmt.Errorf("copy output dir archive: status %d: %s", archiveResp.StatusCode, raw)
	}

	return io.ReadAll(archiveResp.Body)
}

// WaitForAddress probes addr via TCP every 2 seconds until it succeeds or timeout elapses.
func (c *Client) WaitForAddress(ctx context.Context, addr string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
		if err == nil {
			conn.Close()
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(2 * time.Second):
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Event stream
// ---------------------------------------------------------------------------

func (c *Client) WatchEvents(ctx context.Context) (<-chan Event, <-chan error) {
	out := make(chan Event, 32)
	errc := make(chan error, 1)

	go func() {
		defer close(out)

		filters := fmt.Sprintf(`{"label":["%s=true"],"type":["container"],"event":["start","die"]}`, managedLabel)
		path := fmt.Sprintf("/events?filters=%s", filters)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url(path), nil)
		if err != nil {
			errc <- err
			return
		}

		resp, err := c.http.Do(req)
		if err != nil {
			errc <- err
			return
		}
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			var ev dockerEvent
			if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
				continue
			}
			sessionID := ev.Actor.Attributes[sessionLabel]
			if sessionID == "" {
				continue
			}
			out <- Event{
				Type:        EventType(ev.Action),
				SessionID:   sessionID,
				ContainerID: ev.Actor.ID,
				ExitCode:    ev.Actor.Attributes["exitCode"],
			}
		}

		if err := scanner.Err(); err != nil && ctx.Err() == nil {
			errc <- err
		}
	}()

	return out, errc
}

// dockerGroupAdd returns the GroupAdd slice to pass to bridge containers.
// If DOCKER_GID is set, it uses that GID so the bridge can access /var/run/docker.sock.
// Falls back to "0" (root group) for backwards compatibility.
func (c *Client) dockerGroupAdd() []string {
	if c.cfg.DockerGID != "" {
		return []string{c.cfg.DockerGID}
	}
	return []string{"0"}
}
