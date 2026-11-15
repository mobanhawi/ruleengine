package mcp

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mobanhawi/ruleengine"
)

const (
	testRuleID       = "age_check"
	testChildRuleID  = "verified_age_check"
	testRulesetID    = "registration"
	testNewRulesetID = "verified_registration"
	testValidMinAge  = 18
	testPassingAge   = 21
	testFailingAge   = 14

	readOnlyToolCount = 6
	fullToolCount     = 8
)

func newTestServer(t *testing.T, opts ...Option) *Server {
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
	return NewServer(engine, opts...)
}

func connectTestSession(t *testing.T, srv *Server) *mcpsdk.ClientSession {
	t.Helper()
	ctx := context.Background()
	serverTransport, clientTransport := mcpsdk.NewInMemoryTransports()

	serverSession, err := srv.MCPServer().Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect() error = %v", err)
	}
	t.Cleanup(func() {
		_ = serverSession.Close()
	})

	client := mcpsdk.NewClient(&mcpsdk.Implementation{
		Name:    "test-mcp-client",
		Version: "1.0.0",
	}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect() error = %v", err)
	}
	t.Cleanup(func() {
		_ = clientSession.Close()
	})

	return clientSession
}

func TestListTools(t *testing.T) {
	t.Run("GivenDefaultReadOnlyServer_WhenListTools_ThenExposesSixReadOnlyTools", func(t *testing.T) {
		srv := newTestServer(t)
		session := connectTestSession(t, srv)

		res, err := session.ListTools(context.Background(), nil)
		if err != nil {
			t.Fatalf("ListTools() error = %v", err)
		}
		if len(res.Tools) != readOnlyToolCount {
			t.Fatalf("ListTools() count = %d, want %d", len(res.Tools), readOnlyToolCount)
		}
		for _, tool := range res.Tools {
			if tool.Name == "upsert_rule" || tool.Name == "upsert_ruleset" {
				t.Errorf("read-only server exposed authoring tool %q", tool.Name)
			}
		}
	})

	t.Run("GivenServerWithAuthoringEnabled_WhenListTools_ThenExposesAllEightTools", func(t *testing.T) {
		srv := newTestServer(t, WithAuthoring())
		session := connectTestSession(t, srv)

		res, err := session.ListTools(context.Background(), nil)
		if err != nil {
			t.Fatalf("ListTools() error = %v", err)
		}
		if len(res.Tools) != fullToolCount {
			t.Fatalf("ListTools() count = %d, want %d", len(res.Tools), fullToolCount)
		}
	})
}

