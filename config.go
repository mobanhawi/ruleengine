package ruleengine

import "github.com/mobanhawi/ruleengine/config"

const (
	// SelectorAnd requires all rules in a ruleset to pass.
	SelectorAnd = config.SelectorAnd
	// SelectorOr requires at least one rule in a ruleset to pass.
	SelectorOr = config.SelectorOr
	// SelectorNone requires zero rules in a ruleset to pass (logical NOR).
	SelectorNone = config.SelectorNone
)

// Type aliases re-exporting configuration and domain types from ruleengine/config.
type (
	SelectorType    = config.SelectorType
	Policy          = config.Policy
	RulesetConfig   = config.RulesetConfig
	Rule            = config.Rule
	Ruleset         = config.Ruleset
	Metadata        = config.Metadata
	ExecutionPolicy = config.ExecutionPolicy
	ErrorHandling   = config.ErrorHandling
	Environment     = config.Environment
)

var (
	// NewRulesetConfig loads and parses the YAML configuration file at configPath.
	NewRulesetConfig = config.Load
	// NewRulesetConfigFromReader loads a YAML/JSON configuration from an io.Reader.
	NewRulesetConfigFromReader = config.LoadFromReader
	// NewRulesetConfigFromBytes loads a YAML/JSON configuration from raw bytes.
	NewRulesetConfigFromBytes = config.LoadFromBytes
)
