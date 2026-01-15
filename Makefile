.PHONY: build run clean test lint help setup install deps

# define variables
BINARY_NAME=chainscan-sync
MAIN_PATH=./cmd/relayer
BUILD_DIR=./bin
GO=go
GOFLAGS=-v
VERSION=1.0.0

# default target
help:
	@echo "Chainscan-Sync Makefile"
	@echo ""
	@echo "Available commands:"
	@echo "  make setup      - Initialize the project (create directories, download dependencies)"
	@echo "  make deps       - Download Go dependencies"
	@echo "  make build      - Build the project"
	@echo "  make run        - Run the project (using config.yaml)"
	@echo "  make install    - Install to system path"
	@echo "  make clean      - Clean build artifacts"
	@echo "  make test       - Run tests"
	@echo "  make lint       - Run code linting"
	@echo "  make help       - Show this help message"

# Initialize project
setup:
	@echo "Initializing project..."
	@mkdir -p $(BUILD_DIR)
	@mkdir -p logs
	@mkdir -p data
	@mkdir -p abis
	@echo "Directories created"
	@if [ ! -f "config.yaml" ]; then \
		cp config.example.yaml config.yaml; \
		echo "config.yaml created"; \
	fi
	@$(MAKE) deps
	@echo "Initialization complete"

# Download dependencies
deps:
	@echo "Downloading Go dependencies..."
	$(GO) mod download
	$(GO) mod tidy
	@echo "Dependencies downloaded"

# Build project
build:
	@echo "Building $(BINARY_NAME) v$(VERSION)..."
	@mkdir -p $(BUILD_DIR)
	$(GO) build $(GOFLAGS) -ldflags "-X main.version=$(VERSION)" -o $(BUILD_DIR)/$(BINARY_NAME) $(MAIN_PATH)
	@echo "Build complete: $(BUILD_DIR)/$(BINARY_NAME)"

# Run project
run: build
	@echo "Running $(BINARY_NAME)..."
	$(BUILD_DIR)/$(BINARY_NAME) -config=config.yaml

# Run (development mode)
dev:
	@echo "Running (development mode)..."
	$(GO) run $(MAIN_PATH)/main.go -config=config.yaml

# Install to system
install: build
	@echo "Installing to system..."
	@cp $(BUILD_DIR)/$(BINARY_NAME) /usr/local/bin/
	@echo "Installation complete"

# Clean
clean:
	@echo "Cleaning..."
	@rm -rf $(BUILD_DIR)
	@rm -rf logs/*.log
	@echo "Clean complete"

# Deep clean (including data)
clean-all: clean
	@echo "Deep cleaning..."
	@rm -rf data/
	@echo "Deep clean complete"

# Run tests
test:
	@echo "Running tests..."
	$(GO) test -v ./...

# Run tests (with coverage)
test-coverage:
	@echo "Running tests (with coverage)..."
	$(GO) test -v -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

# Code linting
lint:
	@echo "Running code linting..."
	@which golangci-lint > /dev/null || (echo "Please install golangci-lint first: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest" && exit 1)
	golangci-lint run ./...

# Code formatting
fmt:
	@echo "Formatting code..."
	$(GO) fmt ./...
	@echo "Formatting complete"

# Check database connection
check-db:
	@echo "Checking database connection..."
	@psql -h localhost -U chainscan_user -d chainscan -c "SELECT version();" || echo "Database connection failed"

# Docker related
docker-build:
	@echo "Building Docker image..."
	docker build -t chainscan-sync:$(VERSION) .

docker-run:
	@echo "Running Docker container..."
	docker run -d --name chainscan-sync \
		-v $(PWD)/config.yaml:/app/config.yaml \
		-v $(PWD)/logs:/app/logs \
		chainscan-sync:$(VERSION)


# Build all platforms
build-all:
	@echo "Building all platforms..."
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 $(GO) build -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 $(MAIN_PATH)
	GOOS=darwin GOARCH=amd64 $(GO) build -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-amd64 $(MAIN_PATH)
	GOOS=darwin GOARCH=arm64 $(GO) build -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 $(MAIN_PATH)
	GOOS=windows GOARCH=amd64 $(GO) build -o $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe $(MAIN_PATH)
	@echo "All platforms built successfully"

