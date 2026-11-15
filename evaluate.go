package ruleengine

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"
)

// EvaluateRule evaluates a single rule (including its ancestor chain from root to child) by name.
// It enforces both the caller's context deadline and Policy.MaxExecutionTime.
func (re *RuleEngine) EvaluateRule(ctx context.Context, ruleName string, input map[string]any) (RuleResult, error) {
	if err := ctx.Err(); err != nil {
		return RuleResult{}, fmt.Errorf("context cancelled before evaluating rule %q: %w", ruleName, err)
	}

	evalCtx, cancel := re.withPolicyTimeout(ctx)
	defer cancel()

	s := re.state.Load()
	return re.evaluateRule(evalCtx, s, ruleName, buildActivation(s, input))
}

func (re *RuleEngine) evaluateRule(ctx context.Context, s *snapshot, ruleName string, activation map[string]any) (RuleResult, error) {
	start := time.Now()

	if _, exists := s.config.Rules[ruleName]; !exists {
		return RuleResult{}, fmt.Errorf("rule %q: %w", ruleName, ErrRuleNotFound)
	}

	parents := s.parents[ruleName]
	chain := make([]string, 0, len(parents)+1)
	chain = append(chain, parents...)
	chain = append(chain, ruleName)

	for _, r := range chain {
		if err := checkContext(ctx, ruleName); err != nil {
			return RuleResult{}, err
		}
		program, ok := s.programs[r]
		if !ok {
			return RuleResult{}, fmt.Errorf("program for rule %q: %w", r, ErrRuleNotFound)
		}

		out, _, evalErr := program.ContextEval(ctx, activation)
		// A cancellation surfacing as a CEL error is a caller-level failure, not a rule result.
		if err := checkContext(ctx, ruleName); err != nil {
			return RuleResult{}, err
		}
		if evalErr != nil {
			return s.failedRule(ruleName, start, fmt.Errorf("evaluating rule %q: %w", r, evalErr)), nil
		}

		passed, isBool := out.Value().(bool)
		if !isBool {
			return s.failedRule(ruleName, start, fmt.Errorf("rule %q returned %T: %w", r, out.Value(), ErrNonBooleanExpression)), nil
		}
		if !passed {
			return s.failedRule(ruleName, start, nil), nil
		}
	}

	return RuleResult{
		RuleName: ruleName,
		Passed:   true,
		Duration: time.Since(start),
	}, nil
}

// failedRule builds a non-passing RuleResult with the business failure message and an optional evaluation error.
func (s *snapshot) failedRule(ruleName string, start time.Time, evalErr error) RuleResult {
	return RuleResult{
		RuleName:  ruleName,
		Passed:    false,
		Error:     s.failureMessage("rule", ruleName),
		EvalError: evalErr,
		Duration:  time.Since(start),
	}
}

func (s *snapshot) failureMessage(kind, name string) error {
	if msg, ok := s.config.ErrorHandling.CustomErrorMessages[name]; ok {
		return errors.New(msg)
	}
	return fmt.Errorf("%s '%s' did not pass evaluation", kind, name)
}

// EvaluateRuleset evaluates a ruleset by name, handling rule inheritance and selector logic.
// It enforces both the caller's context deadline and Policy.MaxExecutionTime.
func (re *RuleEngine) EvaluateRuleset(ctx context.Context, rulesetName string, input map[string]any) (RulesetResult, error) {
	if err := ctx.Err(); err != nil {
		return RulesetResult{}, fmt.Errorf("context cancelled before evaluating ruleset %q: %w", rulesetName, err)
	}

	evalCtx, cancel := re.withPolicyTimeout(ctx)
	defer cancel()

	s := re.state.Load()
	return re.evaluateRuleset(evalCtx, s, rulesetName, buildActivation(s, input))
}

