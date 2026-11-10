# --match: skip the VS Code extension's ext-v* tags, which share this repo.
VERSION ?= $(shell git describe --tags --match 'v*' --always --dirty 2>/dev/null || echo dev)

.PHONY: build build-mac test vet ext clean

build:
	go build -ldflags "-X main.version=$(VERSION)" -o softgaze .

# Both Mac binaries, to copy over.
build-mac:
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags "-X main.version=$(VERSION)" -o dist/softgaze-darwin-arm64 .
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags "-X main.version=$(VERSION)" -o dist/softgaze-darwin-amd64 .

# The Go packages, then the window's renderers (Node).
test:
	go test -count=1 ./...
	node --test webtest/*.test.mjs

vet:
	go vet ./...

# The VS Code extension, as dist/softgaze.vsix.
ext:
	mkdir -p dist
	cd extension && npm run compile && npx --yes @vscode/vsce@3.9.2 package -o ../dist/softgaze.vsix

clean:
	rm -rf softgaze softgaze.exe dist
