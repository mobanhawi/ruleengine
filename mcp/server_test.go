package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/mobanhawi/ruleengine"
)

const (
	testProtocolVersion = "2025-03-26"
	testRuleID          = "age_check"
	testChildRuleID     = "verified_age_check"
	testRulesetID       = "registration"
	testValidMinAge     = 18
	testPassingAge      = 21
	testFailingAge      = 14
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	env, err := ruleengine.NewDefaultEnv("user", "request")
	if err != nil {
		t.Fatalf("NewDefaultEnv() error = %v", err)
	}
	cfg := &ruleengine.RulesetConfig{
		Globals: map[string]any{
			"min_age": testValidMinAge,
		},
		Rules: map[string]ruleengine.Rule{
			testRuleID: {
				Name:        "Age Check",
				Description: "Checks minimum user age",
				Expression:  "user.age >= globals.min_age",
			},
		},
		Rulesets: map[string]ruleengine.Ruleset{
			testRulesetID: {
				Name:     "Registration Ruleset",
				Selector: ruleengine.SelectorAnd,
				Rules:    []string{testRuleID},
			},
		},
	}
	engine, err := ruleengine.NewRuleEngineFromConfig(cfg, "", env, ruleengine.WithOptimise())
	if err != nil {
		t.Fatalf("NewRuleEngineFromConfig() error = %v", err)
	}
	return NewServer(engine)
}

