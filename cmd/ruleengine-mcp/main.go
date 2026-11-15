package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/mobanhawi/ruleengine"
	"github.com/mobanhawi/ruleengine/mcp"
)

func main() {
	configPath := flag.String("config", "", "Optional path to rules YAML configuration file")
	environment := flag.String("env", "development", "Environment name for configuration overrides")
	varsFlag := flag.String("vars", "user,request", "Comma-separated CEL context variable names")
	allowAuthoring := flag.Bool("allow-authoring", false,
		"Expose upsert_rule/upsert_ruleset tools so clients can change rules (in-memory only, not persisted)")
	flag.Parse()

	celEnv, err := ruleengine.NewDefaultEnv(strings.Split(*varsFlag, ",")...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create CEL environment: %v\n", err)
		os.Exit(1)
	}

	var engine *ruleengine.RuleEngine
	if *configPath != "" {
		engine, err = ruleengine.NewRuleEngine(*configPath, *environment, celEnv, ruleengine.WithOptimise())
	} else {
		engine, err = ruleengine.NewRuleEngineFromConfig(&ruleengine.RulesetConfig{}, *environment, celEnv, ruleengine.WithOptimise())
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize rule engine: %v\n", err)
		os.Exit(1)
	}

	var serverOpts []mcp.Option
	if *allowAuthoring {
		serverOpts = append(serverOpts, mcp.WithAuthoring())
		fmt.Fprintln(os.Stderr, "ruleengine-mcp: rule authoring enabled; changes are not persisted")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Serve closes os.Stdin on cancellation to release its reader goroutine.
	srv := mcp.NewServer(engine, serverOpts...)
	if err := srv.Serve(ctx, os.Stdin, os.Stdout); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintf(os.Stderr, "mcp server error: %v\n", err)
		os.Exit(1)
	}
}
