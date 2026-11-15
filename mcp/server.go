package mcp

import (
	"context"
	"fmt"
	"io"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mobanhawi/ruleengine"
)

const (
	serverName    = "ruleengine-mcp"
	serverVersion = "1.0.0"

	notPersistedNote = " Changes are held in memory only and are lost when the server restarts."
)

// Engine defines the rule evaluation and management contract consumed by the MCP Server.
type Engine interface {
	EvaluateRule(ctx context.Context, ruleName string, input map[string]any) (ruleengine.RuleResult, error)
	EvaluateRuleset(ctx context.Context, rulesetName string, input map[string]any) (ruleengine.RulesetResult, error)
	EvaluateAllRulesets(ctx context.Context, input map[string]any) (map[string]ruleengine.RulesetResult, error)
	ValidateExpression(expression string) error
	UpsertRule(ruleID string, rule ruleengine.Rule) error
	UpsertRuleset(rulesetID string, ruleset ruleengine.Ruleset) error
	ListRules() map[string]ruleengine.Rule
	ListRulesets() map[string]ruleengine.Ruleset
}

// Server exposes a RuleEngine over the Model Context Protocol (MCP) using the official Go SDK.
// By default the server is read-only: rule authoring tools (upsert_rule, upsert_ruleset) are
// omitted unless WithAuthoring is passed to NewServer.
type Server struct {
	engine         Engine
	mcpServer      *mcpsdk.Server
	allowAuthoring bool
}

// Option configures a Server.
type Option func(*Server)

// WithAuthoring exposes the upsert_rule and upsert_ruleset tools, allowing connected
// clients to register or update rules and rulesets in memory.
func WithAuthoring() Option {
	return func(s *Server) {
		s.allowAuthoring = true
	}
}

// NewServer creates a new MCP Server backed by the provided Engine.
func NewServer(engine Engine, opts ...Option) *Server {
	s := &Server{
		engine: engine,
		mcpServer: mcpsdk.NewServer(&mcpsdk.Implementation{
			Name:    serverName,
			Version: serverVersion,
		}, nil),
	}
	for _, opt := range opts {
		opt(s)
	}
	s.registerTools()
	return s
}

// MCPServer returns the underlying official MCP SDK server for custom transports or HTTP handlers.
func (s *Server) MCPServer() *mcpsdk.Server {
	return s.mcpServer
}

// Run runs the MCP server over the provided SDK Transport until EOF or context cancellation.
func (s *Server) Run(ctx context.Context, transport mcpsdk.Transport) error {
	if err := s.mcpServer.Run(ctx, transport); err != nil {
		return fmt.Errorf("running mcp server: %w", err)
	}
	return nil
}

// ServeStdio runs the MCP server over standard input and standard output.
func (s *Server) ServeStdio(ctx context.Context) error {
	return s.Run(ctx, &mcpsdk.StdioTransport{})
}

// Serve runs the MCP server over arbitrary io.Reader and io.Writer streams.
// When ctx is cancelled, Serve closes in if it implements io.Closer (or sets an immediate read
// deadline if supported) so a blocked reader unblocks promptly.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	rc := toReadCloser(in)
	wc := toWriteCloser(out)

	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			if rd, ok := in.(interface{ SetReadDeadline(time.Time) error }); ok {
				_ = rd.SetReadDeadline(time.Now())
			}
			_ = rc.Close()
		case <-done:
		}
	}()

	return s.Run(ctx, &mcpsdk.IOTransport{
		Reader: rc,
		Writer: wc,
	})
}

type nopWriteCloser struct {
	io.Writer
}

func (nopWriteCloser) Close() error { return nil }

func toReadCloser(r io.Reader) io.ReadCloser {
	if rc, ok := r.(io.ReadCloser); ok {
		return rc
	}
	return io.NopCloser(r)
}

func toWriteCloser(w io.Writer) io.WriteCloser {
	if wc, ok := w.(io.WriteCloser); ok {
		return wc
	}
	return nopWriteCloser{Writer: w}
}
