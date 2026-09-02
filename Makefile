BINARY := bin/hexlet-go-crawler

.PHONY: build test run lint fmt install clean

build:
	go build -o $(BINARY) ./cmd/hexlet-go-crawler

test:
	go test ./... -race

run:
	go run ./cmd/hexlet-go-crawler $(URL)

lint:
	golangci-lint run ./...

fmt:
	go fmt ./...

install:
	go mod download

clean:
	rm -rf bin
