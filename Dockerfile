FROM gcr.io/distroless/static:nonroot
ARG TARGETOS
ARG TARGETARCH
COPY ./dist/clyde_${TARGETOS}_${TARGETARCH}/clyde /
USER root:root
ENTRYPOINT ["/clyde"]