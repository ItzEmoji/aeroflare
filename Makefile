.DEFAULT_GOAL := help

.PHONY: build
build: ## Build the aeroflare binary for the host OS/arch into ./out/aeroflare
	go run script/build.go build

.PHONY: build-ci
build-ci: ## Build the aeroflare-ci binary for the host OS/arch into ./out/aeroflare-ci
	go run script/build.go build-ci

.PHONY: build-all
build-all: ## Build both aeroflare and aeroflare-ci for the host OS/arch
	go run script/build.go build-all

.PHONY: dist
dist: ## Cross-compile aeroflare release tarballs (linux/amd64, linux/arm64) into ./out/
	go run script/build.go dist

.PHONY: dist-ci
dist-ci: ## Cross-compile aeroflare-ci release tarballs into ./out/
	go run script/build.go dist-ci

.PHONY: dist-all
dist-all: ## Cross-compile release tarballs for both binaries
	go run script/build.go dist-all

.PHONY: clean
clean: ## Remove ./out/
	go run script/build.go clean

.PHONY: lint
lint: ## Run golangci-lint
	golangci-lint run ./...

.PHONY: test
test: ## Run go test ./...
	go test ./...

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-12s\033[0m %s\n", $$1, $$2}'
