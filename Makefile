BIN     := bin/balloon
PKG     := ./cmd/balloon
GOFILES := $(shell find . -name '*.go' -not -path './web/*')
WEBFILES := $(shell find web -type f)
WINDOWS_ARCH ?= amd64
WINDOWS_BIN := bin/balloon-windows-$(WINDOWS_ARCH).exe
WINDOWS_CLI_BIN := bin/balloon-cli-windows-$(WINDOWS_ARCH).exe

.PHONY: all build windows windows-cli windows-resources test cover vet fmt demo serve clean

all: fmt vet test build

build: $(BIN)

$(BIN): $(GOFILES) $(WEBFILES) go.mod go.sum
	go build -o $(BIN) $(PKG)

# A native launcher without a console, for double-clicking on Windows.
windows: windows-resources
	GOOS=windows GOARCH=$(WINDOWS_ARCH) CGO_ENABLED=0 go build -trimpath -ldflags="-H windowsgui -s -w" -o $(WINDOWS_BIN) ./cmd/balloon-desktop

# Preserve the console commands as a separate optional executable.
windows-cli: windows-resources
	GOOS=windows GOARCH=$(WINDOWS_ARCH) CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o $(WINDOWS_CLI_BIN) $(PKG)

# Generate icon resources before Windows builds; compiled objects stay out of Git.
# After editing the SVG, first render its PNG with rsvg-convert (see README).
windows-resources:
	go run github.com/tc-hib/go-winres@v0.3.3 make --in cmd/balloon-desktop/resources/winres.json --arch amd64,arm64 --out cmd/balloon-desktop/rsrc
	go run github.com/tc-hib/go-winres@v0.3.3 make --in cmd/balloon-desktop/resources/winres.json --arch amd64,arm64 --out cmd/balloon/rsrc

test:
	go test ./...

cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -1

vet:
	go vet ./...

fmt:
	gofmt -l -w ./cmd ./internal

# Regenerates the picture in the README.
demo: $(BIN)
	./$(BIN) demo -o docs/demo.svg

serve: $(BIN)
	./$(BIN) serve

clean:
	rm -rf bin coverage.out
	rm -f cmd/balloon/rsrc_windows_*.syso cmd/balloon-desktop/rsrc_windows_*.syso
