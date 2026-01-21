BINARY_NAME=ainspector
BINARY_PATH=bin/$(BINARY_NAME)
GO=go

.PHONY: build run clean fmt lint test deps

build:
	@mkdir -p bin
	$(GO) build -o $(BINARY_PATH) .

run: build
	./$(BINARY_PATH)

clean:
	rm -rf bin
	$(GO) clean

fmt:
	$(GO) fmt ./...

lint:
	golangci-lint run ./...

test:
	$(GO) test -v ./...

deps:
	$(GO) mod tidy
