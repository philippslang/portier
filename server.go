package portier

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

// Server is a configured portier MCP gateway. Create one with NewServer,
// then call Run to start accepting connections.
type Server struct {
	cfg          *Config
	registry     *Registry
	mcp          *mcpserver.MCPServer
	otelShutdown func(context.Context) error // nil if telemetry disabled
}

// NewServer builds a Server from a Config. It loads all OpenAPI specs and
// initializes telemetry. Call Run to start serving.
func NewServer(cfg *Config) (*Server, error) {
	var otelShutdown func(context.Context) error
	if cfg.Server.Telemetry.Enabled {
		shutdown, err := initTracer(context.Background(), cfg.Server.Telemetry, cfg.Server.Name)
		if err != nil {
			return nil, fmt.Errorf("initializing tracing: %w", err)
		}
		otelShutdown = shutdown
	}

	httpClient := &http.Client{}
	if cfg.Server.Telemetry.Enabled {
		httpClient.Transport = otelhttp.NewTransport(http.DefaultTransport)
	}

	reg := NewRegistry(httpClient)
	for _, svcCfg := range cfg.Services {
		if err := reg.LoadSpec(svcCfg); err != nil {
			if otelShutdown != nil {
				_ = otelShutdown(context.Background())
			}
			return nil, fmt.Errorf("loading service %q: %w", svcCfg.Name, err)
		}
	}

	s := mcpserver.NewMCPServer(
		cfg.Server.Name,
		"1.0.0",
		mcpserver.WithToolCapabilities(false),
		mcpserver.WithRecovery(),
	)
	RegisterTools(s, reg)

	return &Server{
		cfg:          cfg,
		registry:     reg,
		mcp:          s,
		otelShutdown: otelShutdown,
	}, nil
}

// NewServerFromFile loads a YAML config file and creates a Server.
func NewServerFromFile(path string) (*Server, error) {
	cfg, err := LoadConfig(path)
	if err != nil {
		return nil, err
	}
	return NewServer(cfg)
}

// Run starts the configured transport and blocks until the server exits.
func (s *Server) Run(ctx context.Context) error {
	if s.otelShutdown != nil {
		defer func() {
			if err := s.otelShutdown(context.Background()); err != nil {
				log.Printf("Error shutting down tracer: %v", err)
			}
		}()
	}

	if s.cfg.Server.Transport == "stdio" {
		log.Printf("Starting MCP server %q over stdio", s.cfg.Server.Name)
		return mcpserver.ServeStdio(s.mcp)
	}

	httpServer := mcpserver.NewStreamableHTTPServer(s.mcp)
	log.Printf("Starting MCP server %q on %s (streamable HTTP)", s.cfg.Server.Name, s.cfg.Server.Addr)
	return httpServer.Start(s.cfg.Server.Addr)
}

// Registry returns the underlying service registry for direct programmatic access.
func (s *Server) Registry() *Registry { return s.registry }

// MCPServer returns the underlying MCP server, allowing additional tools to be
// registered before calling Run.
func (s *Server) MCPServer() *mcpserver.MCPServer { return s.mcp }
