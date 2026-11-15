package ruleengine

import (
	"fmt"
	"slices"

	"github.com/google/cel-go/cel"
)

// defaultInterruptCheckFrequency is how many comprehension iterations CEL runs between
// context cancellation checks during ContextEval.
const defaultInterruptCheckFrequency = 100

// buildSnapshot compiles every rule, resolves inheritance and validates rulesets for cfg.
func (re *RuleEngine) buildSnapshot(cfg *RulesetConfig) (*snapshot, error) {
	programs := make(map[string]cel.Program, len(cfg.Rules))
	for name, rule := range cfg.Rules {
		program, err := re.compileExpression(rule.Expression)
		if err != nil {
			return nil, fmt.Errorf("failed to compile program for rule '%s': %w", name, err)
		}
		programs[name] = program
	}

	parents, err := resolveAllParents(cfg.Rules)
	if err != nil {
		return nil, err
	}

	for id, ruleset := range cfg.Rulesets {
		if err := validateRuleset(id, ruleset, cfg.Rules); err != nil {
			return nil, fmt.Errorf("invalid ruleset %q: %w", id, err)
		}
	}

	return &snapshot{config: cfg, programs: programs, parents: parents}, nil
}

func validateRuleset(id string, ruleset Ruleset, rules map[string]Rule) error {
	if !ruleset.Selector.IsValid() {
		return fmt.Errorf("ruleset %q has unsupported selector %q: %w", id, ruleset.Selector, ErrInvalidSelector)
	}
	if ruleset.MinPassed < 0 || ruleset.MinPassed > len(ruleset.Rules) {
		return fmt.Errorf("ruleset %q min_passed (%d) out of range [0, %d]: %w", id, ruleset.MinPassed, len(ruleset.Rules), ErrInvalidMinPassed)
	}
	if ruleset.MinPassed > 0 && ruleset.Selector != "" {
		return fmt.Errorf("ruleset %q sets both min_passed and selector %q; use one: %w", id, ruleset.Selector, ErrInvalidMinPassed)
	}
	for _, ruleRef := range ruleset.Rules {
		if _, exists := rules[ruleRef]; !exists {
			return fmt.Errorf("ruleset %q references unknown rule %q: %w", id, ruleRef, ErrRuleNotFound)
		}
	}
	return nil
}

func (re *RuleEngine) compileExpression(expression string) (cel.Program, error) {
	ast, issues := re.env.Compile(expression)
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("failed to compile expression '%s': %w", expression, issues.Err())
	}

	outType := ast.OutputType()
	if outType != cel.BoolType && outType != cel.DynType {
		return nil, fmt.Errorf("expression %q returns %s: %w", expression, outType, ErrNonBooleanExpression)
	}

	var evalOpts cel.EvalOption
	if re.exhaustive {
		evalOpts |= cel.OptExhaustiveEval
	}
	if re.optimise {
		evalOpts |= cel.OptOptimize
	}
	programOpts := []cel.ProgramOption{cel.InterruptCheckFrequency(defaultInterruptCheckFrequency)}
	if evalOpts != 0 {
		programOpts = append(programOpts, cel.EvalOptions(evalOpts))
	}

	program, err := re.env.Program(ast, programOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create program for expression '%s': %w", expression, err)
	}
	return program, nil
}

// resolveAllParents computes the ancestor chain for every rule, failing on cycles or missing parents.
func resolveAllParents(rules map[string]Rule) (map[string][]string, error) {
	parents := make(map[string][]string, len(rules))
	for id, rule := range rules {
		chain, err := resolveParents(rules, id, rule)
		if err != nil {
			return nil, fmt.Errorf("failed to find parent rules for rule '%s': %w", id, err)
		}
		parents[id] = chain
	}
	return parents, nil
}

// resolveParents returns a rule's ancestors in top-down order (root first, immediate parent last).
func resolveParents(rules map[string]Rule, ruleID string, rule Rule) ([]string, error) {
	current := rule
	chain := make([]string, 0)
	visited := map[string]bool{ruleID: true}

	for current.Extends != "" {
		if visited[current.Extends] {
			return nil, fmt.Errorf("%w: rule '%s'", ErrCircularDependency, ruleID)
		}
		visited[current.Extends] = true

		parent, exists := rules[current.Extends]
		if !exists {
			return nil, fmt.Errorf("extended rule '%s' not found for rule '%s': %w", current.Extends, ruleID, ErrRuleNotFound)
		}
		chain = append(chain, current.Extends)
		current = parent
	}

	slices.Reverse(chain)
	return slices.Clip(chain), nil
}
