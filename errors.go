package ruleengine

import "errors"

var (
	// ErrPolicyNotFound is returned when a referenced execution policy does not exist in config.
	ErrPolicyNotFound = errors.New("execution policy not found")
	// ErrInvalidSelector is returned when a ruleset specifies an unsupported selector value.
	ErrInvalidSelector = errors.New("invalid ruleset selector")
	// ErrInvalidMinPassed is returned when min_passed is out of range or combined with a selector.
	ErrInvalidMinPassed = errors.New("invalid ruleset min_passed")
	// ErrRuleNotFound is returned when a requested rule ID does not exist.
	ErrRuleNotFound = errors.New("rule not found")
	// ErrRulesetNotFound is returned when a requested ruleset ID does not exist.
	ErrRulesetNotFound = errors.New("ruleset not found")
	// ErrCircularDependency is returned when rule inheritance forms a cycle.
	ErrCircularDependency = errors.New("circular dependency detected in rule inheritance")
	// ErrRuleInUse is returned when attempting to remove a rule still referenced by another rule or ruleset.
	ErrRuleInUse = errors.New("rule is still referenced by another rule or ruleset")
	// ErrNonBooleanExpression is returned when a rule expression does not evaluate to a boolean.
	ErrNonBooleanExpression = errors.New("rule expression must return a boolean")
)
