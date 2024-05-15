FROM gcr.io/distroless/static-debian12:nonroot
ARG TARGETOS
ARG TARGETARCH

COPY .work/bin/crik-${TARGETOS}-${TARGETARCH} /usr/local/bin/crik
USER 65532

ENTRYPOINT ["crik"]
