# syntax=docker/dockerfile:1.23-labs@sha256:7eca9451d94f9b8ad22e44988b92d595d3e4d65163794237949a8c3413fbed5d
FROM debian:trixie-slim@sha256:cedb1ef40439206b673ee8b33a46a03a0c9fa90bf3732f54704f99cb061d2c5a AS base
ARG PROJECT_NAME=homeassistant-barcode-scanner
RUN apt-get update && apt-get install -y --no-install-recommends \
        ca-certificates \
        libusb-1.0-0 \
        libhidapi-libusb0 \
        libhidapi-hidraw0 \
    && rm -rf /var/lib/apt/lists/*
ENTRYPOINT ["/usr/local/bin/homeassistant-barcode-scanner"]

FROM golang:1.27.1@sha256:3680233e3204827fbdc66088528ae6d4b3d034f51d03a99d454f6de034888244 AS build
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
