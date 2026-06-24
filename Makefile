IMAGE ?= claude-switch:latest
PLATFORMS ?= linux/amd64,linux/arm64

VERSION ?= dev
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build
build:
	go build -ldflags="$(LDFLAGS)" -o bin/cs ./cmd/cs
	go build -ldflags="$(LDFLAGS)" -o bin/cs-proxy ./cmd/cs-proxy

.PHONY: build-all
build-all:
	GOOS=linux   GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o bin/cs-linux-amd64   ./cmd/cs
	GOOS=linux   GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o bin/cs-proxy-linux-amd64 ./cmd/cs-proxy
	GOOS=linux   GOARCH=arm64 go build -ldflags="$(LDFLAGS)" -o bin/cs-linux-arm64   ./cmd/cs
	GOOS=linux   GOARCH=arm64 go build -ldflags="$(LDFLAGS)" -o bin/cs-proxy-linux-arm64 ./cmd/cs-proxy
	GOOS=windows GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o bin/cs-windows-amd64.exe   ./cmd/cs
	GOOS=windows GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o bin/cs-proxy-windows-amd64.exe ./cmd/cs-proxy
	GOOS=darwin  GOARCH=arm64 go build -ldflags="$(LDFLAGS)" -o bin/cs-darwin-arm64  ./cmd/cs
	GOOS=darwin  GOARCH=arm64 go build -ldflags="$(LDFLAGS)" -o bin/cs-proxy-darwin-arm64 ./cmd/cs-proxy

.PHONY: test
test:
	go test ./...

.PHONY: clean
clean:
	rm -rf bin/

.PHONY: docker-build-single
docker-build-single:
	docker build -t $(IMAGE) .

.PHONY: docker-build
docker-build:
	docker buildx build --platform $(PLATFORMS) -t $(IMAGE) --push .

.PHONY: docker-push
docker-push:
	docker buildx build --platform $(PLATFORMS) -t $(IMAGE) --push .
