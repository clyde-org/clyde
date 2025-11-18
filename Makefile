TAG = $$(git rev-parse --short HEAD)
IMG_NAME ?= clyde
IMG_REF = $(IMG_NAME):$(TAG)

E2E_PROXY_MODE ?= iptables
E2E_IP_FAMILY ?= ipv4

build:
	goreleaser build --snapshot --clean --skip before

# Multi-arch build using Docker Buildx
build-image: build
	docker buildx create --use --name clyde-builder || true
	docker buildx build \
		--platform linux/amd64,linux/arm64 \
		--build-arg TARGETOS=linux \
		--build-arg TARGETARCH=amd64 \
		-t ${IMG_REF} .

# Optional separate builds (for local testing)
build-image-amd64: build
	docker build --platform linux/amd64 --build-arg TARGETOS=linux --build-arg TARGETARCH=amd64 -t ${IMG_REF} .

build-image-arm64: build
	docker build --platform linux/arm64 --build-arg TARGETOS=linux --build-arg TARGETARCH=arm64 -t ${IMG_REF} .

test-unit:
	go test ./...

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

clean:
	rm -rf dist
