package mcp

import (
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mobanhawi/ruleengine"
)

// EvaluateRuleInput is the input schema for the evaluate_rule tool.
type EvaluateRuleInput struct {
	RuleName string         `json:"rule_name" jsonschema:"Rule ID to evaluate"`
	Input    map[string]any `json:"input" jsonschema:"Evaluation input variables"`
}

// EvaluateRulesetInput is the input schema for the evaluate_ruleset tool.
type EvaluateRulesetInput struct {
	RulesetName string         `json:"ruleset_name" jsonschema:"Ruleset ID to evaluate"`
	Input       map[string]any `json:"input" jsonschema:"Evaluation input variables"`
}

// EvaluateAllRulesetsInput is the input schema for the evaluate_all_rulesets tool.
type EvaluateAllRulesetsInput struct {
	Input map[string]any `json:"input" jsonschema:"Evaluation input variables"`
}

// ValidateExpressionInput is the input schema for the validate_expression tool.
type ValidateExpressionInput struct {
	Expression string `json:"expression" jsonschema:"CEL boolean expression to compile and type-check"`
}

// EmptyInput is used by read-only listing tools that take no arguments.
type EmptyInput struct{}

// UpsertRuleInput is the input schema for the upsert_rule tool.
type UpsertRuleInput struct {
	ID          string   `json:"id" jsonschema:"Unique rule identifier"`
	Name        string   `json:"name,omitempty" jsonschema:"Human-readable rule name"`
	Description string   `json:"description,omitempty" jsonschema:"Rule description"`
	Expression  string   `json:"expression" jsonschema:"CEL boolean expression"`
	Extends     string   `json:"extends,omitempty" jsonschema:"Optional parent rule ID"`
	Tags        []string `json:"tags,omitempty" jsonschema:"Optional tags for categorization"`
}

// UpsertRulesetInput is the input schema for the upsert_ruleset tool.
type UpsertRulesetInput struct {
	ID          string                  `json:"id" jsonschema:"Unique ruleset identifier"`
	Name        string                  `json:"name,omitempty" jsonschema:"Human-readable ruleset name"`
	Description string                  `json:"description,omitempty" jsonschema:"Ruleset description"`
	Selector    ruleengine.SelectorType `json:"selector,omitempty" jsonschema:"Logical combinator (AND, OR, or NONE; mutually exclusive with min_passed)"`
	MinPassed   int                     `json:"min_passed,omitempty" jsonschema:"Minimum passing rules threshold (mutually exclusive with selector)"`
	Rules       []string                `json:"rules" jsonschema:"Member rule IDs"`
	Tags        []string                `json:"tags,omitempty" jsonschema:"Optional tags for categorization"`
}

func (s *Server) registerTools() {
	mcpsdk.AddTool(s.mcpServer, &mcpsdk.Tool{
		Name:        "evaluate_rule",
		Description: "Evaluate a single CEL rule by ID against an input map. Results include eval_error when the rule could not be evaluated.",
	}, s.handleEvaluateRule)

	mcpsdk.AddTool(s.mcpServer, &mcpsdk.Tool{
		Name:        "evaluate_ruleset",
		Description: "Evaluate a ruleset by ID against an input map.",
	}, s.handleEvaluateRuleset)

	mcpsdk.AddTool(s.mcpServer, &mcpsdk.Tool{
		Name:        "evaluate_all_rulesets",
		Description: "Evaluate all configured rulesets against an input map.",
	}, s.handleEvaluateAllRulesets)

	mcpsdk.AddTool(s.mcpServer, &mcpsdk.Tool{
		Name:        "validate_expression",
		Description: "Compile and type-check a candidate CEL boolean expression.",
	}, s.handleValidateExpression)

	mcpsdk.AddTool(s.mcpServer, &mcpsdk.Tool{
		Name:        "list_rules",
		Description: "List all configured rules and their CEL expressions.",
	}, s.handleListRules)

	mcpsdk.AddTool(s.mcpServer, &mcpsdk.Tool{
		Name:        "list_rulesets",
		Description: "List all configured rulesets and their combinator selectors.",
	}, s.handleListRulesets)

	if !s.allowAuthoring {
		return
	}

	mcpsdk.AddTool(s.mcpServer, &mcpsdk.Tool{
		Name:        "upsert_rule",
		Description: "Create or update a CEL rule at runtime." + notPersistedNote,
	}, s.handleUpsertRule)

	mcpsdk.AddTool(s.mcpServer, &mcpsdk.Tool{
		Name:        "upsert_ruleset",
		Description: "Create or update a ruleset at runtime. Set either selector or min_passed, not both." + notPersistedNote,
	}, s.handleUpsertRuleset)
}
