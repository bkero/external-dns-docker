BINARY     := external-dns-docker
MODULE     := github.com/bkero/external-dns-docker
CMD        := ./cmd/external-dns-docker
BIN_DIR    := bin
IMAGE_NAME := external-dns-docker
IMAGE_TAG  ?= latest

PLATFORMS  := linux/amd64,linux/arm64

.PHONY: all build test lint docker clean

all: build

## build: compile the binary into bin/
build:
	mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/$(BINARY) $(CMD)

## test: run unit tests
test:
	go test ./...

## test-coverage: run unit tests with coverage report
test-coverage:
	go test -cover ./...

## lint: run golangci-lint
lint:
	golangci-lint run ./...

## docker: build multi-arch Docker image (requires buildx)
docker:
	docker buildx build \
		--platform $(PLATFORMS) \
		-t $(IMAGE_NAME):$(IMAGE_TAG) \
		.

## docker-push: build and push multi-arch Docker image
docker-push:
	docker buildx build \
		--platform $(PLATFORMS) \
		-t $(IMAGE_NAME):$(IMAGE_TAG) \
		--push \
		.

## clean: remove build artifacts
clean:
	rm -rf $(BIN_DIR)

## help: list available targets
help:
	@grep -E '^## ' Makefile | sed 's/^## //'
