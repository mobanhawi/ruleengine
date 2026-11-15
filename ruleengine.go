package ruleengine

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/overloads"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

const defaultInterruptCheckFrequency = 100

var (
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

// RuleEngine holds the configuration and compiled programs for concurrent rule evaluation.
type RuleEngine struct {
	mu       sync.RWMutex
	config   *RulesetConfig
	env      *cel.Env
	programs map[string]cel.Program
	parents  map[string][]string
	policy   Policy
	optimise bool
}

// Policy controls execution behavior such as fail-fast and timeout duration.
type Policy struct {
	StopOnFailure    bool
	MaxExecutionTime time.Duration
}

// Option defines a function that configures a RuleEngine.
type Option func(*RuleEngine)

// WithOptimise enables CEL AST optimization during rule compilation.
func WithOptimise() Option {
	return func(re *RuleEngine) {
		re.optimise = true
	}
}

// NewDefaultEnv creates a CEL environment pre-configured with "globals" (DynType),
// the provided variable names (DynType), and standard "timestamp" and "now" functions.
func NewDefaultEnv(variableNames ...string) (*cel.Env, error) {
	seen := map[string]bool{"globals": true}
	envOpts := make([]cel.EnvOption, 0, len(variableNames)+3)
	envOpts = append(envOpts, cel.Variable("globals", cel.DynType))

	for _, name := range variableNames {
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		envOpts = append(envOpts, cel.Variable(name, cel.DynType))
	}

	envOpts = append(envOpts,
		cel.Function("timestamp",
			cel.Overload(overloads.StringToTimestamp, []*cel.Type{cel.StringType}, cel.TimestampType,
				cel.UnaryBinding(func(val ref.Val) ref.Val {
					str, ok := val.Value().(string)
					if !ok {
						return types.NewErr("timestamp() requires string input")
					}
					t, err := time.Parse(time.RFC3339, str)
					if err != nil {
						return types.NewErr("invalid timestamp format: %v", err)
					}
					return types.Timestamp{Time: t}
				}),
			),
		),
		cel.Function("now",
			cel.Overload("now", []*cel.Type{}, cel.TimestampType,
				cel.FunctionBinding(func(args ...ref.Val) ref.Val {
					return types.Timestamp{Time: time.Now()}
				}),
			),
		),
	)

	env, err := cel.NewEnv(envOpts...)
	if err != nil {
		return nil, fmt.Errorf("creating default CEL env: %w", err)
	}
	return env, nil
}

// NewRuleEngine creates a new RuleEngine instance from a YAML configuration file path.
func NewRuleEngine(configPath string, environment string, env *cel.Env, opts ...Option) (*RuleEngine, error) {
	config, err := NewRulesetConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}
	return NewRuleEngineFromConfig(config, environment, env, opts...)
}

// NewRuleEngineFromConfig creates a new RuleEngine instance from an in-memory RulesetConfig.
// The provided configuration is deep-copied so subsequent caller mutations cannot affect the engine.
func NewRuleEngineFromConfig(config *RulesetConfig, environment string, env *cel.Env, opts ...Option) (*RuleEngine, error) {
	if config == nil {
		return nil, errors.New("ruleset config is nil")
	}
	if env == nil {
		return nil, errors.New("cel env is nil")
	}

	cfg := config.Clone()
	if cfg.Globals == nil {
		cfg.Globals = make(map[string]any)
	}
	if cfg.Rules == nil {
		cfg.Rules = make(map[string]Rule)
	}
	if cfg.Rulesets == nil {
		cfg.Rulesets = make(map[string]Ruleset)
	}
	if cfg.ErrorHandling.CustomErrorMessages == nil {
		cfg.ErrorHandling.CustomErrorMessages = make(map[string]string)
	}

	cfg.ApplyEnvironment(environment)

	policy := Policy{
		StopOnFailure:    true,
		MaxExecutionTime: defaultMaxExecutionTime,
	}
	if cfg.ErrorHandling.ExecutionPolicy != "" || len(cfg.ExecutionPolicies) > 0 {
		var err error
		policy, err = cfg.ToExecutionPolicy()
		if err != nil {
			return nil, fmt.Errorf("failed to get execution policy: %w", err)
		}
	}

	engine := &RuleEngine{
		config:   cfg,
		env:      env,
		policy:   policy,
		programs: make(map[string]cel.Program, len(cfg.Rules)),
		parents:  make(map[string][]string, len(cfg.Rules)),
		optimise: false,
	}

	for _, opt := range opts {
		opt(engine)
	}

	if err := engine.compileRules(); err != nil {
		return nil, fmt.Errorf("failed to compile rules: %w", err)
	}

	return engine, nil
}

