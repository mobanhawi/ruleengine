package ruleengine

import (
	"errors"
	"fmt"
	"slices"
)

// ValidateExpression compiles and type-checks a candidate CEL expression against the engine's CEL environment.
func (re *RuleEngine) ValidateExpression(expression string) error {
	_, err := re.compileExpression(expression)
	return err
}

// UpsertRule compiles and registers or updates a rule at runtime.
func (re *RuleEngine) UpsertRule(id string, rule Rule) error {
	if id == "" {
		return errors.New("rule id cannot be empty")
	}

	// Compile outside the write lock: cel.Env is safe for concurrent use.
	program, err := re.compileExpression(rule.Expression)
	if err != nil {
		return fmt.Errorf("compiling rule %q: %w", id, err)
	}

	re.writeMu.Lock()
	defer re.writeMu.Unlock()

	next := re.state.Load().clone()
	rule.Tags = slices.Clone(rule.Tags)
	next.config.Rules[id] = rule

	parents, err := resolveAllParents(next.config.Rules)
	if err != nil {
		return fmt.Errorf("upserting rule %q: %w", id, err)
	}
	next.programs[id] = program
	next.parents = parents

	re.state.Store(next)
	return nil
}

// RemoveRule deletes a rule (and its custom error message) if it is not extended by
// another rule or referenced by a ruleset.
func (re *RuleEngine) RemoveRule(id string) error {
	re.writeMu.Lock()
	defer re.writeMu.Unlock()

	current := re.state.Load()
	if _, exists := current.config.Rules[id]; !exists {
		return fmt.Errorf("rule %q: %w", id, ErrRuleNotFound)
	}
	for otherID, r := range current.config.Rules {
		if r.Extends == id {
			return fmt.Errorf("rule %q is extended by rule %q: %w", id, otherID, ErrRuleInUse)
		}
	}
	for rsID, rs := range current.config.Rulesets {
		if slices.Contains(rs.Rules, id) {
			return fmt.Errorf("rule %q is referenced by ruleset %q: %w", id, rsID, ErrRuleInUse)
		}
	}

	next := current.clone()
	delete(next.config.Rules, id)
	delete(next.config.ErrorHandling.CustomErrorMessages, id)
	delete(next.programs, id)
	delete(next.parents, id)

	re.state.Store(next)
	return nil
}

// UpsertRuleset validates and registers or updates a ruleset at runtime.
func (re *RuleEngine) UpsertRuleset(id string, ruleset Ruleset) error {
	if id == "" {
		return errors.New("ruleset id cannot be empty")
	}

	re.writeMu.Lock()
	defer re.writeMu.Unlock()

	current := re.state.Load()
	if err := validateRuleset(id, ruleset, current.config.Rules); err != nil {
		return err
	}

	next := current.clone()
	ruleset.Rules = slices.Clone(ruleset.Rules)
	ruleset.Tags = slices.Clone(ruleset.Tags)
	next.config.Rulesets[id] = ruleset

	re.state.Store(next)
	return nil
}

// ListRules returns a snapshot copy of all configured rules.
func (re *RuleEngine) ListRules() map[string]Rule {
	s := re.state.Load()
	out := make(map[string]Rule, len(s.config.Rules))
	for k, v := range s.config.Rules {
		v.Tags = slices.Clone(v.Tags)
		out[k] = v
	}
	return out
}

// ListRulesets returns a snapshot copy of all configured rulesets.
func (re *RuleEngine) ListRulesets() map[string]Ruleset {
	s := re.state.Load()
	out := make(map[string]Ruleset, len(s.config.Rulesets))
	for k, v := range s.config.Rulesets {
		v.Rules = slices.Clone(v.Rules)
		v.Tags = slices.Clone(v.Tags)
		out[k] = v
	}
	return out
}
