SHELL := /bin/sh

APP_NAME ?= gatelens
VERSION ?= dev
REGISTRY ?= ghcr.io/gatelens
IMAGE ?= $(REGISTRY)/$(APP_NAME):$(VERSION)
PLATFORM ?= linux/amd64
GOPROXY ?= https://proxy.golang.org,direct
GOSUMDB ?= sum.golang.org
BIN_DIR ?= bin
KUBECTL ?= kubectl
DOCKER ?= docker

.DEFAULT_GOAL := help
.PHONY: help fmt test build run-demo image image-push deploy undeploy clean

help: ## Show available targets.
	@awk 'BEGIN {FS = ":.*##"}; /^[a-zA-Z_-]+:.*##/ {printf "\033[36m%-14s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

fmt: ## Format all Go source files.
	gofmt -w $$(find cmd internal -name '*.go' -type f)

test: ## Run Go unit tests.
	go test ./...

build: ## Build a local binary in bin/.
	@mkdir -p $(BIN_DIR)
	go build -trimpath -ldflags="-s -w" -o $(BIN_DIR)/$(APP_NAME) ./cmd/gatelens

run-demo: ## Run the local UI with deterministic demo data.
	GATELENS_MODE=demo GATELENS_ADDR=:8080 go run ./cmd/gatelens

image: ## Build the container image. Override IMAGE, VERSION, or PLATFORM as needed.
	$(DOCKER) build --platform $(PLATFORM) --build-arg GOPROXY=$(GOPROXY) --build-arg GOSUMDB=$(GOSUMDB) --tag $(IMAGE) .

image-push: ## Push IMAGE to its registry.
	$(DOCKER) push $(IMAGE)

deploy: ## Apply the Kubernetes manifest with the selected IMAGE.
	$(KUBECTL) apply -f deploy/kubernetes.yaml
	$(KUBECTL) -n gatelens-system set image deployment/gatelens gatelens=$(IMAGE)
	$(KUBECTL) -n gatelens-system rollout status deployment/gatelens

undeploy: ## Remove the GateLens Kubernetes resources.
	$(KUBECTL) delete -f deploy/kubernetes.yaml

clean: ## Remove local build outputs.
	rm -rf $(BIN_DIR)

