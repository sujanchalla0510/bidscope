//go:build js && wasm

// JavaScript bindings for the BidScope web tester.
//
// bidscopeProfile(jsonl, qps) -> JSON string
//
//	{"ok":true,"result":{...}} on success, {"ok":false,"error":"..."} on
//	failure. The result document is the same profile.Result the CLI emits
//	with -json.
//
// bidscopeGenerate(n, seed, profile) -> JSON string
//
//	{"ok":true,"sample":"<jsonl>"} or {"ok":false,"error":"..."}.
package main

import (
	"encoding/json"
	"syscall/js"
)

// jsResponse is the envelope every binding returns: JSON in, JSON out, so
// JavaScript never has to deal with Go exceptions.
type jsResponse struct {
	OK     bool            `json:"ok"`
	Error  string          `json:"error,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Sample string          `json:"sample,omitempty"`
}

// profileBinding implements bidscopeProfile(jsonl, qps).
func profileBinding(_ js.Value, args []js.Value) any {
	resp := jsResponse{}
	jsonl, qps := "", 0.0
	if len(args) > 0 && args[0].Type() == js.TypeString {
		jsonl = args[0].String()
	}
	if len(args) > 1 && args[1].Type() == js.TypeNumber {
		qps = args[1].Float()
	}
	out, err := ProfileJSONL(jsonl, qps)
	if err != nil {
		resp.Error = err.Error()
	} else {
		resp.OK = true
		resp.Result = json.RawMessage(out)
	}
	b, _ := json.Marshal(resp)
	return string(b)
}

// generateBinding implements bidscopeGenerate(n, seed, profile).
func generateBinding(_ js.Value, args []js.Value) any {
	resp := jsResponse{}
	n, seed, prof := 500, int64(7), "mixed"
	if len(args) > 0 && args[0].Type() == js.TypeNumber {
		n = args[0].Int()
	}
	if len(args) > 1 && args[1].Type() == js.TypeNumber {
		seed = int64(args[1].Int())
	}
	if len(args) > 2 && args[2].Type() == js.TypeString {
		prof = args[2].String()
	}
	sample, err := GenerateJSONL(n, seed, prof)
	if err != nil {
		resp.Error = err.Error()
	} else {
		resp.OK = true
		resp.Sample = sample
	}
	b, _ := json.Marshal(resp)
	return string(b)
}

func main() {
	js.Global().Set("bidscopeProfile", js.FuncOf(profileBinding))
	js.Global().Set("bidscopeGenerate", js.FuncOf(generateBinding))
	select {} // keep the module alive for the page's lifetime
}
