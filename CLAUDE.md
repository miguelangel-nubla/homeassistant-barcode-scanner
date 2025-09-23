# Claude Development Notes

This file contains development-specific information for working with this Home Assistant Barcode Scanner project using Claude Code.

## Project Overview

This is a Go application that bridges USB HID barcode scanners with Home Assistant via MQTT auto-discovery. Built using the [ekristen/go-project-template](https://ekristen.github.io/go-project-template/) as a foundation.

## Development Commands

### Building and Testing
```bash
# Build the application
go build -o homeassistant-barcode-scanner

# Run tests (when implemented)
go test ./...

# Run linting
golangci-lint run

# Tidy dependencies
go mod tidy
```

### Documentation
```bash
# Build documentation
make docs-build

# Serve documentation locally
make docs-serve

# Seed docs from README
make docs-seed
```

## Architecture Notes

### Package Structure
- `pkg/config/` - YAML configuration management with validation
- `pkg/mqtt/` - MQTT client with auto-reconnection and TLS support
- `pkg/scanner/` - USB HID barcode scanner input handling
- `pkg/homeassistant/` - Home Assistant MQTT auto-discovery integration

### Key Design Decisions
- **Raw USB HID approach**: Chosen over input event handling for reliability in background service scenarios
- **Configuration-driven termination**: Supports scanners with different termination characters (enter, tab, none)
- **Simplified detection**: Enhanced to recognize Newtologic and other scanner manufacturers
- **Security focus**: TLS support with configurable certificate validation

## Configuration Management

### Required Environment
- Go 1.24+ (specified in go.mod)
- USB HID device access permissions (may require udev rules on Linux)
- MQTT broker with appropriate credentials

### Scanner Detection
The application uses multiple detection strategies:
1. **Auto-detection**: Based on manufacturer names and HID usage patterns
2. **Vendor/Product ID**: Direct specification for known devices
3. **Device path**: Direct path specification for maximum control

### MQTT Protocols Supported
- `mqtt://` - Standard MQTT over TCP
- `mqtts://` - MQTT over SSL/TLS
- `ws://` - MQTT over WebSocket
- `wss://` - MQTT over Secure WebSocket

## Development Workflow

### Adding New Scanner Support
1. Run `--list-devices` to identify device characteristics
2. Add manufacturer/product patterns to `isLikelyBarcodeScanner()`
3. Test auto-detection or provide configuration examples
4. Update documentation with compatibility information

### Configuration Changes
1. Update structs in `pkg/config/config.go`
2. Add validation in `validate()` method
3. Set defaults in `setDefaults()` method
4. Update `config.example.yaml` with documentation
5. Pass new config to relevant components

### MQTT Topics Structure
- Discovery: `{prefix}/sensor/{entity_id}/config`
- State: `{prefix}/sensor/{entity_id}/state`
- Availability: `{prefix}/sensor/{entity_id}/availability`

## Troubleshooting Development Issues

### USB Permission Issues (Linux)
```bash
# Create udev rules for HID access
sudo tee /etc/udev/rules.d/99-hid-barcode.rules > /dev/null << 'EOF'
SUBSYSTEM=="hidraw", MODE="0666"
KERNEL=="hidraw*", MODE="0666"
EOF

# Reload rules
sudo udevadm control --reload-rules
sudo udevadm trigger
```

### Common Development Patterns

#### Adding New Configuration Options
1. Add field to appropriate config struct
2. Set default value in `setDefaults()`
3. Add validation in `validate()` if needed
4. Pass to component via constructor
5. Update example configuration

#### Testing Scanner Devices
```bash
# List all devices with detailed info
sudo ./homeassistant-barcode-scanner --list-devices

# Test with specific device
# Update config.yaml with vendor_id/product_id or device_path
./homeassistant-barcode-scanner --log-level debug
```

## Build and Release

### Local Development
```bash
go run . --config config.yaml --log-level debug
```

### Dependencies
- `github.com/karalabe/hid` - USB HID device access
- `github.com/eclipse/paho.mqtt.golang` - MQTT client
- `github.com/sirupsen/logrus` - Structured logging
- `github.com/urfave/cli/v3` - CLI framework
- `gopkg.in/yaml.v3` - YAML configuration parsing

## Security Considerations

- Configuration files may contain MQTT credentials (excluded from git)
- TLS certificate validation configurable for development vs production
- USB device access requires elevated permissions on some systems
- MQTT will messages ensure Home Assistant knows about disconnections

## Future Enhancements

- [ ] Add support for serial-based barcode scanners
- [ ] Implement scanner configuration commands
- [ ] Add metrics/health check endpoints
- [ ] Support for multiple simultaneous scanners
- [ ] WebUI for configuration and monitoring