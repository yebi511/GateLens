SHELL := /bin/sh

APP_NAME ?= gatelens
VERSION ?= dev
REGISTRY ?= ghcr.io/gatelens
API_IMAGE ?= $(REGISTRY)/$(APP_NAME)-api:$(VERSION)
WEB_IMAGE ?= $(REGISTRY)/$(APP_NAME)-web:$(VERSION)
PLATFORM ?= linux/amd64
GOPROXY ?= https://proxy.golang.org,direct
GOSUMDB ?= sum.golang.org
BIN_DIR ?= bin
WEB_DIR ?= frontend
KUBECTL ?= kubectl
DOCKER ?= docker
NPM ?= npm

.DEFAULT_GOAL := help
.PHONY: help fmt test test-api test-web build build-api build-web build-all frontend-toolchain frontend-install run-api-demo run-web image image-api image-web image-push deploy undeploy clean

help: ## Show available targets.
	@awk 'BEGIN {FS = ":.*##"}; /^[a-zA-Z_-]+:.*##/ {printf "\033[36m%-18s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

fmt: ## Format all Go source files.
	gofmt -w $$(find cmd internal -name '*.go' -type f)

test: test-api test-web ## Run backend and frontend checks.

test-api: ## Run Go unit tests.
	go test ./...

test-web: frontend-install ## Type-check the Vue frontend.
	cd $(WEB_DIR) && $(NPM) run typecheck

build: build-api ## Build the API binary (backward-compatible alias).

build-api: ## Build the API binary in bin/.
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o $(BIN_DIR)/$(APP_NAME)-api ./cmd/gatelens

build-web: frontend-install ## Build production frontend assets.
	cd $(WEB_DIR) && $(NPM) run build

build-all: build-api build-web ## Build the API and frontend.

frontend-toolchain: ## Validate the Node.js and npm versions used by the frontend.
	@cd $(WEB_DIR) && node scripts/check-toolchain.cjs

frontend-install: frontend-toolchain ## Install locked frontend dependencies.
	cd $(WEB_DIR) && $(NPM) ci

run-api-demo: ## Run the API with deterministic demo data on :8080.
	GATELENS_MODE=demo GATELENS_ADDR=:8080 go run ./cmd/gatelens

run-web: frontend-install ## Run the Vue development server on :5173.
	cd $(WEB_DIR) && $(NPM) run dev

image: image-api image-web ## Build both container images.

image-api: ## Build the API container image.
	$(DOCKER) build --platform $(PLATFORM) --build-arg GOPROXY=$(GOPROXY) --build-arg GOSUMDB=$(GOSUMDB) --tag $(API_IMAGE) -f Dockerfile .

image-web: ## Build the frontend container image.
	$(DOCKER) build --platform $(PLATFORM) --tag $(WEB_IMAGE) -f frontend/Dockerfile .

image-push: ## Push both container images.
	$(DOCKER) push $(API_IMAGE)
	$(DOCKER) push $(WEB_IMAGE)

deploy: ## Apply manifests and roll out the selected API and Web images.
	$(KUBECTL) apply -f deploy/kubernetes.yaml
	$(KUBECTL) -n gatelens-system set image deployment/gatelens-api api=$(API_IMAGE)
	$(KUBECTL) -n gatelens-system set image deployment/gatelens-web web=$(WEB_IMAGE)
	$(KUBECTL) -n gatelens-system set image deployment/gatelens-agent agent=$(API_IMAGE)
	$(KUBECTL) -n gatelens-system rollout status deployment/gatelens-api
	$(KUBECTL) -n gatelens-system rollout status deployment/gatelens-web

	$(KUBECTL) -n gatelens-system rollout status deployment/gatelens-agent
undeploy: ## Remove the GateLens Kubernetes resources.
	$(KUBECTL) delete -f deploy/kubernetes.yaml

clean: ## Remove local build outputs.
	rm -rf $(BIN_DIR) $(WEB_DIR)/dist
