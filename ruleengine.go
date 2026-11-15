package ruleengine

import (
	"errors"
	"fmt"
	"maps"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/cel-go/cel"
)

// snapshot is an immutable view of the compiled engine state. It is never mutated after
// being published via RuleEngine.state; writers build a modified copy and swap it in.
type snapshot struct {
	config   *RulesetConfig
	programs map[string]cel.Program
	parents  map[string][]string
}

// clone returns a copy of the snapshot that a writer may mutate before publishing.
// cel.Program values are immutable and safe to share between snapshots.
func (s *snapshot) clone() *snapshot {
	return &snapshot{
		config:   s.config.Clone(),
		programs: maps.Clone(s.programs),
		parents:  maps.Clone(s.parents),
	}
}

// RuleEngine evaluates CEL rules and rulesets. It is safe for concurrent use.
//
// Evaluations are lock-free: they load an immutable snapshot, so a slow evaluation never
// blocks writers and a pending writer never blocks new evaluations. Writers (Upsert*/Remove*)
// are serialized and publish a new snapshot atomically; in-flight evaluations keep using the
// snapshot they started with.
type RuleEngine struct {
	writeMu    sync.Mutex
	state      atomic.Pointer[snapshot]
	env        *cel.Env
	policy     Policy
	optimise   bool
	exhaustive bool
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

// WithExhaustiveEval disables CEL's short-circuiting of && and || so every sub-expression is
// evaluated. This is slower and is mainly useful for debugging or state tracking.
func WithExhaustiveEval() Option {
	return func(re *RuleEngine) {
		re.exhaustive = true
	}
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
		env:    env,
		policy: policy,
	}
	for _, opt := range opts {
		opt(engine)
	}

	snap, err := engine.buildSnapshot(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to compile rules: %w", err)
	}
	engine.state.Store(snap)

	return engine, nil
}
