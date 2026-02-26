# Docker Configuration Reference

This document describes the minimal viable Docker configuration for the Home Assistant barcode scanner integration.

## Overview

The application requires USB HID device access for barcode scanner communication.
Due to the low-level nature of USB HID operations via the [karalabe/hid](https://github.com/karalabe/hid) Go library, privileged container mode is **mandatory**.

## Minimal Docker Compose Configuration

**File:** `compose.yaml`

```yaml
services:
  barcode-scanner:
    build: .
    container_name: ha-barcode-scanner
    privileged: true
    volumes:
      - ./config.yaml:/config.yaml
    restart: unless-stopped
```

### Required Settings

- **`privileged: true`** - **MANDATORY** for USB HID device access via karalabe/hid library
- **`volumes`** - Mount configuration file for scanner and MQTT settings
- **`restart: unless-stopped`** - Ensure service persistence

### Why Privileged Mode is Required

The karalabe/hid library requires:

1. Direct access to USB HID device nodes (`/dev/hidraw*`)
2. USB device enumeration capabilities
3. Low-level hardware abstraction layer access
4. Kernel driver interface access

These requirements cannot be satisfied with:

- Specific device mounts (`devices:`)
- Individual capabilities (`cap_add:`)
- Filesystem volume mounts (`/dev:/dev`, `/sys/*:/sys/*`)

## Docker Image Configuration

**File:** `Dockerfile`

```dockerfile
# syntax=docker/dockerfile:1.18-labs@sha256:79cdc14e1c220efb546ad14a8ebc816e3277cd72d27195ced5bebdd226dd1025
FROM debian:trixie-slim@sha256:c2880112cc5c61e1200c26f106e4123627b49726375eb5846313da9cca117337 AS base
ARG PROJECT_NAME=homeassistant-barcode-scanner
RUN apt-get update && apt-get install -y --no-install-recommends \
        ca-certificates \
        libusb-1.0-0 \
        libhidapi-libusb0 \
        libhidapi-hidraw0 \
    && rm -rf /var/lib/apt/lists/*
ENTRYPOINT ["/usr/local/bin/homeassistant-barcode-scanner"]

FROM golang:1.26.0@sha256:c83e68f3ebb6943a2904fa66348867d108119890a2c6a2e6f07b38d0eb6c25c5 AS build
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
```

### Key Dockerfile Features

1. **Multi-stage build** - Optimizes final image size
2. **Debian Trixie base** - Ensures glibc compatibility with Go 1.26.0
3. **Pinned SHA256 hashes** - Reproducible builds and security
4. **CGO enabled** - Required for HID library compilation
5. **GoReleaser target** - Supports external binary injection

### Runtime Dependencies

- **libusb-1.0-0** - USB device access library
- **libhidapi-libusb0** - HID API libusb backend
- **libhidapi-hidraw0** - HID API raw backend
- **ca-certificates** - TLS/SSL certificate validation

### Build Dependencies

- **gcc** - C compiler for CGO
- **libc6-dev** - Standard C library development files
- **libusb-1.0-0-dev** - USB development headers
- **libhidapi-dev** - HID API development headers
- **linux-libc-dev** - Linux kernel headers

## Security Considerations

**WARNING:** Privileged containers have elevated security risks:

- Full access to host devices and kernel interfaces
- Potential for container escape
- Access to sensitive system resources

### Mitigation Strategies

1. **Network isolation** - Run on isolated network segments
2. **Resource limits** - Apply CPU/memory constraints
3. **Read-only filesystem** - Mount application code as read-only
4. **Non-root user in container** - Use dedicated user (though privileged mode overrides many protections)
5. **Regular updates** - Keep base images and dependencies current

## Testing Configuration

The minimal configuration has been verified to:

- ✅ Successfully detect USB HID barcode scanners
- ✅ Establish device connections
- ✅ Read barcode scan data
- ✅ Publish to Home Assistant via MQTT discovery

Alternative configurations tested and **failed**:

- ❌ `cap_add: ALL` without privileged mode
- ❌ Specific device mounts (`/dev/hidraw*`)
- ❌ Filesystem volume mounts (`/dev:/dev`, `/sys/*`)
- ❌ Individual capabilities (`SYS_RAWIO`, `DAC_OVERRIDE`)

## Deployment Commands

```bash
# Build and run with docker-compose
docker compose up --build -d

# Check scanner detection
docker compose logs | grep -i "scanner.*detected\|device.*connected"

# Stop service
docker compose down
```
