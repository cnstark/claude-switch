IMAGE ?= claude-switch:latest
PLATFORMS ?= linux/amd64,linux/arm64

.PHONY: docker-build-single
docker-build-single:
	docker build -t $(IMAGE) .

.PHONY: docker-build
docker-build:
	docker buildx build --platform $(PLATFORMS) -t $(IMAGE) --load .

.PHONY: docker-push
docker-push:
	docker buildx build --platform $(PLATFORMS) -t $(IMAGE) --push .