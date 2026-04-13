IMAGE_REPO ?= engram9
IMAGE_TAG  ?= latest

.PHONY: build test docker-build

build:
	go build -o engram9 ./cmd/engram9

test:
	go test ./...

docker-build:
	docker build -t $(IMAGE_REPO):$(IMAGE_TAG) .
