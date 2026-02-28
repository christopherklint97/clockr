.PHONY: build run install clean vet test test-integration

build:
	go build -o bin/clockr ./cmd/clockr

run: build
	./bin/clockr $(ARGS)

install:
	go install ./cmd/clockr

vet:
	go vet ./...

test: vet
	go test ./...

test-integration: vet
	go test -tags integration -v -timeout 120s ./internal/ai/...

clean:
	rm -rf bin/
