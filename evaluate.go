package ruleengine

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"time"
)

// EvaluateRule evaluates a single rule (including its ancestor chain from root to child) by name.
// It enforces both the caller's context deadline and Policy.MaxExecutionTime.
// The read path is lock-free: it loads the current immutable snapshot atomically.
func (re *RuleEngine) EvaluateRule(ctx context.Context, ruleName string, input map[string]any) (RuleResult, error) {
	if err := ctx.Err(); err != nil {
		return RuleResult{}, fmt.Errorf("context cancelled before evaluating rule %q: %w", ruleName, err)
	}

	snap := re.state.Load()
	evalCtx, cancel := re.withPolicyTimeout(ctx)
	defer cancel()

	activation := buildActivation(snap, input)
	return re.evaluateRule(evalCtx, snap, ruleName, activation)
}

func (re *RuleEngine) evaluateRule(ctx context.Context, snap *snapshot, ruleName string, activation map[string]any) (RuleResult, error) {
	start := time.Now()

	if _, exists := snap.rules[ruleName]; !exists {
		return RuleResult{}, fmt.Errorf("rule %q: %w", ruleName, ErrRuleNotFound)
	}

	parents := snap.parents[ruleName]
	allRules := make([]string, 0, len(parents)+1)
	allRules = append(allRules, parents...)
	allRules = append(allRules, ruleName)

	passed := false
	for _, r := range allRules {
		if err := checkContext(ctx, ruleName); err != nil {
			return RuleResult{}, err
		}
		evalFn, pExists := snap.programs[r]
		if !pExists {
			return RuleResult{}, fmt.Errorf("program for rule %q: %w", r, ErrRuleNotFound)
		}
		boolVal, err := evalFn(ctx, activation)
		if err != nil {
			if ctxErr := checkContext(ctx, ruleName); ctxErr != nil {
				return RuleResult{}, ctxErr
			}
			return RuleResult{
				RuleName:  ruleName,
				Passed:    false,
				Error:     ruleFailureMessage(snap, ruleName),
				EvalError: err,
				Duration:  time.Since(start),
			}, nil
		}
		if ctxErr := checkContext(ctx, ruleName); ctxErr != nil {
			return RuleResult{}, ctxErr
		}
		passed = boolVal
		if !passed {
			break
		}
	}

	var failureMsg error
	if !passed {
		failureMsg = ruleFailureMessage(snap, ruleName)
	}
	return RuleResult{
		RuleName: ruleName,
		Passed:   passed,
		Error:    failureMsg,
		Duration: time.Since(start),
	}, nil
}

func ruleFailureMessage(snap *snapshot, ruleName string) error {
	if msg, ok := snap.errorMessages[ruleName]; ok {
		return errors.New(msg)
	}
	return fmt.Errorf("rule '%s' did not pass evaluation", ruleName)
}

// EvaluateRuleset evaluates a ruleset by name, handling rule inheritance and selector logic.
// It enforces both the caller's context deadline and Policy.MaxExecutionTime.
func (re *RuleEngine) EvaluateRuleset(ctx context.Context, rulesetName string, input map[string]any) (RulesetResult, error) {
	if err := ctx.Err(); err != nil {
		return RulesetResult{}, fmt.Errorf("context cancelled before evaluating ruleset %q: %w", rulesetName, err)
	}

	snap := re.state.Load()
	evalCtx, cancel := re.withPolicyTimeout(ctx)
	defer cancel()

	activation := buildActivation(snap, input)
	return re.evaluateRuleset(evalCtx, snap, rulesetName, activation)
}

// ruleTally tracks how many rules in a ruleset passed and how many failed with an evaluation error.
type ruleTally struct {
	passed     int
	evalErrors int
}

func (re *RuleEngine) evaluateRuleset(ctx context.Context, snap *snapshot, rulesetName string, activation map[string]any) (RulesetResult, error) {
	start := time.Now()

	ruleset, rOk := snap.rulesets[rulesetName]
	if !rOk {
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
		ruleResult, err := re.evaluateRule(ctx, snap, ruleRef, activation)
		if err != nil {
			return RulesetResult{}, err
		}
		result.RuleResults[ruleRef] = ruleResult
		if ruleResult.Passed {
			tally.passed++
		}
		if ruleResult.EvalError != nil {
			tally.evalErrors++
		}

		if re.shouldShortCircuit(ruleset, ruleResult, tally) {
			break
		}
	}

	result.Passed = rulesetPassed(ruleset, tally)
	if !result.Passed {
		result.Error = rulesetFailureMessage(snap, rulesetName)
	}
	result.Duration = time.Since(start)
	return result, nil
}

// rulesetPassed decides whether a ruleset passed given its rule tally.
// SelectorNone fails closed if any evaluated rule hit an EvalError.
func rulesetPassed(ruleset Ruleset, t ruleTally) bool {
	if ruleset.MinPassed > 0 {
		return t.passed >= ruleset.MinPassed
	}
	switch ruleset.Selector {
	case SelectorOr:
		return t.passed > 0
	case SelectorNone:
		return t.passed == 0 && t.evalErrors == 0
	default:
		return t.passed == len(ruleset.Rules)
	}
}

func rulesetFailureMessage(snap *snapshot, rulesetName string) error {
	if msg, ok := snap.errorMessages[rulesetName]; ok {
		return errors.New(msg)
	}
	return fmt.Errorf("ruleset '%s' did not pass evaluation", rulesetName)
}

func (re *RuleEngine) shouldShortCircuit(ruleset Ruleset, lastResult RuleResult, t ruleTally) bool {
	if !re.policy.StopOnFailure {
		return false
	}
	if ruleset.MinPassed > 0 {
		return t.passed >= ruleset.MinPassed
	}
	switch ruleset.Selector {
	case SelectorOr:
		return lastResult.Passed
	case SelectorNone:
		return lastResult.Passed || lastResult.EvalError != nil
	default:
		return !lastResult.Passed
	}
}

// EvaluateAllRulesets evaluates all rulesets defined in the engine within Policy.MaxExecutionTime.
func (re *RuleEngine) EvaluateAllRulesets(ctx context.Context, input map[string]any) (map[string]RulesetResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context cancelled before evaluating rulesets: %w", err)
	}

	snap := re.state.Load()
	evalCtx, cancel := re.withPolicyTimeout(ctx)
	defer cancel()

	activation := buildActivation(snap, input)
	results := make(map[string]RulesetResult, len(snap.rulesets))

	for rulesetName := range snap.rulesets {
		if err := checkContext(evalCtx, rulesetName); err != nil {
			return results, err
		}
		res, err := re.evaluateRuleset(evalCtx, snap, rulesetName, activation)
		if err != nil {
			return results, err
		}
		results[rulesetName] = res
	}
	return results, nil
}

// buildActivation clones the caller's input map and injects globals without mutating caller state.
func buildActivation(snap *snapshot, input map[string]any) map[string]any {
	activation := make(map[string]any, len(input)+1)
	maps.Copy(activation, input)
	activation["globals"] = snap.globals
	return activation
}

func (re *RuleEngine) withPolicyTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if re.policy.MaxExecutionTime <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, re.policy.MaxExecutionTime)
}

// checkContext checks both ctx.Err() and the wall-clock deadline directly so sub-microsecond
// policy timeouts are detected deterministically before the timer goroutine fires.
func checkContext(ctx context.Context, target string) error {
	if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) {
		return fmt.Errorf("execution timed out while evaluating %q: %w", target, context.DeadlineExceeded)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("execution cancelled while evaluating %q: %w", target, err)
	}
	return nil
}