// EvaluateRule evaluates a single rule (including its ancestor chain from root to child) by name.
// It enforces both the caller's context deadline and Policy.MaxExecutionTime.
func (re *RuleEngine) EvaluateRule(ctx context.Context, ruleName string, input map[string]any) (RuleResult, error) {
	if err := ctx.Err(); err != nil {
		return RuleResult{}, fmt.Errorf("context cancelled before evaluating rule %q: %w", ruleName, err)
	}

	re.mu.RLock()
	defer re.mu.RUnlock()

	evalCtx, cancel := re.withPolicyTimeoutLocked(ctx)
	defer cancel()

	activation := re.buildActivationLocked(input)
	return re.evaluateRuleLocked(evalCtx, time.Now(), ruleName, activation)
}

func (re *RuleEngine) evaluateRuleLocked(ctx context.Context, deadlineStart time.Time, ruleName string, activation map[string]any) (RuleResult, error) {
	start := time.Now()

	if _, exists := re.config.Rules[ruleName]; !exists {
		return RuleResult{}, fmt.Errorf("rule %q: %w", ruleName, ErrRuleNotFound)
	}

	parents := re.parents[ruleName]
	allRules := make([]string, 0, len(parents)+1)
	allRules = append(allRules, parents...)
	allRules = append(allRules, ruleName)

	passed := false
	for _, r := range allRules {
		if err := re.checkTimeoutLocked(ctx, deadlineStart, ruleName); err != nil {
			return RuleResult{}, err
		}
		program, pExists := re.programs[r]
		if !pExists {
			return RuleResult{}, fmt.Errorf("program for rule %q: %w", r, ErrRuleNotFound)
		}
		out, _, err := program.ContextEval(ctx, activation)
		if err != nil {
			if ctxErr := re.checkTimeoutLocked(ctx, deadlineStart, ruleName); ctxErr != nil {
				return RuleResult{}, ctxErr
			}
			return RuleResult{
				RuleName: ruleName,
				Passed:   false,
				Error:    err,
				Duration: time.Since(start),
			}, nil
		}
		if ctxErr := re.checkTimeoutLocked(ctx, deadlineStart, ruleName); ctxErr != nil {
			return RuleResult{}, ctxErr
		}
		boolVal, ok := out.Value().(bool)
		passed = ok && boolVal
		if !passed {
			break
		}
	}

	var errorMessage error
	if !passed {
		errorMessage = fmt.Errorf("rule '%s' did not pass evaluation", ruleName)
		if msg, ok := re.config.ErrorHandling.CustomErrorMessages[ruleName]; ok {
			errorMessage = errors.New(msg)
		}
	}
	return RuleResult{
		RuleName: ruleName,
		Passed:   passed,
		Error:    errorMessage,
		Duration: time.Since(start),
	}, nil
}

// EvaluateRuleset evaluates a ruleset by name, handling rule inheritance and selector logic.
// It enforces both the caller's context deadline and Policy.MaxExecutionTime.
func (re *RuleEngine) EvaluateRuleset(ctx context.Context, rulesetName string, input map[string]any) (RulesetResult, error) {
	if err := ctx.Err(); err != nil {
		return RulesetResult{}, fmt.Errorf("context cancelled before evaluating ruleset %q: %w", rulesetName, err)
	}

	re.mu.RLock()
	defer re.mu.RUnlock()

	evalCtx, cancel := re.withPolicyTimeoutLocked(ctx)
	defer cancel()

	activation := re.buildActivationLocked(input)
	return re.evaluateRulesetLocked(evalCtx, time.Now(), rulesetName, activation)
}

