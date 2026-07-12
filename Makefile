.DEFAULT_GOAL := help

.PHONY: build
build: ## Build the aeroflare binary for the host OS/arch into ./out/aeroflare
	go run scripts/build.go build

.PHONY: build-ci
build-ci: ## Build the aeroflare-ci binary for the host OS/arch into ./out/aeroflare-ci
	go run scripts/build.go build-ci

.PHONY: build-all
build-all: ## Build both aeroflare and aeroflare-ci for the host OS/arch
	go run scripts/build.go build-all

.PHONY: dist
dist: ## Cross-compile aeroflare release tarballs (linux/amd64, linux/arm64) into ./out/
	go run scripts/build.go dist

.PHONY: dist-ci
dist-ci: ## Cross-compile aeroflare-ci release tarballs into ./out/
	go run scripts/build.go dist-ci

.PHONY: dist-all
dist-all: ## Cross-compile release tarballs for both binaries
	go run scripts/build.go dist-all

.PHONY: install
install: ## Build aeroflare and install it to PREFIX/bin (default /usr/local)
	go run scripts/build.go install

.PHONY: install-ci
install-ci: ## Build aeroflare-ci and install it to PREFIX/bin (default /usr/local)
	go run scripts/build.go install-ci

.PHONY: install-all
install-all: ## Build both and install them to PREFIX/bin (default /usr/local)
	go run scripts/build.go install-all

.PHONY: install-release
install-release: ## Fetch the aeroflare release from GitHub and install it to PREFIX/bin
	go run scripts/build.go install-release

.PHONY: install-release-ci
install-release-ci: ## Fetch the aeroflare-ci release from GitHub and install it to PREFIX/bin
	go run scripts/build.go install-release-ci

.PHONY: install-release-all
install-release-all: ## Fetch both releases from GitHub and install them to PREFIX/bin
	go run scripts/build.go install-release-all

.PHONY: clean
clean: ## Remove ./out/
	go run scripts/build.go clean

.PHONY: lint
lint: ## Run golangci-lint
	golangci-lint run ./...

.PHONY: test
test: ## Run go test ./...
	go test ./...

.PHONY: check-api
check-api: ## Verify no internal/ types leak into the public pkg/ API
	@./scripts/check_api_leaks.sh

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-12s\033[0m %s\n", $$1, $$2}'
