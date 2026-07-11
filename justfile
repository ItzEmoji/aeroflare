build:
    go run script/build.go build

build-ci:
    go run script/build.go build-ci

build-all:
    go run script/build.go build-all

dist:
    go run script/build.go dist

dist-ci:
    go run script/build.go dist-ci

dist-all:
    go run script/build.go dist-all

clean:
    go run script/build.go clean

lint:
    golangci-lint run ./...

test:
    go test ./...
