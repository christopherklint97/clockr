.PHONY: build install clean

build:
	go build -o bin/clockr ./cmd/clockr

install:
	go install ./cmd/clockr

clean:
	rm -rf bin/
