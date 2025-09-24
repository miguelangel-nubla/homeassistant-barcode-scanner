# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

This is a Home Assistant barcode scanner integration that bridges USB HID barcode scanners to Home Assistant via MQTT. The application runs as a standalone service, automatically discovers and manages multiple barcode scanners, and publishes scanned barcodes to Home Assistant through MQTT discovery.

## Development Commands

### Building
```bash
# Build for local development
go build -o homeassistant-barcode-scanner

# Build release binaries with GoReleaser
goreleaser --clean --snapshot --skip sign

# Build Docker image
docker build -t homeassistant-barcode-scanner .
```

### Testing
```bash
# Run all tests
go test ./...

# Run tests with timeout
go test -timeout 60s ./...

# Download dependencies
go mod download
```

### Linting
```bash
# Run golangci-lint (requires golangci-lint to be installed)
golangci-lint run

# Lint YAML files
pip install yamllint
yamllint .
```

### Running
```bash
# Run with default config
./homeassistant-barcode-scanner

# Run with custom config
./homeassistant-barcode-scanner --config /path/to/config.yaml

# List available HID devices for configuration
./homeassistant-barcode-scanner --list-devices

# Run with debug logging
./homeassistant-barcode-scanner --log-level debug
```

### Documentation
```bash
# Serve docs locally
make docs-serve

# Build docs
make docs-build
```

## Architecture

The application follows a layered service architecture:

### Core Components

- **CLI Layer** (`pkg/cli/`): Command-line interface handling, argument parsing, and application lifecycle
- **Application Layer** (`pkg/app/`): Main application orchestration, service management, and event handling
- **Configuration** (`pkg/config/`): YAML-based configuration management with validation
- **Scanner Management** (`pkg/scanner/`): HID device discovery, connection management, and barcode reading
- **MQTT Client** (`pkg/mqtt/`): MQTT broker communication with automatic reconnection
- **Home Assistant Integration** (`pkg/homeassistant/`): MQTT discovery message generation and device registration

### Service Architecture

The application uses a `ServiceManager` pattern that coordinates multiple independent services:

1. **MQTT Client**: Maintains broker connection with automatic reconnection and last will testament
2. **Scanner Manager**: Manages multiple USB HID barcode scanners with device monitoring
3. **Home Assistant Integration**: Publishes device discovery messages and forwards barcode events

### Event Flow

1. Scanner Manager detects barcode scan from USB HID device
2. Event is published through internal event system
3. Home Assistant Integration receives event and publishes to MQTT
4. Home Assistant automatically discovers and creates sensor entities

### Configuration Structure

The application supports multiple scanner configurations through YAML:

```yaml
scanners:
  scanner_id:  # Map key becomes scanner ID
    name: "Friendly Name"
    identification:
      vendor_id: 0x60e    # USB VID (required)
      product_id: 0x16c7  # USB PID (required)
      serial: "ABC123"    # Optional for multiple identical devices
    termination_char: "enter"  # Scan termination detection
```

### Key Design Patterns

- **Service Management**: All major components implement a common service interface for lifecycle management
- **Event-Driven Architecture**: Scanner events flow through handlers to appropriate services
- **Configuration Mapping**: Scanner IDs come from YAML map keys, not device properties
- **Graceful Shutdown**: Signal handling ensures clean service shutdown
- **Multi-Device Support**: Single instance can manage multiple scanners simultaneously

## Important Implementation Notes

- Scanner IDs are derived from YAML configuration map keys, not hardware serials
- MQTT topics include instance ID to support multiple application instances
- Device reconnection is handled automatically with configurable retry delays
- Home Assistant discovery messages are published on startup and device connection
- The application uses Logrus for structured logging with configurable levels and formats
- USB HID access requires appropriate permissions (udev rules on Linux)

## Testing Approach

The project includes unit tests for core functionality. Run tests before committing changes. The CI pipeline runs both Go tests and YAML linting.
