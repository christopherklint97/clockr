.PHONY: build run install clean vet test

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

clean:
	rm -rf bin/
