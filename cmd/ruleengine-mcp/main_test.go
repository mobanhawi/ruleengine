package main

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"
)

const (
	testRulesConfigPath      = "../../testdata/rules.yml"
	testNonExistentConfig    = "../../testdata/does_not_exist.yml"
	testInitializeRPCRequest = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}` + "\n"
)

func sendInitializeAndReadResponse(t *testing.T, args []string) (stdout string, stderr string, err error) {
	t.Helper()
	inReader, inWriter := io.Pipe()
	outReader, outWriter := io.Pipe()

	var errOut bytes.Buffer
	errCh := make(chan error, 1)
	go func() {
		errCh <- run(context.Background(), args, inReader, outWriter, &errOut)
		_ = outWriter.Close()
	}()

	if _, writeErr := io.WriteString(inWriter, testInitializeRPCRequest); writeErr != nil {
		t.Fatalf("writing initialize request: %v", writeErr)
	}

	scanner := bufio.NewScanner(outReader)
	var line string
	if scanner.Scan() {
		line = scanner.Text()
	}
	_ = inWriter.Close()
	_ = outReader.Close()

	runErr := <-errCh
	return line, errOut.String(), runErr
}

func TestRun(t *testing.T) {
	t.Run("GivenDefaultFlagsAndInitializeRequest_WhenRun_ThenStartsReadOnlyServerAndResponds", func(t *testing.T) {
		stdout, stderr, err := sendInitializeAndReadResponse(t, nil)
		if err != nil {
			t.Fatalf("run() unexpected error = %v", err)
		}
		if !strings.Contains(stdout, `"ruleengine-mcp"`) {
			t.Errorf("stdout = %q, expected serverInfo ruleengine-mcp", stdout)
		}
		if stderr != "" {
			t.Errorf("stderr = %q, expected empty for read-only default", stderr)
		}
	})

	t.Run("GivenValidConfigFileAndAllowAuthoringFlag_WhenRun_ThenLogsAuthoringNoticeAndServes", func(t *testing.T) {
		args := []string{
			"-config", testRulesConfigPath,
			"-env", "development",
			"-vars", "user,request",
			"-allow-authoring",
		}
		stdout, stderr, err := sendInitializeAndReadResponse(t, args)
		if err != nil {
			t.Fatalf("run() unexpected error = %v", err)
		}
		if !strings.Contains(stderr, "rule authoring enabled") {
			t.Errorf("stderr = %q, expected authoring notice", stderr)
		}
		if !strings.Contains(stdout, `"ruleengine-mcp"`) {
			t.Errorf("stdout = %q, expected initialize response", stdout)
		}
	})

	t.Run("GivenNonExistentConfigPath_WhenRun_ThenReturnsInitializationError", func(t *testing.T) {
		var out, errOut bytes.Buffer
		err := run(context.Background(), []string{"-config", testNonExistentConfig}, strings.NewReader(""), &out, &errOut)
		if err == nil {
			t.Fatal("run() with missing config expected error, got nil")
		}
	})

	t.Run("GivenUnknownFlag_WhenRun_ThenReturnsFlagParseError", func(t *testing.T) {
		var out, errOut bytes.Buffer
		err := run(context.Background(), []string{"-unknown-flag"}, strings.NewReader(""), &out, &errOut)
		if err == nil {
			t.Fatal("run() with unknown flag expected error, got nil")
		}
	})

	t.Run("GivenCancelledContextWhileBlockedOnInput_WhenRun_ThenExitsCleanlyWithoutError", func(t *testing.T) {
		pr, pw := io.Pipe()
		defer func() {
			_ = pw.Close()
			_ = pr.Close()
		}()

		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			time.Sleep(20 * time.Millisecond)
			cancel()
		}()

		var out, errOut bytes.Buffer
		if err := run(ctx, nil, pr, &out, &errOut); err != nil {
			t.Fatalf("run() on cancelled context returned error = %v, want nil", err)
		}
	})
}
