default: help

# Show available recipes
help:
    @just --list

# Build the aeroflare binary for the host OS/arch into ./out/aeroflare
build:
    go run scripts/build.go build

# Build the aeroflare-ci binary for the host OS/arch into ./out/aeroflare-ci
build-ci:
    go run scripts/build.go build-ci

# Build both aeroflare and aeroflare-ci for the host OS/arch
build-all:
    go run scripts/build.go build-all

# Cross-compile aeroflare release tarballs (linux/amd64, linux/arm64) into ./out/
dist:
    go run scripts/build.go dist

# Cross-compile aeroflare-ci release tarballs into ./out/
dist-ci:
    go run scripts/build.go dist-ci

# Cross-compile release tarballs for both binaries
dist-all:
    go run scripts/build.go dist-all

# Build aeroflare and install it to PREFIX/bin (default /usr/local)
install *args:
    go run scripts/build.go install {{args}}

# Build aeroflare-ci and install it to PREFIX/bin (default /usr/local)
install-ci *args:
    go run scripts/build.go install-ci {{args}}

# Build both and install them to PREFIX/bin (default /usr/local)
install-all *args:
    go run scripts/build.go install-all {{args}}

# Fetch the aeroflare release from GitHub and install it to PREFIX/bin
install-release *args:
    go run scripts/build.go install-release {{args}}

# Fetch the aeroflare-ci release from GitHub and install it to PREFIX/bin
install-release-ci *args:
    go run scripts/build.go install-release-ci {{args}}

# Fetch both releases from GitHub and install them to PREFIX/bin
install-release-all *args:
    go run scripts/build.go install-release-all {{args}}

# Build the aeroflare-proxy container image
docker:
    docker build -t aeroflare-proxy .

# Remove ./out/
clean:
    go run scripts/build.go clean

# Run golangci-lint
lint:
    golangci-lint run ./...

# Run go test ./...
test:
    go test ./...