func TestHandleRequest(t *testing.T) {
	tests := []struct {
		name           string
		request        map[string]any
		wantNoResponse bool
		wantErrCode    int
		verifyResult   func(*testing.T, map[string]any)
	}{
		{
			name: "GivenInitializeRequest_WhenHandleRequest_ThenReturnsServerCapabilities",
			request: map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"method":  "initialize",
				"params": map[string]any{
					"protocolVersion": testProtocolVersion,
				},
			},
			verifyResult: func(t *testing.T, res map[string]any) {
				if res["protocolVersion"] != testProtocolVersion {
					t.Errorf("protocolVersion = %v, want %s", res["protocolVersion"], testProtocolVersion)
				}
			},
		},
		{
			name: "GivenValidInitializedNotification_WhenHandleRequest_ThenProducesNoResponse",
			request: map[string]any{
				"jsonrpc": "2.0",
				"method":  "notifications/initialized",
			},
			wantNoResponse: true,
		},
		{
			name: "GivenMalformedMessageWithoutIDAndMissingMethod_WhenHandleRequest_ThenReturnsInvalidRequestError",
			request: map[string]any{
				"jsonrpc": "2.0",
			},
			wantErrCode: errCodeInvalidRequest,
		},
		{
			name: "GivenMalformedMessageWithoutIDAndInvalidVersion_WhenHandleRequest_ThenReturnsInvalidRequestError",
			request: map[string]any{
				"jsonrpc": "1.0",
				"method":  "notifications/initialized",
			},
			wantErrCode: errCodeInvalidRequest,
		},
		{
			name: "GivenToolsListRequest_WhenHandleRequest_ThenReturnsAllEightToolsWithTagsSchemas",
			request: map[string]any{
				"jsonrpc": "2.0",
				"id":      2,
				"method":  "tools/list",
			},
			verifyResult: func(t *testing.T, res map[string]any) {
				tools, ok := res["tools"].([]any)
				if !ok || len(tools) != 8 {
					t.Fatalf("tools count = %d, want 8", len(tools))
				}
				for _, item := range tools {
					toolMap, _ := item.(map[string]any)
					name, _ := toolMap["name"].(string)
					if name == "upsert_rule" || name == "upsert_ruleset" {
						schema, _ := toolMap["inputSchema"].(map[string]any)
						props, _ := schema["properties"].(map[string]any)
						if _, hasTags := props["tags"]; !hasTags {
							t.Errorf("tool %q inputSchema.properties is missing 'tags'", name)
						}
					}
				}
			},
		},
		{
			name: "GivenEvaluateRuleToolCall_WhenInputPasses_ThenReturnsPassedTrue",
			request: map[string]any{
				"jsonrpc": "2.0",
				"id":      3,
				"method":  "tools/call",
				"params": map[string]any{
					"name": "evaluate_rule",
					"arguments": map[string]any{
						"rule_name": testRuleID,
						"input": map[string]any{
							"user": map[string]any{"age": testPassingAge},
						},
					},
				},
			},
			verifyResult: func(t *testing.T, res map[string]any) {
				if isErr, _ := res["isError"].(bool); isErr {
					t.Fatalf("unexpected tool error: %+v", res)
				}
				text := extractToolText(t, res)
				if !strings.Contains(text, `"passed":true`) {
					t.Errorf("tool output = %s, expected passed:true", text)
				}
			},
		},
		{
			name: "GivenEvaluateRulesetToolCall_WhenInputFails_ThenReturnsPassedFalse",
			request: map[string]any{
				"jsonrpc": "2.0",
				"id":      4,
				"method":  "tools/call",
				"params": map[string]any{
					"name": "evaluate_ruleset",
					"arguments": map[string]any{
						"ruleset_name": testRulesetID,
						"input": map[string]any{
							"user": map[string]any{"age": testFailingAge},
						},
					},
				},
			},
			verifyResult: func(t *testing.T, res map[string]any) {
				text := extractToolText(t, res)
				if !strings.Contains(text, `"passed":false`) {
					t.Errorf("tool output = %s, expected passed:false", text)
				}
			},
		},
		{
			name: "GivenEvaluateAllRulesetsToolCall_WhenCalled_ThenReturnsMapOfResults",
			request: map[string]any{
				"jsonrpc": "2.0",
				"id":      5,
				"method":  "tools/call",
				"params": map[string]any{
					"name": "evaluate_all_rulesets",
					"arguments": map[string]any{
						"input": map[string]any{
							"user": map[string]any{"age": testPassingAge},
						},
					},
				},
			},
			verifyResult: func(t *testing.T, res map[string]any) {
				text := extractToolText(t, res)
				if !strings.Contains(text, testRulesetID) {
					t.Errorf("tool output = %s, expected ruleset %s", text, testRulesetID)
				}
			},
		},
		{
			name: "GivenValidateExpressionToolCall_WhenExpressionIsValidBool_ThenReturnsValidTrue",
			request: map[string]any{
				"jsonrpc": "2.0",
				"id":      6,
				"method":  "tools/call",
				"params": map[string]any{
					"name": "validate_expression",
					"arguments": map[string]any{
						"expression": "user.age >= globals.min_age",
					},
				},
			},
			verifyResult: func(t *testing.T, res map[string]any) {
				if isErr, _ := res["isError"].(bool); isErr {
					t.Fatalf("expected valid expression, got error: %+v", res)
				}
			},
		},
		{
			name: "GivenUpsertRuleAndUpsertRulesetToolCalls_WhenValid_ThenRegistersInEngine",
			request: map[string]any{
				"jsonrpc": "2.0",
				"id":      7,
				"method":  "tools/call",
				"params": map[string]any{
					"name": "upsert_rule",
					"arguments": map[string]any{
						"id":         testChildRuleID,
						"name":       "Verified Age Check",
						"extends":    testRuleID,
						"expression": "user.verified == true",
						"tags":       []string{"auth", "kyc"},
					},
				},
			},
			verifyResult: func(t *testing.T, res map[string]any) {
				if isErr, _ := res["isError"].(bool); isErr {
					t.Fatalf("upsert_rule failed: %+v", res)
				}
			},
		},
		{
			name: "GivenListRulesAndRulesetsToolCalls_WhenCalled_ThenReturnsConfiguredDefinitions",
			request: map[string]any{
				"jsonrpc": "2.0",
				"id":      8,
				"method":  "tools/call",
				"params": map[string]any{
					"name":      "list_rules",
					"arguments": map[string]any{},
				},
			},
			verifyResult: func(t *testing.T, res map[string]any) {
				text := extractToolText(t, res)
				if !strings.Contains(text, testRuleID) {
					t.Errorf("list_rules output = %s, expected %s", text, testRuleID)
				}
			},
		},
		{
			name: "GivenUnknownMethod_WhenHandleRequest_ThenReturnsMethodNotFoundError",
			request: map[string]any{
				"jsonrpc": "2.0",
				"id":      9,
				"method":  "unknown/method",
			},
			wantErrCode: errCodeMethodNotFound,
		},
	}

	srv := newTestServer(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, err := json.Marshal(tt.request)
			if err != nil {
				t.Fatalf("json.Marshal() error = %v", err)
			}
			respBytes, err := srv.HandleRequest(context.Background(), raw)
			if err != nil {
				t.Fatalf("HandleRequest() unexpected error = %v", err)
			}
			if tt.wantNoResponse {
				if len(respBytes) != 0 {
					t.Fatalf("expected no response for notification, got %s", string(respBytes))
				}
				return
			}

			var resp struct {
				Result map[string]any `json:"result"`
				Error  *struct {
					Code    int    `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal(respBytes, &resp); err != nil {
				t.Fatalf("json.Unmarshal(response) error = %v", err)
			}

			if tt.wantErrCode != 0 {
				if resp.Error == nil || resp.Error.Code != tt.wantErrCode {
					t.Fatalf("got error = %+v, want code %d", resp.Error, tt.wantErrCode)
				}
				return
			}

			if resp.Error != nil {
				t.Fatalf("unexpected RPC error: %+v", resp.Error)
			}
			if tt.verifyResult != nil {
				tt.verifyResult(t, resp.Result)
			}
		})
	}
}

func TestServe(t *testing.T) {
	t.Run("GivenJSONRPCStream_WhenServe_ThenProcessesLinesUntilEOF", func(t *testing.T) {
		srv := newTestServer(t)
		inputLines := strings.Join([]string{
			`{"jsonrpc":"2.0","id":1,"method":"ping"}`,
			`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
			`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"list_rulesets","arguments":{}}}`,
		}, "\n") + "\n"

		var out bytes.Buffer
		if err := srv.Serve(context.Background(), strings.NewReader(inputLines), &out); err != nil {
			t.Fatalf("Serve() error = %v", err)
		}

		lines := strings.Split(strings.TrimSpace(out.String()), "\n")
		if len(lines) != 2 {
			t.Fatalf("got %d response lines, want 2: %v", len(lines), lines)
		}
	})

	t.Run("GivenBlockingInputStream_WhenContextIsCancelled_ThenServeUnblocksImmediately", func(t *testing.T) {
		srv := newTestServer(t)
		pr, pw := io.Pipe()
		defer func() {
			_ = pw.Close()
			_ = pr.Close()
		}()

		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
		defer cancel()

		var out bytes.Buffer
		err := srv.Serve(ctx, pr, &out)
		if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
			t.Fatalf("Serve() error = %v, want context cancellation error", err)
		}
	})
}

func extractToolText(t *testing.T, res map[string]any) string {
	t.Helper()
	content, ok := res["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("missing content array in tool result: %+v", res)
	}
	first, ok := content[0].(map[string]any)
	if !ok {
		t.Fatalf("invalid content item: %+v", content[0])
	}
	text, _ := first["text"].(string)
	return text
}
