package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/mobanhawi/ruleengine"
	"github.com/mobanhawi/ruleengine/mcp"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, in io.Reader, out io.Writer, errOut io.Writer) error {
	fs := flag.NewFlagSet("ruleengine-mcp", flag.ContinueOnError)
	fs.SetOutput(errOut)

	configPath := fs.String("config", "", "Optional path to rules YAML configuration file")
	environment := fs.String("env", "development", "Environment name for configuration overrides")
	varsFlag := fs.String("vars", "user,request", "Comma-separated CEL context variable names")
	allowAuthoring := fs.Bool("allow-authoring", false,
		"Expose upsert_rule/upsert_ruleset tools so clients can change rules (in-memory only, not persisted)")

	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parsing flags: %w", err)
	}

	celEnv, err := ruleengine.NewDefaultEnv(strings.Split(*varsFlag, ",")...)
	if err != nil {
		return fmt.Errorf("failed to create CEL environment: %w", err)
	}

	var engine *ruleengine.RuleEngine
	if *configPath != "" {
		engine, err = ruleengine.NewRuleEngine(*configPath, *environment, celEnv, ruleengine.WithOptimise())
	} else {
		engine, err = ruleengine.NewRuleEngineFromConfig(&ruleengine.RulesetConfig{}, *environment, celEnv, ruleengine.WithOptimise())
	}
	if err != nil {
		return fmt.Errorf("failed to initialize rule engine: %w", err)
	}

	var serverOpts []mcp.Option
	if *allowAuthoring {
		serverOpts = append(serverOpts, mcp.WithAuthoring())
		_, _ = fmt.Fprintln(errOut, "ruleengine-mcp: rule authoring enabled; changes are not persisted")
	}

	srv := mcp.NewServer(engine, serverOpts...)
	if err := srv.Serve(ctx, in, out); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("mcp server error: %w", err)
	}
	return nil
}
