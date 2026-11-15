package ruleengine

import "github.com/mobanhawi/ruleengine/cel"

// NewDefaultEnv creates a standard CEL environment pre-configured with "globals",
// the built-in "timestamp(string)" and "now()" functions, and any custom variable names
// (such as "user", "request") declared as dynamic types.
var NewDefaultEnv = cel.NewDefaultEnv
