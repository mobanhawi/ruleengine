# CEL Rule Engine

A concurrency-safe, configurable rule engine in Go powered by [CEL (Common Expression Language)](https://opensource.google/projects/cel) with YAML configuration, programmatic rule management, and a built-in Model Context Protocol (MCP) server.

## Features

- **Stateless, Concurrency-Safe Evaluation**: Evaluate rules and rulesets safely across concurrent goroutines with `context.Context` cancellation and timeout propagation.
- **YAML & Programmatic Configuration**: Load rules from YAML files, `io.Reader`, `[]byte`, or construct/mutate rules and rulesets dynamically at runtime.
- **Rule Inheritance (`extends`)**: Compose hierarchical rules evaluated top-down from root ancestor to child with compile-time cycle detection.
- **Rich Ruleset Combinators**: Combine rules using `AND`, `OR`, `NONE` (logical NOR), or `min_passed` ($N$-of-$M$ threshold) selectors.
- **Model Context Protocol (MCP) Server**: Expose rule evaluation, expression validation, and runtime rule authoring to AI agents over JSON-RPC 2.0 stdio (`cmd/ruleengine-mcp`).

## Defining Rules & Rulesets in YAML

```yaml
rules:
  age_validation:
    name: "Age Validation"
    description: "Validates user age requirements"
    expression: "user.age >= globals.min_age"

  email_format:
    name: "Email Format Check"
    description: "Validates email format using regex"
    expression: |
      user.email.matches("^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}$")

  email_whitelist:
    name: "Domain Whitelist Check"
    extends: email_format
    expression: |
      globals.allowed_domains.exists(domain, user.email.endsWith('@' + domain))

rulesets:
  user_registration:
    name: "User Registration Validation"
    selector: "AND" # Supports AND (default), OR, NONE, or min_passed threshold
    rules:
      - age_validation
      - email_whitelist
```

## Programmatic Usage

```go
package main

import (
	"context"
	"fmt"

	"github.com/mobanhawi/ruleengine"
)

func main() {
	env, err := ruleengine.NewDefaultEnv("user", "request")
	if err != nil {
		panic(err)
	}

	engine, err := ruleengine.NewRuleEngineFromConfig(&ruleengine.RulesetConfig{
		Globals: map[string]any{"min_age": 18},
	}, "production", env, ruleengine.WithOptimise())
	if err != nil {
		panic(err)
	}

	// Register rules and rulesets at runtime
	_ = engine.UpsertRule("is_adult", ruleengine.Rule{
		Name:       "Adult Check",
		Expression: "user.age >= globals.min_age",
	})
	_ = engine.UpsertRuleset("onboarding", ruleengine.Ruleset{
		Name:     "Onboarding",
		Selector: ruleengine.SelectorAnd,
		Rules:    []string{"is_adult"},
	})

	result, err := engine.EvaluateRuleset(context.Background(), "onboarding", map[string]any{
		"user": map[string]any{"age": 21},
	})
	fmt.Printf("Passed: %v (err: %v)\n", result.Passed, err)
}
```

## MCP Server (`cmd/ruleengine-mcp`)

Build and run the standalone MCP server over stdio:

```bash
go build -o bin/ruleengine-mcp ./cmd/ruleengine-mcp
```

Configure in `~/.gemini/config/mcp_config.json` (or Claude Desktop / Cursor MCP config):

```json
{
  "mcpServers": {
    "ruleengine": {
      "command": "/path/to/bin/ruleengine-mcp",
      "args": ["-config", "/path/to/rules.yml", "-env", "development", "-vars", "user,request"]
    }
  }
}
```

### Exposed MCP Tools
| Tool | Description |
|------|-------------|
| `evaluate_rule` | Evaluate a single rule by `rule_name` against an `input` object |
| `evaluate_ruleset` | Evaluate a ruleset by `ruleset_name` against an `input` object |
| `evaluate_all_rulesets` | Evaluate all configured rulesets against an `input` object |
| `validate_expression` | Compile and verify that a CEL `expression` returns `bool` |
| `list_rules` | List all configured rules, expressions, and parent inheritance |
| `list_rulesets` | List all configured rulesets and combinator selectors |
| `upsert_rule` | Dynamically compile and register/update a rule at runtime |
| `upsert_ruleset` | Dynamically register/update a ruleset at runtime |
