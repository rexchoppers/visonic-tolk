VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
PKG := ./cmd/tolk

.PHONY: build run test vet tidy clean release

build:
	go build -ldflags "$(LDFLAGS)" -o tolk $(PKG)

run:
	go run $(PKG)

test:
	go test ./...

vet:
	go vet ./...

tidy:
	go mod tidy

clean:
	rm -rf tolk dist

release:
	GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/tolk-amd64 $(PKG)
	GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/tolk-arm64 $(PKG)
	GOOS=linux GOARCH=arm GOARM=7 go build -ldflags "$(LDFLAGS)" -o dist/tolk-armv7 $(PKG)
