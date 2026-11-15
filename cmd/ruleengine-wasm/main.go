//go:build js && wasm

package main

import (
	"context"
	"syscall/js"
)

func main() {
	js.Global().Set("ruleengineEvaluate", js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) < 1 {
			return `{"ok":false,"error":"missing request JSON argument"}`
		}
		return RunEvaluationJSON(context.Background(), args[0].String())
	}))

	select {}
}
