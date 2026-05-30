.PHONY: help build test lint proto tidy clean run-server run-server-config run-client docker-build docker-run

# Default configuration file path
CONFIG_FILE ?= configs/config.yaml

help: ## Show this help
	@type $(MAKEFILE_LIST) | findstr /R "^[a-zA-Z_-][a-zA-Z_-]*:.*##"

build: ## Build the project
	go build ./...

build-server: ## Build the server binary
	go build -o bin/server cmd/server/main.go

build-client: ## Build the client binary
	go build -o bin/client cmd/client/main.go

test: ## Run tests
	go test ./...

test-race: ## Run tests with race detector
	go test -race ./...

test-verbose: ## Run tests with verbose output
	go test -v ./...

lint: ## Run linter
	golangci-lint run

proto: ## Generate Go code from protobuf definitions
	protoc --go_opt=module=golang-project.local --go-grpc_opt=module=golang-project.local --go_out=. --go-grpc_out=. proto/*.proto

tidy: ## Tidy go modules
	go mod tidy

clean: ## Clean build artifacts
	go clean
	rm -f bin/*

run-server: ## Run the gRPC server (default config)
	go run cmd/server/main.go

run-server-config: ## Run the gRPC server with config file (CONFIG_FILE=configs/config.yaml)
	go run cmd/server/main.go --config=$(CONFIG_FILE)

run-server-custom: ## Run the gRPC server with custom flags (example)
	go run cmd/server/main.go --port=50051 --interval=10 --window=30 --collectors=cpu,disk,load

run-client: ## Run the gRPC client
	go run cmd/client/main.go

run-client-custom: ## Run the gRPC client with custom flags (example)
	go run cmd/client/main.go --server=localhost:50051 --interval=10 --window=30

docker-build: ## Build Docker image
	docker build -t golang-project .

docker-run: ## Run Docker container
	docker run -p 50051:50051 golang-project

docker-run-config: ## Run Docker container with config file mounted
	docker run -p 50051:50051 -v $(PWD)/$(CONFIG_FILE):/app/configs/config.yaml golang-project --config=/app/configs/config.yaml

vet: ## Run go vet
	go vet ./...

fmt: ## Format Go code
	go fmt ./...

check: vet lint test ## Run all checks (vet, lint, test)