//go:build !(js && wasm)

package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
)

func main() {
	if err := runCLI(context.Background(), os.Stdin, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

func runCLI(ctx context.Context, in io.Reader, out io.Writer) error {
	data, err := io.ReadAll(in)
	if err != nil {
		return fmt.Errorf("reading stdin: %w", err)
	}
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil
	}
	_, err = fmt.Fprintln(out, RunEvaluationJSON(ctx, trimmed))
	return err
}
