FROM golang:1.22 as build
ARG TARGETOS
ARG TARGETARCH

WORKDIR /build

COPY go.mod go.mod
COPY go.sum go.sum
RUN go mod download

COPY cmd cmd
COPY internal internal

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -o crik cmd/crik/main.go && \
    chmod +x crik


FROM ubuntu:22.04

RUN apt-get update && apt-get install --no-install-recommends --yes gnupg curl ca-certificates

RUN curl "https://keyserver.ubuntu.com/pks/lookup?op=get&search=0x4E2A48715C45AEEC077B48169B29EEC9246B6CE2" | gpg --dearmor > /usr/share/keyrings/criu-ppa.gpg \
    && echo "deb [signed-by=/usr/share/keyrings/criu-ppa.gpg] https://ppa.launchpadcontent.net/criu/ppa/ubuntu jammy main" > /etc/apt/sources.list.d/criu.list \
    && apt-get update \
    && apt-get install --no-install-recommends --yes criu iptables

# The PR https://github.com/checkpoint-restore/criu/pull/2360 is not merged yet, so we use criu from the docker image
# built from the PR. This is necessary if you get sched policy error during restore, which is the case with webkit-based
# browsers.

#RUN apt-get update \
#    && apt install --no-install-recommends --yes libprotobuf-dev libprotobuf-c-dev protobuf-c-compiler protobuf-compiler python3-protobuf iptables nftables iproute2 libnftables-dev libcap-dev libnl-3-dev libnet-dev libaio-dev
#COPY --from=docker.io/muvaf/criu-x86_64:rst0git-6673a3b /criu/criu/criu /usr/sbin/criu

COPY --from=build /build/crik /usr/local/bin/crik

ENTRYPOINT ["crik", "run", "--"]
