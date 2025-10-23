#cmc does not support multi-arch targing/manifest if it does like gcr or docker we can use the same tag and docker will pull the right image.
# FROM cmc.centralrepo.rnd.huawei.com/clyde/distroless/static:nonroot-amd
# For amd if on amd machine first pull the using docker pull linux/amd64 gcr.io/distroless/static:nonroot 
# or docker pull --platform linux/amd64 gcr.io/distroless/static:nonroot if you are on a non amd machine then tag and push to your repo.
#FROM cmc.centralrepo.rnd.huawei.com/clyde/distroless/static:nonroot
FROM gcr.io/distroless/static:nonroot
ARG TARGETOS
ARG TARGETARCH
COPY ./dist/clyde_${TARGETOS}_${TARGETARCH}/clyde /
USER root:root
ENTRYPOINT ["/clyde"]