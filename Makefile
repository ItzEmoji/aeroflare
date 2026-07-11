.PHONY: build
build:
	go run script/build.go bin/aeroflare

.PHONY: build-ci
build-ci:
	go run script/build.go bin/aeroflare-ci

.PHONY: dist
dist:
	go run script/build.go dist

.PHONY: clean
clean:
	go run script/build.go clean

.PHONY: lint
lint:
	golangci-lint run ./...

.PHONY: test
test:
	go test ./...
