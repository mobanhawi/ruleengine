package mcp

import (
	"context"
	"fmt"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mobanhawi/ruleengine"
)

func (s *Server) handleEvaluateRule(
	ctx context.Context,
	_ *mcpsdk.CallToolRequest,
	in EvaluateRuleInput,
) (*mcpsdk.CallToolResult, any, error) {
	res, err := s.engine.EvaluateRule(ctx, in.RuleName, in.Input)
	if err != nil {
		return nil, nil, fmt.Errorf("evaluate_rule failed: %w", err)
	}
	return nil, res, nil
}

func (s *Server) handleEvaluateRuleset(
	ctx context.Context,
	_ *mcpsdk.CallToolRequest,
	in EvaluateRulesetInput,
) (*mcpsdk.CallToolResult, any, error) {
	res, err := s.engine.EvaluateRuleset(ctx, in.RulesetName, in.Input)
	if err != nil {
		return nil, nil, fmt.Errorf("evaluate_ruleset failed: %w", err)
	}
	return nil, res, nil
}

func (s *Server) handleEvaluateAllRulesets(
	ctx context.Context,
	_ *mcpsdk.CallToolRequest,
	in EvaluateAllRulesetsInput,
) (*mcpsdk.CallToolResult, any, error) {
	res, err := s.engine.EvaluateAllRulesets(ctx, in.Input)
	if err != nil {
		return nil, nil, fmt.Errorf("evaluate_all_rulesets failed: %w", err)
	}
	return nil, res, nil
}

func (s *Server) handleValidateExpression(
	_ context.Context,
	_ *mcpsdk.CallToolRequest,
	in ValidateExpressionInput,
) (*mcpsdk.CallToolResult, any, error) {
	if err := s.engine.ValidateExpression(in.Expression); err != nil {
		return nil, nil, fmt.Errorf("invalid expression: %w", err)
	}
	return nil, map[string]any{"valid": true, "expression": in.Expression}, nil
}

func (s *Server) handleListRules(
	_ context.Context,
	_ *mcpsdk.CallToolRequest,
	_ EmptyInput,
) (*mcpsdk.CallToolResult, any, error) {
	return nil, s.engine.ListRules(), nil
}

func (s *Server) handleListRulesets(
	_ context.Context,
	_ *mcpsdk.CallToolRequest,
	_ EmptyInput,
) (*mcpsdk.CallToolResult, any, error) {
	return nil, s.engine.ListRulesets(), nil
}

func (s *Server) handleUpsertRule(
	_ context.Context,
	_ *mcpsdk.CallToolRequest,
	in UpsertRuleInput,
) (*mcpsdk.CallToolResult, any, error) {
	err := s.engine.UpsertRule(in.ID, ruleengine.Rule{
		Name:        in.Name,
		Description: in.Description,
		Expression:  in.Expression,
		Extends:     in.Extends,
		Tags:        in.Tags,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("upsert_rule failed: %w", err)
	}
	return nil, map[string]any{"status": "upserted", "id": in.ID, "persisted": false}, nil
}

func (s *Server) handleUpsertRuleset(
	_ context.Context,
	_ *mcpsdk.CallToolRequest,
	in UpsertRulesetInput,
) (*mcpsdk.CallToolResult, any, error) {
	err := s.engine.UpsertRuleset(in.ID, ruleengine.Ruleset{
		Name:        in.Name,
		Description: in.Description,
		Selector:    in.Selector,
		MinPassed:   in.MinPassed,
		Rules:       in.Rules,
		Tags:        in.Tags,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("upsert_ruleset failed: %w", err)
	}
	return nil, map[string]any{"status": "upserted", "id": in.ID, "persisted": false}, nil
}
