.PHONY: build run install clean

build:
	go build -o bin/clockr ./cmd/clockr

run: build
	./bin/clockr $(ARGS)

install:
	go install ./cmd/clockr

clean:
	rm -rf bin/
