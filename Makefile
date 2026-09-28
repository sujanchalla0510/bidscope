.PHONY: build vet test clean

build:
	go build ./...

vet:
	go vet ./...

test:
	go test ./...

clean:
	go clean
