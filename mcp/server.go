package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/mobanhawi/ruleengine"
)

const (
	jsonrpcVersion         = "2.0"
	defaultProtocolVersion = "2025-03-26"
	serverName             = "ruleengine-mcp"
	serverVersion          = "1.0.0"
	maxScannerBufferSize   = 4 * 1024 * 1024

	errCodeParseError     = -32700
	errCodeInvalidRequest = -32600
	errCodeMethodNotFound = -32601
	errCodeInvalidParams  = -32602
)

// Server exposes a RuleEngine over the Model Context Protocol (MCP) JSON-RPC 2.0 transport.
type Server struct {
	engine *ruleengine.RuleEngine
}

// NewServer creates a new MCP Server backed by the provided RuleEngine.
func NewServer(engine *ruleengine.RuleEngine) *Server {
	return &Server{engine: engine}
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type toolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type callToolParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type toolResult struct {
	Content []toolContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

type toolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// Serve reads newline-delimited JSON-RPC 2.0 requests from in and writes responses to out until EOF or context cancellation.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), maxScannerBufferSize)

	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("mcp server context cancelled: %w", err)
		}

		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}

		resp, err := s.HandleRequest(ctx, line)
		if err != nil {
			return fmt.Errorf("handling mcp request: %w", err)
		}
		if len(resp) == 0 {
			continue
		}

		if _, err := out.Write(append(resp, '\n')); err != nil {
			return fmt.Errorf("writing mcp response: %w", err)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("reading mcp input stream: %w", err)
	}
	return nil
}

// HandleRequest processes a single JSON-RPC 2.0 message and returns the serialized JSON-RPC response.
// For JSON-RPC notifications (requests without an "id"), HandleRequest returns (nil, nil).
func (s *Server) HandleRequest(ctx context.Context, raw []byte) ([]byte, error) {
	var req rpcRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return marshalError(json.RawMessage("null"), errCodeParseError, fmt.Sprintf("invalid JSON: %v", err))
	}

	// JSON-RPC notifications omit "id" and must not receive a response.
	if len(req.ID) == 0 {
		return nil, nil
	}

	if req.JSONRPC != jsonrpcVersion || req.Method == "" {
		return marshalError(req.ID, errCodeInvalidRequest, "invalid JSON-RPC 2.0 request")
	}

	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "ping":
		return marshalResult(req.ID, map[string]any{})
	case "tools/list":
		return marshalResult(req.ID, map[string]any{
			"tools": s.toolDefinitions(),
		})
	case "tools/call":
		return s.handleToolCall(ctx, req)
	default:
		return marshalError(req.ID, errCodeMethodNotFound, fmt.Sprintf("method %q not found", req.Method))
	}
}

func (s *Server) handleInitialize(req rpcRequest) ([]byte, error) {
	protocolVersion := defaultProtocolVersion
	if len(req.Params) > 0 {
		var initParams struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		if err := json.Unmarshal(req.Params, &initParams); err == nil && initParams.ProtocolVersion != "" {
			protocolVersion = initParams.ProtocolVersion
		}
	}

	return marshalResult(req.ID, map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities": map[string]any{
			"tools": map[string]any{},
		},
		"serverInfo": map[string]any{
			"name":    serverName,
			"version": serverVersion,
		},
	})
}

func (s *Server) handleToolCall(ctx context.Context, req rpcRequest) ([]byte, error) {
	var params callToolParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return marshalError(req.ID, errCodeInvalidParams, fmt.Sprintf("invalid tools/call params: %v", err))
	}

	res := s.dispatchTool(ctx, params.Name, params.Arguments)
	return marshalResult(req.ID, res)
}

