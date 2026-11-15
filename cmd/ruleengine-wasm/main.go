//go:build js && wasm

package main

import (
	"context"
	"syscall/js"
)

func main() {
	js.Global().Set("ruleengineEvaluate", js.FuncOf(func(_ js.Value, args []js.Value) any {
		strArgs := make([]string, len(args))
		for i, a := range args {
			strArgs[i] = a.String()
		}
		return EvaluateArgs(context.Background(), strArgs)
	}))

	select {}
}
