.PHONY: build vet test clean

build:
	go build ./...

vet:
	go vet ./...

test:
	go test ./...

clean:
	go clean

# Web tester: compile the profiler to WebAssembly and stage the static site.
# bidscope.wasm and wasm_exec.js are build artifacts (git-ignored); the
# Pages deploy workflow runs this target in CI.
WEB_DIR := web/site
WASM_OUT := $(WEB_DIR)/bidscope.wasm

.PHONY: web
web:
	GOOS=js GOARCH=wasm go build -ldflags="-s -w" -o $(WASM_OUT) ./web/wasm
	cp $$(go env GOROOT)/lib/wasm/wasm_exec.js $(WEB_DIR)/wasm_exec.js