func (s *Server) dispatchTool(ctx context.Context, name string, rawArgs json.RawMessage) toolResult {
	switch name {
	case "evaluate_rule":
		var args struct {
			RuleName string         `json:"rule_name"`
			Input    map[string]any `json:"input"`
		}
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return errorToolResult("invalid arguments for evaluate_rule: %v", err)
		}
		res, err := s.engine.EvaluateRule(ctx, args.RuleName, args.Input)
		if err != nil {
			return errorToolResult("evaluate_rule failed: %v", err)
		}
		return jsonToolResult(res)

	case "evaluate_ruleset":
		var args struct {
			RulesetName string         `json:"ruleset_name"`
			Input       map[string]any `json:"input"`
		}
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return errorToolResult("invalid arguments for evaluate_ruleset: %v", err)
		}
		res, err := s.engine.EvaluateRuleset(ctx, args.RulesetName, args.Input)
		if err != nil {
			return errorToolResult("evaluate_ruleset failed: %v", err)
		}
		return jsonToolResult(res)

	case "evaluate_all_rulesets":
		var args struct {
			Input map[string]any `json:"input"`
		}
		if len(rawArgs) > 0 {
			if err := json.Unmarshal(rawArgs, &args); err != nil {
				return errorToolResult("invalid arguments for evaluate_all_rulesets: %v", err)
			}
		}
		res, err := s.engine.EvaluateAllRulesets(ctx, args.Input)
		if err != nil {
			return errorToolResult("evaluate_all_rulesets failed: %v", err)
		}
		return jsonToolResult(res)

	case "validate_expression":
		var args struct {
			Expression string `json:"expression"`
		}
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return errorToolResult("invalid arguments for validate_expression: %v", err)
		}
		if err := s.engine.ValidateExpression(args.Expression); err != nil {
			return errorToolResult("invalid expression: %v", err)
		}
		return jsonToolResult(map[string]any{"valid": true, "expression": args.Expression})

	case "list_rules":
		return jsonToolResult(s.engine.ListRules())

	case "list_rulesets":
		return jsonToolResult(s.engine.ListRulesets())

	case "upsert_rule":
		var args struct {
			ID          string   `json:"id"`
			Name        string   `json:"name"`
			Description string   `json:"description"`
			Expression  string   `json:"expression"`
			Extends     string   `json:"extends"`
			Tags        []string `json:"tags"`
		}
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return errorToolResult("invalid arguments for upsert_rule: %v", err)
		}
		err := s.engine.UpsertRule(args.ID, ruleengine.Rule{
			Name:        args.Name,
			Description: args.Description,
			Expression:  args.Expression,
			Extends:     args.Extends,
			Tags:        args.Tags,
		})
		if err != nil {
			return errorToolResult("upsert_rule failed: %v", err)
		}
		return jsonToolResult(map[string]any{"status": "upserted", "id": args.ID})

	case "upsert_ruleset":
		var args struct {
			ID          string                  `json:"id"`
			Name        string                  `json:"name"`
			Description string                  `json:"description"`
			Selector    ruleengine.SelectorType `json:"selector"`
			MinPassed   int                     `json:"min_passed"`
			Rules       []string                `json:"rules"`
			Tags        []string                `json:"tags"`
		}
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return errorToolResult("invalid arguments for upsert_ruleset: %v", err)
		}
		err := s.engine.UpsertRuleset(args.ID, ruleengine.Ruleset{
			Name:        args.Name,
			Description: args.Description,
			Selector:    args.Selector,
			MinPassed:   args.MinPassed,
			Rules:       args.Rules,
			Tags:        args.Tags,
		})
		if err != nil {
			return errorToolResult("upsert_ruleset failed: %v", err)
		}
		return jsonToolResult(map[string]any{"status": "upserted", "id": args.ID})

	default:
		return errorToolResult("unknown tool %q", name)
	}
}

func (s *Server) toolDefinitions() []toolDefinition {
	return []toolDefinition{
		{
			Name:        "evaluate_rule",
			Description: "Evaluate a single CEL rule by ID against an input map.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"rule_name": map[string]any{"type": "string", "description": "Rule ID to evaluate"},
					"input":     map[string]any{"type": "object", "description": "Evaluation input variables"},
				},
				"required": []string{"rule_name", "input"},
			},
		},
		{
			Name:        "evaluate_ruleset",
			Description: "Evaluate a ruleset by ID against an input map.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"ruleset_name": map[string]any{"type": "string", "description": "Ruleset ID to evaluate"},
					"input":        map[string]any{"type": "object", "description": "Evaluation input variables"},
				},
				"required": []string{"ruleset_name", "input"},
			},
		},
		{
			Name:        "evaluate_all_rulesets",
			Description: "Evaluate all configured rulesets against an input map.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"input": map[string]any{"type": "object", "description": "Evaluation input variables"},
				},
				"required": []string{"input"},
			},
		},
		{
			Name:        "validate_expression",
			Description: "Compile and type-check a candidate CEL boolean expression.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"expression": map[string]any{"type": "string", "description": "CEL boolean expression"},
				},
				"required": []string{"expression"},
			},
		},
		{
			Name:        "list_rules",
			Description: "List all configured rules and their CEL expressions.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name:        "list_rulesets",
			Description: "List all configured rulesets and their combinator selectors.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name:        "upsert_rule",
			Description: "Create or update a CEL rule at runtime.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id":          map[string]any{"type": "string", "description": "Unique rule identifier"},
					"name":        map[string]any{"type": "string", "description": "Human-readable name"},
					"description": map[string]any{"type": "string", "description": "Rule description"},
					"expression":  map[string]any{"type": "string", "description": "CEL boolean expression"},
					"extends":     map[string]any{"type": "string", "description": "Optional parent rule ID"},
				},
				"required": []string{"id", "expression"},
			},
		},
		{
			Name:        "upsert_ruleset",
			Description: "Create or update a ruleset at runtime.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id":          map[string]any{"type": "string", "description": "Unique ruleset identifier"},
					"name":        map[string]any{"type": "string", "description": "Human-readable name"},
					"description": map[string]any{"type": "string", "description": "Ruleset description"},
					"selector":    map[string]any{"type": "string", "enum": []string{"AND", "OR", "NONE"}},
					"min_passed":  map[string]any{"type": "integer", "description": "Minimum passing rules threshold"},
					"rules":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				},
				"required": []string{"id", "rules"},
			},
		},
	}
}

func jsonToolResult(payload any) toolResult {
	data, err := json.Marshal(payload)
	if err != nil {
		return errorToolResult("marshaling tool result: %v", err)
	}
	return toolResult{
		Content: []toolContent{{Type: "text", Text: string(data)}},
	}
}

func errorToolResult(format string, args ...any) toolResult {
	return toolResult{
		Content: []toolContent{{Type: "text", Text: fmt.Sprintf(format, args...)}},
		IsError: true,
	}
}

func marshalResult(id json.RawMessage, result any) ([]byte, error) {
	return json.Marshal(rpcResponse{
		JSONRPC: jsonrpcVersion,
		ID:      id,
		Result:  result,
	})
}

func marshalError(id json.RawMessage, code int, msg string) ([]byte, error) {
	return json.Marshal(rpcResponse{
		JSONRPC: jsonrpcVersion,
		ID:      id,
		Error: &rpcError{
			Code:    code,
			Message: msg,
		},
	})
}
