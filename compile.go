package ruleengine

import (
	"fmt"
	"maps"
	"slices"

	"github.com/mobanhawi/ruleengine/config"
)

// buildSnapshot compiles every rule via re.compiler, resolves inheritance, and validates rulesets.
func (re *RuleEngine) buildSnapshot(
	rules map[string]Rule,
	rulesets map[string]Ruleset,
	globals map[string]any,
	errorMessages map[string]string,
) (*snapshot, error) {
	clonedRules := make(map[string]Rule, len(rules))
	programs := make(map[string]EvalFunc, len(rules))
	for name, rule := range rules {
		evalFn, err := re.compiler.Compile(rule.Expression)
		if err != nil {
			return nil, fmt.Errorf("failed to compile program for rule '%s': %w", name, err)
		}
		rule.Tags = slices.Clone(rule.Tags)
		clonedRules[name] = rule
		programs[name] = evalFn
	}

	parents, err := resolveAllParents(clonedRules)
	if err != nil {
		return nil, err
	}

	clonedRulesets := make(map[string]Ruleset, len(rulesets))
	for id, ruleset := range rulesets {
		if err := validateRuleset(id, ruleset, clonedRules); err != nil {
			return nil, fmt.Errorf("invalid ruleset %q: %w", id, err)
		}
		ruleset.Rules = slices.Clone(ruleset.Rules)
		ruleset.Tags = slices.Clone(ruleset.Tags)
		clonedRulesets[id] = ruleset
	}

	return &snapshot{
		rules:         clonedRules,
		programs:      programs,
		parents:       parents,
		rulesets:      clonedRulesets,
		globals:       config.CloneAnyMap(globals),
		errorMessages: maps.Clone(errorMessages),
	}, nil
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
