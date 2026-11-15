package config

import (
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	// SelectorAnd requires all rules in a ruleset to pass.
	SelectorAnd SelectorType = "AND"
	// SelectorOr requires at least one rule in a ruleset to pass.
	SelectorOr SelectorType = "OR"
	// SelectorNone requires zero rules in a ruleset to pass (logical NOR).
	SelectorNone SelectorType = "NONE"

	defaultPolicyMaxExecutionTime = 5 * time.Second
)

var (
	// ErrPolicyNotFound is returned when a referenced execution policy does not exist in configuration.
	ErrPolicyNotFound = errors.New("execution policy not found in config")
	// ErrInvalidSelector is returned when a ruleset specifies an unsupported selector value.
	ErrInvalidSelector = errors.New("invalid ruleset selector")
	// ErrInvalidMinPassed is returned when min_passed is out of range or combined with a selector.
	ErrInvalidMinPassed = errors.New("invalid ruleset min_passed")
)

// SelectorType defines the logical combinator for a ruleset.
type SelectorType string

// IsValid reports whether s is a supported selector value (including empty, which defaults to AND).
func (s SelectorType) IsValid() bool {
	switch s {
	case "", SelectorAnd, SelectorOr, SelectorNone:
		return true
	default:
		return false
	}
}

// Policy defines the runtime execution policy for rule and ruleset evaluation.
type Policy struct {
	StopOnFailure    bool
	MaxExecutionTime time.Duration
}

// RulesetConfig is the top-level configuration structure.
type RulesetConfig struct {
	APIVersion        string                     `yaml:"apiVersion" json:"apiVersion,omitempty"`
	Kind              string                     `yaml:"kind" json:"kind,omitempty"`
	Metadata          Metadata                   `yaml:"metadata" json:"metadata,omitempty"`
	Globals           map[string]any             `yaml:"globals" json:"globals,omitempty"`
	Rules             map[string]Rule            `yaml:"rules" json:"rules"`
	Rulesets          map[string]Ruleset         `yaml:"rulesets" json:"rulesets"`
	ExecutionPolicies map[string]ExecutionPolicy `yaml:"execution_policies" json:"execution_policies,omitempty"`
	ErrorHandling     ErrorHandling              `yaml:"error_handling" json:"error_handling,omitempty"`
	Environments      map[string]Environment     `yaml:"environments" json:"environments,omitempty"`
}

// Rule represents an individual boolean rule definition.
type Rule struct {
	Name        string   `yaml:"name" json:"name"`
	Description string   `yaml:"description" json:"description,omitempty"`
	Expression  string   `yaml:"expression" json:"expression"`
	Extends     string   `yaml:"extends" json:"extends,omitempty"`
	Tags        []string `yaml:"tags,omitempty" json:"tags,omitempty"`
}

// Ruleset represents a collection of rules and their combinator logic.
type Ruleset struct {
	Name        string       `yaml:"name" json:"name"`
	Description string       `yaml:"description" json:"description,omitempty"`
	Selector    SelectorType `yaml:"selector" json:"selector,omitempty"`
	MinPassed   int          `yaml:"min_passed,omitempty" json:"min_passed,omitempty"`
	Rules       []string     `yaml:"rules" json:"rules"`
	Tags        []string     `yaml:"tags,omitempty" json:"tags,omitempty"`
}

// Metadata contains basic information about the ruleset configuration.
type Metadata struct {
	Name        string `yaml:"name" json:"name,omitempty"`
	Description string `yaml:"description" json:"description,omitempty"`
}

// ExecutionPolicy defines how rulesets should be executed in serialized configuration.
type ExecutionPolicy struct {
	Name             string `yaml:"name" json:"name,omitempty"`
	Description      string `yaml:"description" json:"description,omitempty"`
	StopOnFailure    bool   `yaml:"stop_on_failure" json:"stop_on_failure"`
	MaxExecutionTime string `yaml:"max_execution_time" json:"max_execution_time,omitempty"`
}

// ErrorHandling defines how errors should be handled and reported.
type ErrorHandling struct {
	ExecutionPolicy     string            `yaml:"execution_policy" json:"execution_policy,omitempty"`
	CustomErrorMessages map[string]string `yaml:"custom_error_messages" json:"custom_error_messages,omitempty"`
}

// Environment defines environment-specific overrides.
type Environment struct {
	Globals       map[string]any `yaml:"globals" json:"globals,omitempty"`
	ErrorHandling ErrorHandling  `yaml:"error_handling" json:"error_handling,omitempty"`
}

