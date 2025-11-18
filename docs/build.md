# Clyde Build & Deployment Guide

This guide describes how to build Clyde from source, create container images for multiple architectures, push images to a remote registry, and deploy Clyde on a Kubernetes cluster.

---

## 1. Prerequisites

* Go installed
* Docker installed (with `buildx` recommended for multi-arch builds)
* Make & GoReleaser installed
* Kubernetes cluster & Helm installed

Clone the code:

```bash
git clone https://github.com/fanbondi/clyde.git
cd clyde
```

## 2. Quick Build (All-in-One)

For a fast, standard build including binaries and images:

```bash
# Build binaries for all supported architectures
make build

# Build multi-arch container image
sudo make build-image

# Tag and push (replace with actual registry and version)
docker tag clyde:<commit-sha> <REGISTRY>/clyde:v15.0
docker push <REGISTRY>/clyde:v15.0
```

> This will produce multi-arch binaries under `dist/` and a single multi-architecture container image that supports both AMD64 and ARM64.

---

## 3. Detailed Build Process

### 3.1 Build Clyde Binaries

Run:

```bash
make build
```

This will produce the `dist` directory under the project's root directory containing the Go executables and dependencies necessary to run the application on top of ARM 64-bit systems. The following is a SAMPLE output that shows the outputs targetting different architectures are built:

```bash
make build
goreleaser build --snapshot --clean --skip before
  • skipping before and validate...
  • cleaning distribution directory
  • loading environment variables
  • getting and validating git state
    • ignoring errors because this is a snapshot     error=git doesn't contain any tags - either add a tag or use --snapshot
    • git state                                      commit=6a1d49a1748847e0b54aead8636d00bf2aae5ecf branch=helm current_tag=v0.0.0 previous_tag=<unknown> dirty=false
    • pipe skipped or partially skipped              reason=disabled during snapshot mode
  • parsing tag
  • setting defaults
  • snapshotting
    • building snapshot...                           version=0.0.0-SNAPSHOT-6a1d49a
  • ensuring distribution directory
  • setting up metadata
  • writing release metadata
  • loading go mod information
  • build prerequisites
  • building binaries
    • building                                       binary=dist/clyde_linux_arm64/clyde
    • building                                       binary=dist/clyde_linux_arm/clyde
    • building                                       binary=dist/clyde_linux_amd64/clyde
    • took: 1m37s
  • writing artifacts metadata
  • build succeeded after 1m36s
  • thanks for using GoReleaser!
```

Please note that some changes have been introduced to the Makefile such as removing the flag option `--single-target` from the `build` rule, when invoking the GoReleaser command to build the project artefacts, because this flag defaults to amd64 architecture. Removing this flag will allow GoReleaser to build for all the targets specified in the `.goreleaser.yml` file including both `amd64` and `arm64`.


### 3.2 Build Container Images

#### Multi-arch Build (Recommended)

```bash
sudo make build-image
```

* This uses Docker Buildx to create a single image that supports both AMD64 and ARM64.
* Add and optional `--push` to directly push to a remote registry.

#### Separate Builds for each architecture 

```bash
sudo make build-image-amd64
sudo make build-image-arm64
```

> Sample Docker build output for either architecture:

```bash
docker build --build-arg TARGETOS=linux --build-arg TARGETARCH=arm64 -t clyde:$(git rev-parse --short HEAD) .
Sending build context to Docker daemon  121.9MB
Step 1/6 : FROM gcr.io/distroless/static:nonroot
 ---> 55be039f1638
Step 2/6 : ARG TARGETOS
 ---> Using cache
 ---> c70afad92c1e
Step 3/6 : ARG TARGETARCH
 ---> Using cache
 ---> 2625a953ec6d
Step 4/6 : COPY ./dist/clyde_${TARGETOS}_${TARGETARCH}/clyde /
 ---> abb5d2aa8e65
Step 5/6 : USER root:root
 ---> Running in 60a841f23fce
 ---> Removed intermediate container 60a841f23fce
 ---> 13e5480577a7
Step 6/6 : ENTRYPOINT ["/clyde"]
 ---> Running in ad0d03ba3b3a
 ---> Removed intermediate container ad0d03ba3b3a
 ---> e9c604be7690
Successfully built e9c604be7690
Successfully tagged clyde:6a1d49a
```

---

### 3.3 Cleanup

Remove build artifacts:

```bash
sudo make clean
```

---

### 3.4 Tag & Push Images

Tag the image with your registry and version:

```bash
docker tag clyde:<commit-sha> <REGISTRY>/clyde:v15.0
docker push <REGISTRY>/clyde:v15.0
```

Replace `<commit-sha>`, `<REGISTRY>`, and version with your actual values.

---

## 4. Installation on Kubernetes

### 4.1 Deploy via Helm

```bash
helm upgrade --install clyde charts/clyde -f charts/clyde/clyde-values.yml
```

### 4.2 Verify Deployment

```bash
kubectl get pods -n clyde
```

![image](img/clyde_pods.png)

```bash 
kubectl get svc -n clyde
kubectl logs -l app=clyde -n clyde
```

* Check that Clyde is serving/caching images by observing logs or metrics.

### 4.3 Uninstall

```bash
helm delete clyde -n clyde --no-hooks
kubectl delete namespace clyde --ignore-not-found
```

---

### Notes

* Removing `--single-target` allows GoReleaser to build all architectures.
* Use Docker Buildx to build proper multi-arch images.
* Always verify image tags and commit SHA before pushing to registry.