func (re *RuleEngine) evaluateRulesetLocked(ctx context.Context, deadlineStart time.Time, rulesetName string, activation map[string]any) (RulesetResult, error) {
	start := time.Now()

	ruleset, rOk := re.config.Rulesets[rulesetName]
	if !rOk {
		return RulesetResult{}, fmt.Errorf("ruleset %q: %w", rulesetName, ErrRulesetNotFound)
	}

	result := RulesetResult{
		RulesetName: rulesetName,
		RuleResults: make(map[string]RuleResult, len(ruleset.Rules)),
	}

	passedCount := 0
	for _, ruleRef := range ruleset.Rules {
		if err := re.checkTimeoutLocked(ctx, deadlineStart, rulesetName); err != nil {
			return RulesetResult{}, err
		}
		ruleResult, err := re.evaluateRuleLocked(ctx, deadlineStart, ruleRef, activation)
		if err != nil {
			return RulesetResult{}, err
		}
		result.RuleResults[ruleRef] = ruleResult
		if ruleResult.Passed {
			passedCount++
		}

		if re.shouldShortCircuitLocked(ruleset, ruleResult.Passed, passedCount) {
			break
		}
	}

	switch {
	case ruleset.MinPassed > 0:
		result.Passed = passedCount >= ruleset.MinPassed
	case ruleset.Selector == SelectorOr:
		result.Passed = passedCount > 0
	case ruleset.Selector == SelectorNone:
		result.Passed = passedCount == 0
	default:
		// Default to AND logic
		result.Passed = passedCount == len(ruleset.Rules)
	}

	var errorMessage error
	if !result.Passed {
		errorMessage = fmt.Errorf("ruleset '%s' did not pass evaluation", rulesetName)
		if msg, ok := re.config.ErrorHandling.CustomErrorMessages[rulesetName]; ok {
			errorMessage = errors.New(msg)
		}
	}

	result.Duration = time.Since(start)
	result.Error = errorMessage
	return result, nil
}

func (re *RuleEngine) shouldShortCircuitLocked(ruleset Ruleset, lastPassed bool, passedCount int) bool {
	if !re.policy.StopOnFailure {
		return false
	}
	if ruleset.MinPassed > 0 {
		return passedCount >= ruleset.MinPassed
	}
	switch ruleset.Selector {
	case SelectorOr:
		return lastPassed
	case SelectorNone:
		return lastPassed
	default:
		return !lastPassed
	}
}

// EvaluateAllRulesets evaluates all rulesets defined in the configuration within Policy.MaxExecutionTime.
func (re *RuleEngine) EvaluateAllRulesets(ctx context.Context, input map[string]any) (map[string]RulesetResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context cancelled before evaluating rulesets: %w", err)
	}

	re.mu.RLock()
	defer re.mu.RUnlock()

	evalCtx, cancel := re.withPolicyTimeoutLocked(ctx)
	defer cancel()

	deadlineStart := time.Now()
	activation := re.buildActivationLocked(input)
	results := make(map[string]RulesetResult, len(re.config.Rulesets))

	for rulesetName := range re.config.Rulesets {
		if err := re.checkTimeoutLocked(evalCtx, deadlineStart, rulesetName); err != nil {
			return results, err
		}

		result, err := re.evaluateRulesetLocked(evalCtx, deadlineStart, rulesetName, activation)
		if err != nil {
			return results, err
		}
		results[rulesetName] = result
	}

	return results, nil
}

func (re *RuleEngine) withPolicyTimeoutLocked(ctx context.Context) (context.Context, context.CancelFunc) {
	if re.policy.MaxExecutionTime > 0 {
		return context.WithTimeout(ctx, re.policy.MaxExecutionTime)
	}
	return ctx, func() {}
}

