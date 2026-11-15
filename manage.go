package ruleengine

import (
	"errors"
	"fmt"
	"slices"
)

// ValidateExpression checks whether an expression compiles cleanly and returns a boolean.
func (re *RuleEngine) ValidateExpression(expression string) error {
	if expression == "" {
		return errors.New("expression cannot be empty")
	}
	_, err := re.compiler.Compile(expression)
	return err
}

// UpsertRule compiles and registers (or updates) a rule at runtime using copy-on-write.
// In-flight evaluations continue reading the previous snapshot without blocking.
func (re *RuleEngine) UpsertRule(ruleID string, rule Rule) error {
	if ruleID == "" {
		return errors.New("rule ID cannot be empty")
	}
	if rule.Expression == "" {
		return fmt.Errorf("rule %q expression cannot be empty", ruleID)
	}

	evalFn, err := re.compiler.Compile(rule.Expression)
	if err != nil {
		return fmt.Errorf("compiling rule %q: %w", ruleID, err)
	}

	re.writeMu.Lock()
	defer re.writeMu.Unlock()

	next := re.state.Load().clone()
	rule.Tags = slices.Clone(rule.Tags)
	next.rules[ruleID] = rule
	next.programs[ruleID] = evalFn

	parents, err := resolveAllParents(next.rules)
	if err != nil {
		return err
	}
	next.parents = parents

	re.state.Store(next)
	return nil
}

// RemoveRule deletes a rule by ID if it is not referenced by any child rule or ruleset.
// Any custom error message registered for the rule is also removed.
func (re *RuleEngine) RemoveRule(ruleID string) error {
	re.writeMu.Lock()
	defer re.writeMu.Unlock()

	cur := re.state.Load()
	if _, exists := cur.rules[ruleID]; !exists {
		return fmt.Errorf("rule %q: %w", ruleID, ErrRuleNotFound)
	}

	for otherID, otherRule := range cur.rules {
		if otherID != ruleID && otherRule.Extends == ruleID {
			return fmt.Errorf("cannot remove rule %q: extended by rule %q: %w", ruleID, otherID, ErrRuleInUse)
		}
	}
	for rsID, rs := range cur.rulesets {
		if slices.Contains(rs.Rules, ruleID) {
			return fmt.Errorf("cannot remove rule %q: referenced by ruleset %q: %w", ruleID, rsID, ErrRuleInUse)
		}
	}

	next := cur.clone()
	delete(next.rules, ruleID)
	delete(next.programs, ruleID)
	delete(next.parents, ruleID)
	delete(next.errorMessages, ruleID)

	re.state.Store(next)
	return nil
}

// UpsertRuleset registers or updates a ruleset after verifying its selector and rule references.
func (re *RuleEngine) UpsertRuleset(rulesetID string, ruleset Ruleset) error {
	if rulesetID == "" {
		return errors.New("ruleset ID cannot be empty")
	}

	re.writeMu.Lock()
	defer re.writeMu.Unlock()

	cur := re.state.Load()
	if err := validateRuleset(rulesetID, ruleset, cur.rules); err != nil {
		return err
	}

	next := cur.clone()
	ruleset.Rules = slices.Clone(ruleset.Rules)
	ruleset.Tags = slices.Clone(ruleset.Tags)
	next.rulesets[rulesetID] = ruleset

	re.state.Store(next)
	return nil
}

// ListRules returns a deep copy of all currently registered rules.
func (re *RuleEngine) ListRules() map[string]Rule {
	snap := re.state.Load()
	out := make(map[string]Rule, len(snap.rules))
	for k, r := range snap.rules {
		r.Tags = slices.Clone(r.Tags)
		out[k] = r
	}
	return out
}

// ListRulesets returns a deep copy of all currently registered rulesets.
func (re *RuleEngine) ListRulesets() map[string]Ruleset {
	snap := re.state.Load()
	out := make(map[string]Ruleset, len(snap.rulesets))
	for k, rs := range snap.rulesets {
		rs.Rules = slices.Clone(rs.Rules)
		rs.Tags = slices.Clone(rs.Tags)
		out[k] = rs
	}
	return out
}
