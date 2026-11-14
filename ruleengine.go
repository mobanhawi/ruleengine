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
func NewRuleEngineFromConfig(config *RulesetConfig, environment string, env *cel.Env, opts ...Option) (*RuleEngine, error) {
	if config == nil {
		return nil, errors.New("ruleset config is nil")
	}
	if env == nil {
		return nil, errors.New("cel env is nil")
	}

	if config.Globals == nil {
		config.Globals = make(map[string]any)
	}
	if config.Rules == nil {
		config.Rules = make(map[string]Rule)
	}
	if config.Rulesets == nil {
		config.Rulesets = make(map[string]Ruleset)
	}
	if config.ErrorHandling.CustomErrorMessages == nil {
		config.ErrorHandling.CustomErrorMessages = make(map[string]string)
	}

	config.ApplyEnvironment(environment)

	policy := Policy{
		StopOnFailure:    true,
		MaxExecutionTime: defaultMaxExecutionTime,
	}
	if config.ErrorHandling.ExecutionPolicy != "" || len(config.ExecutionPolicies) > 0 {
		var err error
		policy, err = config.ToExecutionPolicy()
		if err != nil {
			return nil, fmt.Errorf("failed to get execution policy: %w", err)
		}
	}

	engine := &RuleEngine{
		config:   config,
		env:      env,
		policy:   policy,
		programs: make(map[string]cel.Program, len(config.Rules)),
		parents:  make(map[string][]string, len(config.Rules)),
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
func (re *RuleEngine) EvaluateRule(ctx context.Context, ruleName string, input map[string]any) (RuleResult, error) {
	if err := ctx.Err(); err != nil {
		return RuleResult{}, fmt.Errorf("context cancelled before evaluating rule %q: %w", ruleName, err)
	}

	re.mu.RLock()
	defer re.mu.RUnlock()

	activation := re.buildActivationLocked(input)
	return re.evaluateRuleLocked(ctx, ruleName, activation)
}

func (re *RuleEngine) evaluateRuleLocked(ctx context.Context, ruleName string, activation map[string]any) (RuleResult, error) {
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
		if err := ctx.Err(); err != nil {
			return RuleResult{}, fmt.Errorf("context cancelled while evaluating rule %q: %w", ruleName, err)
		}
		program, pExists := re.programs[r]
		if !pExists {
			return RuleResult{}, fmt.Errorf("program for rule %q: %w", r, ErrRuleNotFound)
		}
		out, _, err := program.Eval(activation)
		if err != nil {
			return RuleResult{
				RuleName: ruleName,
				Passed:   false,
				Error:    err,
				Duration: time.Since(start),
			}, nil
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
func (re *RuleEngine) EvaluateRuleset(ctx context.Context, rulesetName string, input map[string]any) (RulesetResult, error) {
	if err := ctx.Err(); err != nil {
		return RulesetResult{}, fmt.Errorf("context cancelled before evaluating ruleset %q: %w", rulesetName, err)
	}

	re.mu.RLock()
	defer re.mu.RUnlock()

	activation := re.buildActivationLocked(input)
	return re.evaluateRulesetLocked(ctx, rulesetName, activation)
}

func (re *RuleEngine) evaluateRulesetLocked(ctx context.Context, rulesetName string, activation map[string]any) (RulesetResult, error) {
	start := time.Now()

	ruleset, rOk := re.config.Rulesets[rulesetName]
	if !rOk {
		return RulesetResult{}, fmt.Errorf("ruleset %q: %w", rulesetName, ErrRulesetNotFound)
	}

	result := RulesetResult{
		RulesetName: rulesetName,
		RuleResults: make(map[string]RuleResult, len(ruleset.Rules)),
	}

	for _, ruleRef := range ruleset.Rules {
		ruleResult, err := re.evaluateRuleLocked(ctx, ruleRef, activation)
		if err != nil {
			return RulesetResult{}, err
		}
		result.RuleResults[ruleRef] = ruleResult

		if re.policy.StopOnFailure {
			if ruleset.Selector == SelectorNone && ruleResult.Passed {
				break
			}
			if ruleset.Selector != SelectorOr && ruleset.Selector != SelectorNone && ruleset.MinPassed <= 0 && !ruleResult.Passed {
				break
			}
		}
	}

	passedCount := 0
	for _, ruleResult := range result.RuleResults {
		if ruleResult.Passed {
			passedCount++
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

// EvaluateAllRulesets evaluates all rulesets defined in the configuration within Policy.MaxExecutionTime.
func (re *RuleEngine) EvaluateAllRulesets(ctx context.Context, input map[string]any) (map[string]RulesetResult, error) {
	re.mu.RLock()
	defer re.mu.RUnlock()

	evalCtx, cancel := context.WithTimeout(ctx, re.policy.MaxExecutionTime)
	defer cancel()

	start := time.Now()
	activation := re.buildActivationLocked(input)
	results := make(map[string]RulesetResult, len(re.config.Rulesets))

	for rulesetName := range re.config.Rulesets {
		if time.Since(start) >= re.policy.MaxExecutionTime || evalCtx.Err() != nil {
			return results, fmt.Errorf("timed out waiting for ruleset %s", rulesetName)
		}

		result, err := re.evaluateRulesetLocked(evalCtx, rulesetName, activation)
		if err != nil {
			return results, err
		}
		results[rulesetName] = result
	}

	return results, nil
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

	for _, ruleRef := range ruleset.Rules {
		if _, exists := re.config.Rules[ruleRef]; !exists {
			return fmt.Errorf("ruleset %q references unknown rule %q: %w", id, ruleRef, ErrRuleNotFound)
		}
	}

	re.config.Rulesets[id] = ruleset
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
	program, err := re.env.Program(ast, cel.EvalOptions(evalOpts))
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
