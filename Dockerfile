# syntax=docker/dockerfile:1.20-labs@sha256:dbcde2ebc4abc8bb5c3c499b9c9a6876842bf5da243951cd2697f921a7aeb6a9
FROM debian:trixie-slim@sha256:a347fd7510ee31a84387619a492ad6c8eb0af2f2682b916ff3e643eb076f925a AS base
ARG PROJECT_NAME=homeassistant-barcode-scanner
RUN apt-get update && apt-get install -y --no-install-recommends \
        ca-certificates \
        libusb-1.0-0 \
        libhidapi-libusb0 \
        libhidapi-hidraw0 \
    && rm -rf /var/lib/apt/lists/*
ENTRYPOINT ["/usr/local/bin/homeassistant-barcode-scanner"]

FROM golang:1.24.7@sha256:87916acb3242b6259a26deaa7953bdc6a3a6762a28d340e4f1448e7b5c27c009 AS build
ARG PROJECT_NAME=homeassistant-barcode-scanner
RUN apt-get update && apt-get install -y --no-install-recommends \
        gcc \
        libc6-dev \
        libusb-1.0-0-dev \
        libhidapi-dev \
        linux-libc-dev \
    && rm -rf /var/lib/apt/lists/*

COPY / /src
WORKDIR /src
ENV CGO_ENABLED=1
RUN go build -o bin/${PROJECT_NAME} main.go

FROM base AS goreleaser
ARG PROJECT_NAME=homeassistant-barcode-scanner
COPY ${PROJECT_NAME} /usr/local/bin/${PROJECT_NAME}

FROM base
ARG PROJECT_NAME=homeassistant-barcode-scanner
COPY --from=build /src/bin/${PROJECT_NAME} /usr/local/bin/${PROJECT_NAME}
