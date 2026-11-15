/* global importScripts, Go, ruleengineEvaluate */
importScripts("wasm_exec.js");

let wasmReadyPromise = null;

function initWasm() {
  if (wasmReadyPromise) {
    return wasmReadyPromise;
  }
  wasmReadyPromise = (async () => {
    const go = new Go();
    let result;
    if (WebAssembly.instantiateStreaming) {
      try {
        result = await WebAssembly.instantiateStreaming(
          fetch("ruleengine.wasm"),
          go.importObject
        );
      } catch (_) {
        const resp = await fetch("ruleengine.wasm");
        const bytes = await resp.arrayBuffer();
        result = await WebAssembly.instantiate(bytes, go.importObject);
      }
    } else {
      const resp = await fetch("ruleengine.wasm");
      const bytes = await resp.arrayBuffer();
      result = await WebAssembly.instantiate(bytes, go.importObject);
    }
    go.run(result.instance);
    return true;
  })();
  return wasmReadyPromise;
}

self.onmessage = async (event) => {
  const { id, type, payload } = event.data || {};
  try {
    await initWasm();
    if (type === "init") {
      self.postMessage({ id, type: "ready" });
      return;
    }
    if (type === "evaluate") {
      const rawJSON = ruleengineEvaluate(JSON.stringify(payload));
      self.postMessage({ id, type: "result", data: JSON.parse(rawJSON) });
    }
  } catch (err) {
    self.postMessage({
      id,
      type: "result",
      data: {
        ok: false,
        error: err && err.message ? err.message : String(err),
      },
    });
  }
};
