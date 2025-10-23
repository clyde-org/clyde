#!/bin/bash
export DOCKER_BUILDKIT=1
# Check if tag and architecture are provided
if [ -z "$1" ] || [ -z "$2" ]; then
  echo "Usage: $0 <tag> <architecture>"
  echo "Architecture options: amd64 or arm64"
  exit 1
fi

TAG=$1
ARCH=$2
REPO="cmc.centralrepo.rnd.huawei.com/clyde"



# Push Docker image
echo "Pushing ${IMAGE_NAME}..."
docker push "${IMAGE_NAME}"

  # Clean up Dockerfile
  #rm "${DOCKERFILE_DIR}/Dockerfile.${SIZE}gb"
done

echo "All images built and pushed successfully!"
