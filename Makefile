# Makefile

TAG = $$(git rev-parse --short HEAD)
IMG_NAME ?= clyde
IMG_REF = $(IMG_NAME):$(TAG)

# Build rules

## Build using goreleaser
build:
	goreleaser build --snapshot --clean --skip before

## Build a container image for AMD64
build-image-amd64: build
	docker build --platform linux/amd64 --build-arg TARGETOS=linux --build-arg TARGETARCH=amd64 -t ${IMG_REF} .

## Build a container image for ARM64
build-image-arm64: build
	docker build --platform linux/arm64 --build-arg TARGETOS=linux --build-arg TARGETARCH=arm64 -t ${IMG_REF} .

## Build a multiarch container image
build-image-multiarch: build
	docker buildx build --platform linux/amd64,linux/arm64 -t ${IMG_REF} .

# Test and clean rules

## Run unit tests
tests:
	go test ./...

## Clean build artefacts
clean:
	rm -rf dist

test-e2e: build-image
	IMG_REF=${IMG_REF} \
	E2E_PROXY_MODE=${E2E_PROXY_MODE} \
	E2E_IP_FAMILY=${E2E_IP_FAMILY} \
	go test ./test/e2e -v -timeout 200s -tags e2e -count 1 -run TestE2E

dev-deploy: build-image
	IMG_REF=${IMG_REF} go test ./test/e2e -v -timeout 200s -tags e2e -count 1 -run TestDevDeploy

tools:
	GO111MODULE=on go install github.com/norwoodj/helm-docs/cmd/helm-docs

helm-docs: tools
	cd ./charts/clyde && helm-docs