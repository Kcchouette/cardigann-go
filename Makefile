.PHONY: test lint build fmt vet tidy

test:
	go test ./...

lint:
	golangci-lint run ./...

build:
	go build ./...

fmt:
	gofmt -s -w .

vet:
	go vet ./...

tidy:
	go mod tidy
