package ruleengine

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	celgo "github.com/google/cel-go/cel"

	"github.com/mobanhawi/ruleengine/cel"
	"github.com/mobanhawi/ruleengine/config"
)

const defaultExecutionTimeout = 5 * time.Second

// snapshot is an immutable view of the compiled engine state. It has no dependency on
// serialized configuration structures or specific expression engines.
type snapshot struct {
	rules         map[string]Rule
	programs      map[string]EvalFunc
	parents       map[string][]string
	rulesets      map[string]Ruleset
	globals       map[string]any
	errorMessages map[string]string
}

// clone returns a deep copy of the snapshot that a writer may mutate before publishing.
func (s *snapshot) clone() *snapshot {
	rules := make(map[string]Rule, len(s.rules))
	for k, r := range s.rules {
		r.Tags = slices.Clone(r.Tags)
		rules[k] = r
	}

	rulesets := make(map[string]Ruleset, len(s.rulesets))
	for k, rs := range s.rulesets {
		rs.Rules = slices.Clone(rs.Rules)
		rs.Tags = slices.Clone(rs.Tags)
		rulesets[k] = rs
	}

	return &snapshot{
		rules:         rules,
		programs:      maps.Clone(s.programs),
		parents:       maps.Clone(s.parents),
		rulesets:      rulesets,
		globals:       config.CloneAnyMap(s.globals),
		errorMessages: maps.Clone(s.errorMessages),
	}
}

// RuleEngine evaluates rules and rulesets using a pluggable Compiler.
// Evaluations are stateless and lock-free; runtime mutations use copy-on-write snapshots.
type RuleEngine struct {
	writeMu  sync.Mutex
	state    atomic.Pointer[snapshot]
	compiler Compiler
	policy   Policy

	optimise          bool
	exhaustive        bool
	initGlobals       map[string]any
	initRules         map[string]Rule
	initRulesets      map[string]Ruleset
	initErrorMessages map[string]string
}

// Option configures a RuleEngine.
type Option func(*RuleEngine)

// WithOptimise enables static optimization when constructing a CEL-backed RuleEngine.
func WithOptimise() Option {
	return func(re *RuleEngine) {
		re.optimise = true
	}
}

// WithExhaustiveEval disables short-circuiting of logical operators when constructing a CEL-backed RuleEngine.
func WithExhaustiveEval() Option {
	return func(re *RuleEngine) {
		re.exhaustive = true
	}
}

// WithPolicy sets the execution policy (timeout and stop-on-failure behavior) for the engine.
func WithPolicy(policy Policy) Option {
	return func(re *RuleEngine) {
		re.policy = policy
	}
}

// WithGlobals sets the initial global variables available to all rule evaluations.
func WithGlobals(globals map[string]any) Option {
	return func(re *RuleEngine) {
		re.initGlobals = config.CloneAnyMap(globals)
	}
}

// WithRules sets the initial rule definitions to compile when constructing the engine.
func WithRules(rules map[string]Rule) Option {
	return func(re *RuleEngine) {
		re.initRules = rules
	}
}

// WithRulesets sets the initial ruleset definitions to validate when constructing the engine.
func WithRulesets(rulesets map[string]Ruleset) Option {
	return func(re *RuleEngine) {
		re.initRulesets = rulesets
	}
}

// WithCustomErrorMessages sets custom failure messages keyed by rule or ruleset ID.
func WithCustomErrorMessages(messages map[string]string) Option {
	return func(re *RuleEngine) {
		re.initErrorMessages = maps.Clone(messages)
	}
}

// NewWithCompiler creates a new RuleEngine backed by any Compiler implementation,
// completely decoupled from YAML configuration files and CEL.
func NewWithCompiler(compiler Compiler, opts ...Option) (*RuleEngine, error) {
	if compiler == nil {
		return nil, errors.New("rule compiler cannot be nil")
	}

	engine := &RuleEngine{
		compiler: compiler,
		policy: Policy{
			StopOnFailure:    true,
			MaxExecutionTime: defaultExecutionTimeout,
		},
	}
	for _, opt := range opts {
		opt(engine)
	}

	initial, err := engine.buildSnapshot(
		engine.initRules,
		engine.initRulesets,
		engine.initGlobals,
		engine.initErrorMessages,
	)
	if err != nil {
		return nil, err
	}
	engine.state.Store(initial)
	engine.initRules = nil
	engine.initRulesets = nil
	engine.initGlobals = nil
	engine.initErrorMessages = nil

	return engine, nil
}

// NewRuleEngine creates a new RuleEngine from a YAML configuration file path and a CEL environment.
func NewRuleEngine(configPath string, environment string, env *celgo.Env, opts ...Option) (*RuleEngine, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load ruleset config: %w", err)
	}
	return NewRuleEngineFromConfig(cfg, environment, env, opts...)
}

// NewRuleEngineFromConfig creates a new RuleEngine from an in-memory RulesetConfig and a CEL environment.
// The provided config is deep-copied so subsequent external mutations do not affect the engine.
func NewRuleEngineFromConfig(cfg *RulesetConfig, environment string, env *celgo.Env, opts ...Option) (*RuleEngine, error) {
	if cfg == nil {
		return nil, errors.New("ruleset config cannot be nil")
	}
	if env == nil {
		return nil, errors.New("cel environment cannot be nil")
	}

	cloned := cfg.Clone()
	if environment != "" {
		cloned.ApplyEnvironment(environment)
	}

	policy, err := cloned.ToExecutionPolicy()
	if err != nil {
		return nil, fmt.Errorf("failed to get execution policy: %w", err)
	}

	// Probe options for CEL compiler flags before constructing the compiler.
	probe := &RuleEngine{}
	for _, opt := range opts {
		opt(probe)
	}
	var celOpts []cel.Option
	if probe.optimise {
		celOpts = append(celOpts, cel.WithOptimise())
	}
	if probe.exhaustive {
		celOpts = append(celOpts, cel.WithExhaustiveEval())
	}

	compiler, err := cel.NewCompiler(env, celOpts...)
	if err != nil {
		return nil, err
	}

	engineOpts := make([]Option, 0, len(opts)+5)
	engineOpts = append(engineOpts,
		WithPolicy(policy),
		WithGlobals(cloned.Globals),
		WithRules(cloned.Rules),
		WithRulesets(cloned.Rulesets),
		WithCustomErrorMessages(cloned.ErrorHandling.CustomErrorMessages),
	)
	engineOpts = append(engineOpts, opts...)

	return NewWithCompiler(compiler, engineOpts...)
}