func (re *RuleEngine) evaluateRuleset(ctx context.Context, s *snapshot, rulesetName string, activation map[string]any) (RulesetResult, error) {
	start := time.Now()

	ruleset, ok := s.config.Rulesets[rulesetName]
	if !ok {
		return RulesetResult{}, fmt.Errorf("ruleset %q: %w", rulesetName, ErrRulesetNotFound)
	}

	result := RulesetResult{
		RulesetName: rulesetName,
		RuleResults: make(map[string]RuleResult, len(ruleset.Rules)),
	}

	var tally ruleTally
	for _, ruleRef := range ruleset.Rules {
		if err := checkContext(ctx, rulesetName); err != nil {
			return RulesetResult{}, err
		}
		ruleResult, err := re.evaluateRule(ctx, s, ruleRef, activation)
		if err != nil {
			return RulesetResult{}, err
		}
		result.RuleResults[ruleRef] = ruleResult
		tally.add(ruleResult)

		if re.shouldShortCircuit(ruleset, ruleResult, tally) {
			break
		}
	}

	result.Passed = rulesetPassed(ruleset, tally)
	if !result.Passed {
		result.Error = s.failureMessage("ruleset", rulesetName)
	}
	result.Duration = time.Since(start)
	return result, nil
}

// ruleTally counts rule outcomes within a single ruleset evaluation.
type ruleTally struct {
	passed     int
	evalErrors int
}

func (t *ruleTally) add(r RuleResult) {
	if r.Passed {
		t.passed++
	}
	if r.EvalError != nil {
		t.evalErrors++
	}
}

// rulesetPassed applies selector semantics. Evaluation errors never count as passes, so
// AND / OR / min_passed are naturally fail-closed. NONE must explicitly fail on any
// evaluation error, otherwise "no rule matched" would be satisfied by a broken rule.
func rulesetPassed(rs Ruleset, t ruleTally) bool {
	switch {
	case rs.MinPassed > 0:
		return t.passed >= rs.MinPassed
	case rs.Selector == SelectorOr:
		return t.passed > 0
	case rs.Selector == SelectorNone:
		return t.passed == 0 && t.evalErrors == 0
	default: // AND (explicit or empty selector)
		return t.passed == len(rs.Rules)
	}
}

// shouldShortCircuit reports whether the remaining rules can no longer change the outcome.
func (re *RuleEngine) shouldShortCircuit(rs Ruleset, last RuleResult, t ruleTally) bool {
	if !re.policy.StopOnFailure {
		return false
	}
	switch {
	case rs.MinPassed > 0:
		return t.passed >= rs.MinPassed
	case rs.Selector == SelectorOr:
		return last.Passed
	case rs.Selector == SelectorNone:
		return last.Passed || last.EvalError != nil
	default:
		return !last.Passed
	}
}

// EvaluateAllRulesets evaluates all rulesets (in name order) within Policy.MaxExecutionTime.
// On timeout or cancellation it returns the results gathered so far alongside the error.
func (re *RuleEngine) EvaluateAllRulesets(ctx context.Context, input map[string]any) (map[string]RulesetResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context cancelled before evaluating rulesets: %w", err)
	}

	evalCtx, cancel := re.withPolicyTimeout(ctx)
	defer cancel()

	s := re.state.Load()
	activation := buildActivation(s, input)
	names := slices.Sorted(maps.Keys(s.config.Rulesets))
	results := make(map[string]RulesetResult, len(names))

	for _, rulesetName := range names {
		if err := checkContext(evalCtx, rulesetName); err != nil {
			return results, err
		}
		result, err := re.evaluateRuleset(evalCtx, s, rulesetName, activation)
		if err != nil {
			return results, err
		}
		results[rulesetName] = result
	}

	return results, nil
}

func (re *RuleEngine) withPolicyTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if re.policy.MaxExecutionTime > 0 {
		return context.WithTimeout(ctx, re.policy.MaxExecutionTime)
	}
	return context.WithCancel(ctx)
}

// checkContext reports cancellation or an elapsed deadline. The explicit deadline comparison
// makes expiry deterministic even before the context's internal timer goroutine has fired.
func checkContext(ctx context.Context, target string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("evaluation of %q stopped: %w", target, err)
	}
	if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) {
		return fmt.Errorf("evaluation of %q stopped: %w", target, context.DeadlineExceeded)
	}
	return nil
}

// buildActivation copies the caller's input and injects globals, so the caller's map is never mutated.
func buildActivation(s *snapshot, input map[string]any) map[string]any {
	activation := make(map[string]any, len(input)+1)
	maps.Copy(activation, input)
	activation["globals"] = s.config.Globals
	return activation
}
