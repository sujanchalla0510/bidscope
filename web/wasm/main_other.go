//go:build !js || !wasm

// No-op main for non-WebAssembly builds: the web tester bridge only runs
// in browsers. This keeps `go build ./...` and `go vet ./...` green on
// every platform while the real entry point lives in main_js.go.
package main

func main() {}
