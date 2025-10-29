# Build Instructions

This documentation details the steps for building the project, creating and pushing a relevant container image to a remote repository, and the installation process on a Kubernetes cluster.

## Build the Project

For building the project, please execute the following command:

```
make build
```

This will produce the `dist` directory under the project's root directory containing the Go executables and dependencies necessary to run the application on top of ARM 64-bit systems. The following is a SAMPLE output that shows the outputs targetting different architectures are built:

```
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

## Build the Project's Container Image

The Makefile was updated with two separate rules for building Clyde container images targetting `amd64` and `arm64` architectures, passing the correct environment arguments to the Docker build system. The following sections contain instructions on how to build the container images.

### Build for ARM (64-bit) Architecture

```
sudo make build-image-arm64
```

The output should look similar to this:

```
docker build --build-arg TARGETOS=linux --build-arg TARGETARCH=arm64 -t clyde:$(git rev-parse --short HEAD) .
DEPRECATED: The legacy builder is deprecated and will be removed in a future release.
            Install the buildx component to build images with BuildKit:
            https://docs.docker.com/go/buildx/

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

### Build for AMD (64-bit) Architecture

```
sudo make build-image-amd64
```

## Cleanup

Execute the following command to remove the `dist` directory.

```
sudo make clean
```

## Tag Clyde's Image and Pushing it to a Remote Repository

Clyde's image needs to be tagged first, and then pushed to a remote image repository by executing the following commands:

```
docker tag clyde:34ebf91 <REGISTRY-ADDRESS>/clyde/clyde:v12.0
docker push <REGISTRY-ADDRESS>/clyde/clyde:v12.0
```


