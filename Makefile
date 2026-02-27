.PHONY: build install clean

build:
	CGO_ENABLED=1 go build -o bin/clockr ./cmd/clockr

install:
	CGO_ENABLED=1 go install ./cmd/clockr

clean:
	rm -rf bin/
