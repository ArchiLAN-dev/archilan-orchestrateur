package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Port              int
	APIKey            string
	PortRangeStart    int
	PortRangeEnd      int
	APServerPortOffset int // host_port = bridge_port + APServerPortOffset
	DBPath         string
	DockerHost     string
	BridgeImage    string
	BridgeNetwork  string
	BridgeToken    string
	// ProxyNetwork is the Docker network shared with the reverse proxy. When set, each AP server
	// container is also attached to it so Traefik can reach `ap-server-{sessionId}:38281`
	// directly, without the port ever being published on the host (epic 37).
	ProxyNetwork string
	// PublishBridgePort keeps the historical behaviour of binding the bridge's REST port on
	// 0.0.0.0. The API used to reach it that way - through the server's public address, for a
	// container running on the same machine. Since story 37.7 it joins
	// `archilan-bridge-{sessionId}:5000` over the shared network, so production turns this off and
	// the last public socket of a run closes.
	PublishBridgePort bool
	// PublishAPPort keeps the historical behaviour of binding the AP server port on 0.0.0.0.
	// Production turns it off and routes through the proxy instead; local development, which has
	// no reverse proxy, leaves it on so a desktop client can still connect.
	PublishAPPort bool
	APImage        string
	WebhookURL     string
	WebhookSecret  string
	CentralAPIURL    string
	CentralAPISecret string
	CORSOrigins    []string
	MinioEndpoint       string
	MinioAccessKey      string
	MinioSecretKey      string
	MinioUseSSL         bool
	MinioBucketApworlds string
	MinioBucketSessions string
	DockerGID           string // GID of the docker group on the host (for bridge socket access)

	GenerationTimeout time.Duration // default 10min
	LaunchTimeout     time.Duration // default 2min
	SweeperInterval   time.Duration // default 30s
	PreflightTimeout       time.Duration // default 5min - solo test generations (stories 9.38/9.42)
	PreflightMaxConcurrent int           // shared cap on concurrent preflight containers (default 2)
}

func Load() *Config {
	cfg := loadFromEnv()
	if err := cfg.Validate(); err != nil {
		panic(err.Error())
	}
	return cfg
}

// Validate rejects combinations that would start an orchestrateur unable to serve anyone.
func (c *Config) Validate() error {
	// Neither published on the host nor reachable through a proxy network: every run launched by
	// this instance would be unreachable, and nothing downstream would report it - the session
	// would go `running` and no client could ever connect. Fail at startup instead.
	if !c.PublishAPPort && c.ProxyNetwork == "" {
		return fmt.Errorf("AP_PUBLISH_HOST_PORT=false requires PROXY_NETWORK to be set, " +
			"otherwise no AP server would be reachable")
	}
	return nil
}

func loadFromEnv() *Config {
	return &Config{
		Port:               envInt("PORT", 8000),
		APIKey:             envRequired("API_KEY"),
		PortRangeStart:     envInt("PORT_RANGE_START", 25000),
		PortRangeEnd:       envInt("PORT_RANGE_END", 25099),
		APServerPortOffset: envInt("AP_SERVER_PORT_OFFSET", 10000),
		DBPath:         env("DB_PATH", "/data/orchestrateur.db"),
		DockerHost:     env("DOCKER_HOST", "unix:///var/run/docker.sock"),
		BridgeImage:    env("BRIDGE_IMAGE", "archilan-bridge:latest"),
		BridgeNetwork:  env("BRIDGE_NETWORK", "archilan_default"),
		BridgeToken:    envRequired("BRIDGE_TOKEN"),
		ProxyNetwork:   env("PROXY_NETWORK", ""),
		PublishAPPort:     envBool("AP_PUBLISH_HOST_PORT", true),
		PublishBridgePort: envBool("BRIDGE_PUBLISH_HOST_PORT", true),
		APImage:        env("AP_IMAGE", "archipelago:latest"),
		WebhookURL:     env("WEBHOOK_URL", ""),
		WebhookSecret:  env("WEBHOOK_SECRET", ""),
		CentralAPIURL:    env("CENTRAL_API_URL", ""),
		CentralAPISecret: env("CENTRAL_API_SECRET", ""),
		CORSOrigins:    envList("CORS_ORIGINS", []string{"*"}),
		MinioEndpoint:       env("MINIO_ENDPOINT", ""),
		MinioAccessKey:      env("MINIO_ACCESS_KEY", ""),
		MinioSecretKey:      env("MINIO_SECRET_KEY", ""),
		MinioUseSSL:         envBool("MINIO_USE_SSL", false),
		MinioBucketApworlds: env("MINIO_BUCKET_APWORLDS", "apworlds"),
		MinioBucketSessions: env("MINIO_BUCKET_SESSIONS", "sessions"),
		DockerGID:           env("DOCKER_GID", ""),

		GenerationTimeout: envDuration("GENERATION_TIMEOUT", 600),
		LaunchTimeout:     envDuration("LAUNCH_TIMEOUT", 120),
		SweeperInterval:   envDuration("SWEEPER_INTERVAL", 30),
		PreflightTimeout:       envDuration("PREFLIGHT_TIMEOUT", 300),
		PreflightMaxConcurrent: envInt("PREFLIGHT_MAX_CONCURRENT", 2),
	}
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envRequired(key string) string {
	v := os.Getenv(key)
	if v == "" {
		panic(fmt.Sprintf("required env var %s is not set", key))
	}
	return v
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func envList(key string, fallback []string) []string {
	if v := os.Getenv(key); v != "" {
		return strings.Split(v, ",")
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		return v == "true" || v == "1"
	}
	return fallback
}

func envDuration(key string, fallbackSecs int) time.Duration {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return time.Duration(n) * time.Second
		}
	}
	return time.Duration(fallbackSecs) * time.Second
}
