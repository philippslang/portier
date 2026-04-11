package portier

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config is the top-level configuration loaded from config.yaml.
type Config struct {
	// Server settings
	Server ServerConfig `yaml:"server"`

	// Services to expose via MCP
	Services []ServiceConfig `yaml:"services"`
}

type ServerConfig struct {
	// Address to listen on (default ":8080")
	Addr string `yaml:"addr"`

	// Server name advertised via MCP (default "mcp-api-gateway")
	Name string `yaml:"name"`

	// Transport: "http" (default) or "stdio"
	Transport string `yaml:"transport"`

	// RequireConfirmation controls the write gate for mutating operations.
	// When true (default), POST/PUT/PATCH/DELETE require the agent to pass
	// confirmed=true; otherwise a confirmation prompt is returned instead.
	// When false, mutating operations execute immediately and the confirmed
	// parameter is omitted from the call_operation tool interface entirely.
	RequireConfirmation bool `yaml:"require_confirmation"`

	// OpenTelemetry configuration
	Telemetry TelemetryConfig `yaml:"telemetry"`
}

type TelemetryConfig struct {
	// Enable tracing (default false)
	Enabled bool `yaml:"enabled"`

	// OTLP gRPC endpoint (default "localhost:4317")
	Endpoint string `yaml:"endpoint"`

	// Sample ratio 0.0-1.0 (default 1.0 = sample everything)
	SampleRatio float64 `yaml:"sample_ratio"`
}

type ServiceConfig struct {
	// Logical name for this service (used in list_services, list_operations, etc.)
	Name string `yaml:"name"`

	// Path to the OpenAPI spec file (YAML or JSON)
	SpecPath string `yaml:"spec"`

	// Override the target host for API calls.
	// If empty, uses the first server URL from the spec.
	Host string `yaml:"host,omitempty"`

	// Override the base path prepended to all operation paths.
	// If empty, uses the path from the spec's server URL.
	BasePath string `yaml:"base_path,omitempty"`

	// Allow list of operationIds. If non-empty, only these operations are exposed.
	// If empty/omitted, all operations in the spec are exposed.
	AllowOperations []string `yaml:"allow_operations,omitempty"`

	// Headers to ignore — these are stripped from tool schemas (not shown to the LLM)
	// and not sent in HTTP requests. Use for headers managed outside the agent
	// (e.g. auth headers injected by a gateway, or irrelevant tracing headers).
	// Case-insensitive matching.
	IgnoreHeaders []string `yaml:"ignore_headers,omitempty"`

	// Static headers injected into every HTTP request for this service.
	// These are never exposed to the LLM — they're server-side only.
	// Use for auth tokens, API keys, tenant IDs, etc.
	Headers map[string]string `yaml:"headers,omitempty"`

	// RequireConfirmation controls the write gate for this service's mutating operations.
	// When nil (field absent in config), the server-level require_confirmation setting is used.
	// When true, POST/PUT/PATCH/DELETE require confirmed=true or a confirmation prompt is returned.
	// When false, mutating operations execute immediately without a confirmation step.
	RequireConfirmation *bool `yaml:"require_confirmation,omitempty"`
}

// LoadConfig reads and parses the YAML config file.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}

	cfg := &Config{
		Server: ServerConfig{
			Addr:                ":8080",
			Name:                "mcp-api-gateway",
			Transport:           "http",
			RequireConfirmation: true,
			Telemetry: TelemetryConfig{
				Endpoint:    "localhost:4317",
				SampleRatio: 1.0,
			},
		},
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}

	// Validate
	if len(cfg.Services) == 0 {
		return nil, fmt.Errorf("config %s: no services defined", path)
	}
	for i, svc := range cfg.Services {
		if svc.Name == "" {
			return nil, fmt.Errorf("config %s: service[%d] missing name", path, i)
		}
		if svc.SpecPath == "" {
			return nil, fmt.Errorf("config %s: service %q missing spec path", path, svc.Name)
		}
	}

	return cfg, nil
}
