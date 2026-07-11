.PHONY: build
build:
	go run script/build.go build

.PHONY: build-ci
build-ci:
	go run script/build.go build-ci

.PHONY: build-all
build-all:
	go run script/build.go build-all

.PHONY: dist
dist:
	go run script/build.go dist

.PHONY: dist-ci
dist-ci:
	go run script/build.go dist-ci

.PHONY: dist-all
dist-all:
	go run script/build.go dist-all

.PHONY: clean
clean:
	go run script/build.go clean

.PHONY: lint
lint:
	golangci-lint run ./...

.PHONY: test
test:
	go test ./...
