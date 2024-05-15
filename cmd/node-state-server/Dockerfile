FROM gcr.io/distroless/static-debian12:nonroot
ARG TARGETOS
ARG TARGETARCH

COPY .work/bin/node-state-server-${TARGETOS}-${TARGETARCH} /usr/local/bin/node-state-server
USER 65532

ENTRYPOINT ["node-state-server"]