func (re *RuleEngine) checkTimeoutLocked(ctx context.Context, start time.Time, target string) error {
	if re.policy.MaxExecutionTime > 0 && time.Since(start) >= re.policy.MaxExecutionTime {
		return fmt.Errorf("timed out evaluating %q after %v: %w", target, re.policy.MaxExecutionTime, context.DeadlineExceeded)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("evaluation cancelled for %q: %w", target, err)
	}
	return nil
}

// ValidateExpression compiles and type-checks a candidate CEL expression against the engine's CEL environment.
func (re *RuleEngine) ValidateExpression(expression string) error {
	re.mu.RLock()
	defer re.mu.RUnlock()

	_, err := re.compileExpressionLocked(expression)
	return err
}

// UpsertRule compiles and registers or updates a rule at runtime.
func (re *RuleEngine) UpsertRule(id string, rule Rule) error {
	if id == "" {
		return errors.New("rule id cannot be empty")
	}

	re.mu.Lock()
	defer re.mu.Unlock()

	program, err := re.compileExpressionLocked(rule.Expression)
	if err != nil {
		return fmt.Errorf("compiling rule %q: %w", id, err)
	}

	rule.Tags = slices.Clone(rule.Tags)
	prevRule, hadPrev := re.config.Rules[id]
	re.config.Rules[id] = rule

	// Verify inheritance chains for all rules remain valid and cycle-free.
	newParents := make(map[string][]string, len(re.config.Rules))
	for ruleID, r := range re.config.Rules {
		parents, pErr := re.getRuleParentsLocked(ruleID, r)
		if pErr != nil {
			if hadPrev {
				re.config.Rules[id] = prevRule
			} else {
				delete(re.config.Rules, id)
			}
			return fmt.Errorf("validating inheritance for rule %q: %w", ruleID, pErr)
		}
		newParents[ruleID] = parents
	}

	re.programs[id] = program
	re.parents = newParents
	return nil
}

// RemoveRule deletes a rule by ID if it is not extended by another rule or referenced by a ruleset.
func (re *RuleEngine) RemoveRule(id string) error {
	re.mu.Lock()
	defer re.mu.Unlock()

	if _, exists := re.config.Rules[id]; !exists {
		return fmt.Errorf("rule %q: %w", id, ErrRuleNotFound)
	}

	for otherID, r := range re.config.Rules {
		if r.Extends == id {
			return fmt.Errorf("rule %q is extended by rule %q: %w", id, otherID, ErrRuleInUse)
		}
	}
	for rsID, rs := range re.config.Rulesets {
		if slices.Contains(rs.Rules, id) {
			return fmt.Errorf("rule %q is referenced by ruleset %q: %w", id, rsID, ErrRuleInUse)
		}
	}

	delete(re.config.Rules, id)
	delete(re.programs, id)
	delete(re.parents, id)
	return nil
}

// UpsertRuleset validates and registers or updates a ruleset at runtime.
func (re *RuleEngine) UpsertRuleset(id string, ruleset Ruleset) error {
	if id == "" {
		return errors.New("ruleset id cannot be empty")
	}

	re.mu.Lock()
	defer re.mu.Unlock()

	if err := re.validateRulesetLocked(id, ruleset); err != nil {
		return err
	}

	ruleset.Rules = slices.Clone(ruleset.Rules)
	ruleset.Tags = slices.Clone(ruleset.Tags)
	re.config.Rulesets[id] = ruleset
	return nil
}

