BINARY=agent-brain

.PHONY: build test lint run install-local clean

build:
	go build -o bin/$(BINARY) ./cmd/agent-brain

test:
	go test ./...

lint:
	go vet ./...

run:
	go run ./cmd/agent-brain

install-local:
	go install ./cmd/agent-brain

clean:
	rm -rf bin
