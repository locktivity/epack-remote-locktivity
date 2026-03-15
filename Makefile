.PHONY: build build-dev test test-cover lint clean install tidy sdk-test sdk-run capabilities version

BINARY_NAME := epack-remote-locktivity
BUILD_DIR := ./bin
CMD_DIR := ./cmd/$(BINARY_NAME)

# Build the release binary
build:
	mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(BINARY_NAME) $(CMD_DIR)

# Build the dev binary
build-dev:
	mkdir -p $(BUILD_DIR)
	go build -tags dev -o $(BUILD_DIR)/$(BINARY_NAME) $(CMD_DIR)

# Run tests
test:
	go test -v ./...

# Run tests with coverage
test-cover:
	go test -v -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

# Lint code (downloads golangci-lint binary to match CI)
GOLANGCI_LINT_VERSION := v2.9.0
GOLANGCI_LINT := ./bin/golangci-lint

$(GOLANGCI_LINT):
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/HEAD/install.sh | sh -s -- -b ./bin $(GOLANGCI_LINT_VERSION)

lint: $(GOLANGCI_LINT)
	$(GOLANGCI_LINT) run ./...

# Clean build artifacts
clean:
	rm -rf $(BUILD_DIR)
	rm -f coverage.out coverage.html

# Install to GOBIN
install:
	go install $(CMD_DIR)

# Run go mod tidy
tidy:
	go mod tidy

# SDK development commands
sdk-test: build
	"$${GOBIN:-$$(go env GOPATH)/bin}/epack-conformance" remote $(BUILD_DIR)/$(BINARY_NAME) --level standard

sdk-run: build
	epack sdk run $(BUILD_DIR)/$(BINARY_NAME)

# Check capabilities output
capabilities: build
	$(BUILD_DIR)/$(BINARY_NAME) --capabilities

# Check version output
version: build
	$(BUILD_DIR)/$(BINARY_NAME) --version