func (re *RuleEngine) validateRulesetLocked(id string, ruleset Ruleset) error {
	if !ruleset.Selector.IsValid() {
		return fmt.Errorf("ruleset %q has unsupported selector %q: %w", id, ruleset.Selector, ErrInvalidSelector)
	}
	if ruleset.MinPassed < 0 || (ruleset.MinPassed > 0 && ruleset.MinPassed > len(ruleset.Rules)) {
		return fmt.Errorf("ruleset %q min_passed (%d) out of range [0, %d]: %w", id, ruleset.MinPassed, len(ruleset.Rules), ErrInvalidSelector)
	}
	for _, ruleRef := range ruleset.Rules {
		if _, exists := re.config.Rules[ruleRef]; !exists {
			return fmt.Errorf("ruleset %q references unknown rule %q: %w", id, ruleRef, ErrRuleNotFound)
		}
	}
	return nil
}

// ListRules returns a snapshot copy of all configured rules.
func (re *RuleEngine) ListRules() map[string]Rule {
	re.mu.RLock()
	defer re.mu.RUnlock()

	out := make(map[string]Rule, len(re.config.Rules))
	for k, v := range re.config.Rules {
		v.Tags = slices.Clone(v.Tags)
		out[k] = v
	}
	return out
}

// ListRulesets returns a snapshot copy of all configured rulesets.
func (re *RuleEngine) ListRulesets() map[string]Ruleset {
	re.mu.RLock()
	defer re.mu.RUnlock()

	out := make(map[string]Ruleset, len(re.config.Rulesets))
	for k, v := range re.config.Rulesets {
		v.Rules = slices.Clone(v.Rules)
		v.Tags = slices.Clone(v.Tags)
		out[k] = v
	}
	return out
}

func (re *RuleEngine) buildActivationLocked(input map[string]any) map[string]any {
	activation := make(map[string]any, len(input)+1)
	maps.Copy(activation, input)
	activation["globals"] = re.config.Globals
	return activation
}

func (re *RuleEngine) compileRules() error {
	for name, rule := range re.config.Rules {
		program, err := re.compileExpressionLocked(rule.Expression)
		if err != nil {
			return fmt.Errorf("failed to compile program for rule '%s': %w", name, err)
		}
		re.programs[name] = program
		parents, err := re.getRuleParentsLocked(name, rule)
		if err != nil {
			return fmt.Errorf("failed to find parent rules for rule '%s': %w", name, err)
		}
		re.parents[name] = parents
	}

	for id, ruleset := range re.config.Rulesets {
		if err := re.validateRulesetLocked(id, ruleset); err != nil {
			return fmt.Errorf("invalid ruleset %q: %w", id, err)
		}
	}

	return nil
}

func (re *RuleEngine) compileExpressionLocked(expression string) (cel.Program, error) {
	ast, issues := re.env.Compile(expression)
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("failed to compile expression '%s': %w", expression, issues.Err())
	}

	outType := ast.OutputType()
	if outType != cel.BoolType && outType != cel.DynType {
		return nil, fmt.Errorf("expression %q returns %s: %w", expression, outType, ErrNonBooleanExpression)
	}

	evalOpts := cel.OptExhaustiveEval
	if re.optimise {
		evalOpts = cel.OptOptimize
	}
	program, err := re.env.Program(
		ast,
		cel.EvalOptions(evalOpts),
		cel.InterruptCheckFrequency(defaultInterruptCheckFrequency),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create program for expression '%s': %w", expression, err)
	}
	return program, nil
}

// getRuleParentsLocked retrieves the ancestor rules for a given rule in top-down order
// (root ancestor first, immediate parent last).
func (re *RuleEngine) getRuleParentsLocked(ruleID string, rule Rule) ([]string, error) {
	current := rule
	parents := make([]string, 0)
	visited := map[string]bool{ruleID: true}

	for current.Extends != "" {
		if visited[current.Extends] {
			return nil, fmt.Errorf("%w: rule '%s'", ErrCircularDependency, rule.Name)
		}
		visited[current.Extends] = true

		parent, exists := re.config.Rules[current.Extends]
		if !exists {
			return nil, fmt.Errorf("extended rule '%s' not found for rule '%s': %w", current.Extends, rule.Name, ErrRuleNotFound)
		}
		parents = append(parents, current.Extends)
		current = parent
	}

	slices.Reverse(parents)
	return slices.Clip(parents), nil
}
