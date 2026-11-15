# CEL Rule Engine

A concurrency-safe, configurable rule engine in Go powered by [CEL (Common Expression Language)](https://opensource.google/projects/cel) with YAML configuration, lock-free snapshot evaluation, programmatic rule management, and a built-in Model Context Protocol (MCP) server using the official [MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk).

## Features

- **Lock-Free, Concurrency-Safe Evaluation**: Readers load an immutable compiled snapshot via `atomic.Pointer` while writers (`UpsertRule`, `UpsertRuleset`, `RemoveRule`) publish copy-on-write snapshots without blocking in-flight evaluations.
- **Consistent `context.Context` & Timeout Enforcement**: Enforces both caller deadlines and `Policy.MaxExecutionTime` across `EvaluateRule`, `EvaluateRuleset`, and `EvaluateAllRulesets`, interrupting long-running CEL comprehensions via `cel.InterruptCheckFrequency`.
- **Distinct Business Failures vs. Runtime Evaluation Errors**: `RuleResult.Error` carries the business failure message while `RuleResult.EvalError` records runtime CEL errors, ensuring `NONE` rulesets fail closed when a rule cannot be evaluated.
- **YAML & Programmatic Configuration**: Load rules from YAML files, `io.Reader`, `[]byte`, or construct/mutate rules and rulesets dynamically at runtime.
- **Rule Inheritance (`extends`)**: Compose hierarchical rules evaluated top-down from root ancestor to child with compile-time cycle detection.
- **Rich Ruleset Combinators**: Combine rules using `AND`, `OR`, `NONE` (logical NOR, fail-closed), or `min_passed` ($N$-of-$M$ threshold) selectors.
- **Official MCP Go SDK Server**: Expose rule evaluation, expression validation, and opt-in runtime rule authoring to AI agents (`cmd/ruleengine-mcp`).

## Package Structure

| Package / File | Responsibility |
|------|----------------|
| `compiler.go` | `Compiler` interface and `EvalFunc` abstraction decoupling the engine from CEL |
| `ruleengine.go` | `RuleEngine`, `Option`s (`WithPolicy`, `WithGlobals`, `WithRules`, `WithRulesets`, `WithCustomErrorMessages`, `WithOptimise`, `WithExhaustiveEval`), config-free `NewWithCompiler`, and convenience constructors |
| `evaluate.go` | Lock-free `EvaluateRule`, `EvaluateRuleset`, `EvaluateAllRulesets`, combinator tally/short-circuiting, and deadline checks |
| `compile.go` | Config-free `snapshot` builder, ruleset validation, and top-down parent inheritance resolution |
| `manage.go` | Copy-on-write runtime mutation & introspection (`ValidateExpression`, `UpsertRule`, `RemoveRule`, `UpsertRuleset`, `ListRules`, `ListRulesets`) |
| `rules.go` | `RuleResult`, `RulesetResult`, and JSON marshaling |
| `config.go` / `env.go` / `errors.go` | Root re-exports of configuration types/loaders, `NewDefaultEnv`, and sentinel errors |
| `cel/compiler.go` & `cel/env.go` | `cel.Compiler` implementation (`Compile(string) (EvalFunc, error)`) and `cel.NewDefaultEnv` |
| `config/config.go` | YAML/JSON configuration types (`RulesetConfig`, `Rule`, `Ruleset`, `Policy`), deep `Clone()`, environment overrides, and `Load*` helpers |
| `mcp/server.go`, `mcp/tools.go`, `mcp/handlers.go` | MCP server wrapper around `github.com/modelcontextprotocol/go-sdk/mcp` accepting a consumer-defined `mcp.Engine` interface |

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

By default, `ruleengine-mcp` starts in **read-only mode** (exposing only evaluation, validation, and listing tools). Pass `-allow-authoring` to enable `upsert_rule` and `upsert_ruleset` (in-memory only; changes are not persisted across restarts):

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
| Tool | Mode | Description |
|------|------|-------------|
| `evaluate_rule` | Read-only | Evaluate a single rule by `rule_name` against an `input` object |
| `evaluate_ruleset` | Read-only | Evaluate a ruleset by `ruleset_name` against an `input` object |
| `evaluate_all_rulesets` | Read-only | Evaluate all configured rulesets against an `input` object |
| `validate_expression` | Read-only | Compile and verify that a CEL `expression` returns `bool` |
| `list_rules` | Read-only | List all configured rules, expressions, and parent inheritance |
| `list_rulesets` | Read-only | List all configured rulesets and combinator selectors |
| `upsert_rule` | `-allow-authoring` | Dynamically compile and register/update a rule in memory |
| `upsert_ruleset` | `-allow-authoring` | Dynamically register/update a ruleset in memory |

## Breaking Changes from `v0.0.3`
- Removed stateful `SetContext`; `EvaluateRule`, `EvaluateRuleset`, and `EvaluateAllRulesets` now accept `(ctx context.Context, ..., input map[string]any)`.
- Unknown ruleset `selector` values and combining `selector` with `min_passed` now fail validation (`ErrInvalidSelector` / `ErrInvalidMinPassed`) instead of silently falling back to `AND`.
- Standard CEL short-circuiting (`&&` / `||`) is now enabled by default; use `WithExhaustiveEval()` when exhaustive state tracking is required.
