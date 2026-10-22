.PHONY: test lint build fmt vet tidy test-race fmt-check coverage cover-html

test:
	go test ./...

test-race:
	go test -race ./...

lint:
	golangci-lint run ./...

build:
	go build ./...

fmt:
	gofmt -s -w .

fmt-check:
	@out=$$(gofmt -s -l .); if [ -n "$$out" ]; then echo "gofmt needed on:"; echo "$$out"; exit 1; fi

vet:
	go vet ./...

tidy:
	go mod tidy

coverage:
	go test -race -coverprofile=coverage.out -covermode=atomic ./...
	go tool cover -func=coverage.out | tail -1

cover-html: coverage
	go tool cover -html=coverage.out -o coverage.html
