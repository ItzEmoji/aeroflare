default: help

# Show available recipes
help:
    @just --list

# Build the aeroflare binary for the host OS/arch into ./out/aeroflare
build:
    go run script/build.go build

# Build the aeroflare-ci binary for the host OS/arch into ./out/aeroflare-ci
build-ci:
    go run script/build.go build-ci

# Build both aeroflare and aeroflare-ci for the host OS/arch
build-all:
    go run script/build.go build-all

# Cross-compile aeroflare release tarballs (linux/amd64, linux/arm64) into ./out/
dist:
    go run script/build.go dist

# Cross-compile aeroflare-ci release tarballs into ./out/
dist-ci:
    go run script/build.go dist-ci

# Cross-compile release tarballs for both binaries
dist-all:
    go run script/build.go dist-all

# Remove ./out/
clean:
    go run script/build.go clean

# Run golangci-lint
lint:
    golangci-lint run ./...

# Run go test ./...
test:
    go test ./...
