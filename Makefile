VERSION ?= $(shell git describe --tags --always 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/thecoooler/dleplugin/internal/app.Version=$(VERSION)
CGO_ENABLED ?= 0
export CGO_ENABLED

.PHONY: build test lint pipeline install golden clean

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/dleplugin ./cmd/dleplugin

test:
	go test ./...

lint:
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi
	go vet ./...

pipeline: lint test

install:
	go install -trimpath -ldflags "$(LDFLAGS)" ./cmd/dleplugin

# Update expectations from the committed inputs, then review the diff.
golden:
	DLEPLUGIN_UPDATE_GOLDEN=1 go test -count=1 ./internal/plugin -run 'TestGoldenPlugins|TestGoldenErrors|TestInitFixtures'
	DLEPLUGIN_UPDATE_GOLDEN=1 go test -count=1 ./internal/build -run TestXMLGolden
	DLEPLUGIN_UPDATE_GOLDEN=1 go test -count=1 ./internal/convert -run TestGolden

clean:
	rm -rf bin