func TestCallTool(t *testing.T) {
	t.Run("GivenReadOnlyServer_WhenCallingUpsertRule_ThenReturnsProtocolError", func(t *testing.T) {
		srv := newTestServer(t)
		session := connectTestSession(t, srv)

		_, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
			Name: "upsert_rule",
			Arguments: map[string]any{
				"id":         testChildRuleID,
				"expression": "true",
			},
		})
		if err == nil {
			t.Fatal("CallTool(upsert_rule) on read-only server expected error, got nil")
		}
	})

	t.Run("GivenUnknownToolName_WhenCallingTool_ThenReturnsProtocolError", func(t *testing.T) {
		srv := newTestServer(t)
		session := connectTestSession(t, srv)

		_, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
			Name:      "nonexistent_tool",
			Arguments: map[string]any{},
		})
		if err == nil {
			t.Fatal("CallTool(nonexistent_tool) expected protocol error, got nil")
		}
	})

	t.Run("GivenListRulesTool_WhenCalled_ThenReturnsConfiguredRules", func(t *testing.T) {
		srv := newTestServer(t)
		session := connectTestSession(t, srv)

		res, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
			Name:      "list_rules",
			Arguments: map[string]any{},
		})
		if err != nil {
			t.Fatalf("CallTool(list_rules) error = %v", err)
		}
		if !strings.Contains(extractToolText(t, res), testRuleID) {
			t.Errorf("list_rules output = %s, want %s", extractToolText(t, res), testRuleID)
		}
	})

	t.Run("GivenListRulesetsTool_WhenCalled_ThenReturnsConfiguredRulesets", func(t *testing.T) {
		srv := newTestServer(t)
		session := connectTestSession(t, srv)

		res, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
			Name:      "list_rulesets",
			Arguments: map[string]any{},
		})
		if err != nil {
			t.Fatalf("CallTool(list_rulesets) error = %v", err)
		}
		if !strings.Contains(extractToolText(t, res), testRulesetID) {
			t.Errorf("list_rulesets output = %s, want %s", extractToolText(t, res), testRulesetID)
		}
	})

	t.Run("GivenEvaluateRuleTool_WhenInputPasses_ThenReturnsPassedTrue", func(t *testing.T) {
		srv := newTestServer(t)
		session := connectTestSession(t, srv)

		res, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
			Name: "evaluate_rule",
			Arguments: map[string]any{
				"rule_name": testRuleID,
				"input": map[string]any{
					"user": map[string]any{"age": testPassingAge},
				},
			},
		})
		if err != nil {
			t.Fatalf("CallTool(evaluate_rule) error = %v", err)
		}
		if res.IsError {
			t.Fatalf("unexpected tool error: %+v", res)
		}
		text := extractToolText(t, res)
		if !strings.Contains(text, `"passed":true`) {
			t.Errorf("tool output = %s, expected passed:true", text)
		}
	})

	t.Run("GivenEvaluateRulesetTool_WhenInputFails_ThenReturnsPassedFalse", func(t *testing.T) {
		srv := newTestServer(t)
		session := connectTestSession(t, srv)

		res, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
			Name: "evaluate_ruleset",
			Arguments: map[string]any{
				"ruleset_name": testRulesetID,
				"input": map[string]any{
					"user": map[string]any{"age": testFailingAge},
				},
			},
		})
		if err != nil {
			t.Fatalf("CallTool(evaluate_ruleset) error = %v", err)
		}
		text := extractToolText(t, res)
		if !strings.Contains(text, `"passed":false`) {
			t.Errorf("tool output = %s, expected passed:false", text)
		}
	})

	t.Run("GivenEvaluateAllRulesetsTool_WhenCalled_ThenReturnsAllRulesetResults", func(t *testing.T) {
		srv := newTestServer(t)
		session := connectTestSession(t, srv)

		res, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
			Name: "evaluate_all_rulesets",
			Arguments: map[string]any{
				"input": map[string]any{
					"user": map[string]any{"age": testPassingAge},
				},
			},
		})
		if err != nil {
			t.Fatalf("CallTool(evaluate_all_rulesets) error = %v", err)
		}
		text := extractToolText(t, res)
		if !strings.Contains(text, testRulesetID) {
			t.Errorf("tool output = %s, expected ruleset %s", text, testRulesetID)
		}
	})

	t.Run("GivenValidateExpressionTool_WhenExpressionIsValidBool_ThenReturnsValidTrue", func(t *testing.T) {
		srv := newTestServer(t)
		session := connectTestSession(t, srv)

		res, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
			Name: "validate_expression",
			Arguments: map[string]any{
				"expression": "user.age >= globals.min_age",
			},
		})
		if err != nil {
			t.Fatalf("CallTool(validate_expression) error = %v", err)
		}
		if res.IsError {
			t.Fatalf("expected valid expression, got tool error: %+v", res)
		}
	})

	t.Run("GivenAuthoringEnabled_WhenUpsertRuleCalled_ThenRegistersRuleInMemory", func(t *testing.T) {
		srv := newTestServer(t, WithAuthoring())
		session := connectTestSession(t, srv)
		ctx := context.Background()

		ruleRes, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
			Name: "upsert_rule",
			Arguments: map[string]any{
				"id":         testChildRuleID,
				"name":       "Verified Age Check",
				"extends":    testRuleID,
				"expression": "user.verified == true",
				"tags":       []string{"auth", "kyc"},
			},
		})
		if err != nil || ruleRes.IsError {
			t.Fatalf("CallTool(upsert_rule) res=%+v, err=%v", ruleRes, err)
		}
		if !strings.Contains(extractToolText(t, ruleRes), `"persisted":false`) {
			t.Errorf("upsert_rule output missing persisted:false indicator: %s", extractToolText(t, ruleRes))
		}

		listRulesRes, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
			Name:      "list_rules",
			Arguments: map[string]any{},
		})
		if err != nil || !strings.Contains(extractToolText(t, listRulesRes), testChildRuleID) {
			t.Fatalf("list_rules output missing %s: %v", testChildRuleID, err)
		}
	})

	t.Run("GivenAuthoringEnabled_WhenUpsertRulesetCalled_ThenRegistersRulesetInMemory", func(t *testing.T) {
		srv := newTestServer(t, WithAuthoring())
		session := connectTestSession(t, srv)
		ctx := context.Background()

		rsRes, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
			Name: "upsert_ruleset",
			Arguments: map[string]any{
				"id":       testNewRulesetID,
				"name":     "Verified Registration",
				"selector": "AND",
				"rules":    []string{testRuleID},
				"tags":     []string{"auth"},
			},
		})
		if err != nil || rsRes.IsError {
			t.Fatalf("CallTool(upsert_ruleset) res=%+v, err=%v", rsRes, err)
		}
		if !strings.Contains(extractToolText(t, rsRes), `"persisted":false`) {
			t.Errorf("upsert_ruleset output missing persisted:false indicator: %s", extractToolText(t, rsRes))
		}

		listRulesetsRes, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
			Name:      "list_rulesets",
			Arguments: map[string]any{},
		})
		if err != nil || !strings.Contains(extractToolText(t, listRulesetsRes), testNewRulesetID) {
			t.Fatalf("list_rulesets output missing %s: %v", testNewRulesetID, err)
		}
	})
}

func TestServe(t *testing.T) {
	t.Run("GivenBlockingInputStream_WhenContextIsCancelled_ThenServeClosesReaderAndUnblocks", func(t *testing.T) {
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
		if err == nil {
			t.Fatal("Serve() expected error on cancelled context, got nil")
		}
	})
}

func extractToolText(t *testing.T, res *mcpsdk.CallToolResult) string {
	t.Helper()
	if len(res.Content) == 0 {
		t.Fatalf("empty Content in CallToolResult: %+v", res)
	}
	textContent, ok := res.Content[0].(*mcpsdk.TextContent)
	if !ok {
		t.Fatalf("unexpected content type %T", res.Content[0])
	}
	return textContent.Text
}