// Clone returns a deep copy of RulesetConfig so external callers cannot mutate engine state.
func (rc *RulesetConfig) Clone() *RulesetConfig {
	if rc == nil {
		return &RulesetConfig{}
	}
	cp := *rc
	cp.Globals = CloneAnyMap(rc.Globals)

	if rc.Rules != nil {
		cp.Rules = make(map[string]Rule, len(rc.Rules))
		for k, r := range rc.Rules {
			r.Tags = slices.Clone(r.Tags)
			cp.Rules[k] = r
		}
	}

	if rc.Rulesets != nil {
		cp.Rulesets = make(map[string]Ruleset, len(rc.Rulesets))
		for k, rs := range rc.Rulesets {
			rs.Rules = slices.Clone(rs.Rules)
			rs.Tags = slices.Clone(rs.Tags)
			cp.Rulesets[k] = rs
		}
	}

	cp.ExecutionPolicies = maps.Clone(rc.ExecutionPolicies)
	cp.ErrorHandling.CustomErrorMessages = maps.Clone(rc.ErrorHandling.CustomErrorMessages)

	if rc.Environments != nil {
		cp.Environments = make(map[string]Environment, len(rc.Environments))
		for k, env := range rc.Environments {
			cp.Environments[k] = Environment{
				Globals: CloneAnyMap(env.Globals),
				ErrorHandling: ErrorHandling{
					ExecutionPolicy:     env.ErrorHandling.ExecutionPolicy,
					CustomErrorMessages: maps.Clone(env.ErrorHandling.CustomErrorMessages),
				},
			}
		}
	}

	return &cp
}

// CloneAnyMap deep-copies a string-keyed map containing arbitrary YAML/JSON values.
func CloneAnyMap(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = cloneAny(v)
	}
	return dst
}

// cloneAny recursively deep-copies the container types produced by YAML/JSON decoding.
func cloneAny(v any) any {
	switch typed := v.(type) {
	case map[string]any:
		return CloneAnyMap(typed)
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = cloneAny(item)
		}
		return out
	case []string:
		return slices.Clone(typed)
	default:
		return v
	}
}

// Load reads and parses the YAML configuration file at configPath.
func Load(configPath string) (*RulesetConfig, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("reading config file %q: %w", configPath, err)
	}
	return LoadFromBytes(data)
}

// LoadFromReader reads and parses a YAML/JSON configuration from an io.Reader.
func LoadFromReader(r io.Reader) (*RulesetConfig, error) {
	if r == nil {
		return nil, errors.New("config reader cannot be nil")
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("reading config stream: %w", err)
	}
	return LoadFromBytes(data)
}

// LoadFromBytes parses a YAML/JSON configuration from raw bytes.
func LoadFromBytes(data []byte) (*RulesetConfig, error) {
	var cfg RulesetConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshaling config: %w", err)
	}
	return &cfg, nil
}

// ApplyEnvironment applies environment-specific overrides to the configuration safely.
func (rc *RulesetConfig) ApplyEnvironment(environment string) {
	envConfig, exists := rc.Environments[environment]
	if !exists {
		return
	}

	if envConfig.Globals != nil {
		if rc.Globals == nil {
			rc.Globals = make(map[string]any, len(envConfig.Globals))
		}
		for k, v := range envConfig.Globals {
			rc.Globals[k] = v
		}
	}

	if envConfig.ErrorHandling.ExecutionPolicy != "" {
		rc.ErrorHandling.ExecutionPolicy = envConfig.ErrorHandling.ExecutionPolicy
	}

	if envConfig.ErrorHandling.CustomErrorMessages != nil {
		if rc.ErrorHandling.CustomErrorMessages == nil {
			rc.ErrorHandling.CustomErrorMessages = make(map[string]string, len(envConfig.ErrorHandling.CustomErrorMessages))
		}
		for k, v := range envConfig.ErrorHandling.CustomErrorMessages {
			rc.ErrorHandling.CustomErrorMessages[k] = v
		}
	}
}

// ToExecutionPolicy maps the active execution policy from the current configuration.
// If no execution policies are configured and no policy name is specified, sensible defaults are returned.
func (rc *RulesetConfig) ToExecutionPolicy() (Policy, error) {
	policy := Policy{
		StopOnFailure:    true,
		MaxExecutionTime: defaultPolicyMaxExecutionTime,
	}

	if rc.ErrorHandling.ExecutionPolicy == "" && len(rc.ExecutionPolicies) == 0 {
		return policy, nil
	}

	configPolicy, ok := rc.ExecutionPolicies[rc.ErrorHandling.ExecutionPolicy]
	if !ok {
		return policy, fmt.Errorf("execution policy '%s': %w", rc.ErrorHandling.ExecutionPolicy, ErrPolicyNotFound)
	}

	if configPolicy.MaxExecutionTime != "" {
		dur, err := time.ParseDuration(configPolicy.MaxExecutionTime)
		if err != nil {
			return policy, fmt.Errorf("invalid max_execution_time in execution policy: %w", err)
		}
		policy.MaxExecutionTime = dur
	}
	policy.StopOnFailure = configPolicy.StopOnFailure
	return policy, nil
}
