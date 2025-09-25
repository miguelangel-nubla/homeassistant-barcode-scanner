# Home Assistant Barcode Scanner Bridge

A lightweight Go application that bridges USB HID barcode scanners to Home Assistant via MQTT. Automatically discovers and manages multiple barcode scanners, publishing scanned barcodes as sensor entities in Home Assistant through MQTT discovery.

## Features

- **Multi-Scanner Support**: Configure and manage multiple barcode scanners simultaneously
- **USB HID Integration**: Direct USB HID communication with barcode scanners
- **International Keyboard Support**: Support for different keyboard layouts (US, Spanish, etc.)
- **Home Assistant Discovery**: Automatic sensor entity creation via MQTT discovery
- **Flexible Configuration**: YAML-based configuration with scanner identification by VID/PID
- **Automatic Reconnection**: Handles device disconnections and MQTT broker reconnections
- **Cross-Platform**: Supports Linux, Windows, and macOS
- **Docker Support**: Ready-to-use Docker images available
- **Device Monitoring**: Real-time scanner connection status and health monitoring

## Quick Start

### 1. Download and Install

Download the latest release for your platform from the [releases page](https://github.com/miguelangel-nubla/homeassistant-barcode-scanner/releases).

### 2. Identify Your Scanner

List available HID devices to find your barcode scanner:

```bash
homeassistant-barcode-scanner --list-devices
```

This will show available devices with their Vendor ID (VID) and Product ID (PID).

### 3. Create Configuration

Create a `config.yaml` file:

```yaml
# MQTT broker configuration
mqtt:
  broker_url: "mqtt://homeassistant.local:1883"
  username: "mqtt_user"
  password: "mqtt_password"

# Scanner configuration
scanners:
  warehouse_scanner:
    name: "Warehouse Scanner"
    identification:
      vendor_id: 0x60e     # From --list-devices output
      product_id: 0x16c7   # From --list-devices output
    keyboard_layout: "us"  # Keyboard layout: "us", "es", etc.
    termination_char: "enter"

# Home Assistant integration
homeassistant:
  discovery_prefix: "homeassistant"
  instance_id: "workstation"  # Optional, uses hostname if not set

# Logging
logging:
  level: "info"
  format: "text"
```

### 4. Run the Application

```bash
homeassistant-barcode-scanner --config config.yaml
```

The scanner will appear as a sensor entity in Home Assistant with the name "Warehouse Scanner".

## Configuration

### MQTT Settings

```yaml
mqtt:
  broker_url: "mqtt://homeassistant.local:1883"  # Required: MQTT broker URL
  username: "mqtt_user"                          # Optional: MQTT username
  password: "mqtt_password"                      # Optional: MQTT password
```

**Supported MQTT protocols:**
- `mqtt://` - Standard MQTT
- `mqtts://` - MQTT over SSL/TLS
- `ws://` - MQTT over WebSocket
- `wss://` - MQTT over Secure WebSocket

### Scanner Configuration

Configure multiple scanners using map syntax:

```yaml
scanners:
  # Scanner ID (used in MQTT topics and Home Assistant entity IDs)
  office_scanner:
    name: "Office Barcode Scanner"          # Optional friendly name
    identification:
      vendor_id: 0x60e                     # Required: USB Vendor ID
      product_id: 0x16c7                   # Required: USB Product ID
      serial: "ABC123"                     # Optional: For multiple identical devices
    keyboard_layout: "us"                  # Optional: Keyboard layout ("us", "es", etc.)
    termination_char: "enter"              # "enter", "tab", or "none"

  checkout_scanner_1:
    name: "Checkout #1"
    identification:
      vendor_id: 0x60e
      product_id: 0x16c7
      serial: "DEF456"                     # Required when multiple devices have same VID/PID
    keyboard_layout: "es"                  # Spanish keyboard layout example
    termination_char: "enter"
```

### Keyboard Layout Support

The application supports different keyboard layouts for proper character mapping from HID scancodes:

```yaml
scanners:
  scanner_id:
    keyboard_layout: "us"    # Default US QWERTY layout
    # or
    keyboard_layout: "es"    # Spanish QWERTY layout
```

**Available layouts:**
- `us` - US QWERTY (default)
- `es` - Spanish QWERTY

If no layout is specified, it defaults to US layout.

### Home Assistant Integration

```yaml
homeassistant:
  discovery_prefix: "homeassistant"        # MQTT discovery prefix (default: "homeassistant")
  instance_id: "workstation"               # Optional: Unique instance identifier
```

## Installation Methods

### Binary Installation

1. Download the appropriate binary for your platform from [releases](https://github.com/miguelangel-nubla/homeassistant-barcode-scanner/releases)
2. Extract and place in your desired location
3. Make executable (Linux/macOS): `chmod +x homeassistant-barcode-scanner`
4. Run with your configuration file

### Docker

Pull and run the Docker image:

```bash
docker run -d \
  --name ha-barcode-scanner \
  --device /dev/bus/usb \
  -v ./config.yaml:/config.yaml \
  ghcr.io/miguelangel-nubla/homeassistant-barcode-scanner:latest
```

### Docker Compose

```yaml
services:
  barcode-scanner:
    image: ghcr.io/miguelangel-nubla/homeassistant-barcode-scanner:latest
    container_name: ha-barcode-scanner
    devices:
      - /dev/bus/usb:/dev/bus/usb
    volumes:
      - ./config.yaml:/config.yaml
    restart: unless-stopped
```

### Building from Source

Requirements:
- Go 1.24.0 or later
- USB HID development libraries

```bash
git clone https://github.com/miguelangel-nubla/homeassistant-barcode-scanner.git
cd homeassistant-barcode-scanner
go build -o homeassistant-barcode-scanner
```

## Usage

### Command Line Options

```bash
homeassistant-barcode-scanner [OPTIONS]

OPTIONS:
  --config, -c FILE    Load configuration from FILE (default: config.yaml)
  --list-devices       List available HID devices for configuration
  --log-level LEVEL    Set log level: debug, info, warn, error (default: info)
  --help, -h          Show help
  --version, -v       Show version
```

### Device Permissions (Linux)

USB HID devices may require special permissions. Create a udev rule:

```bash
# Create udev rule file
sudo nano /etc/udev/rules.d/99-barcode-scanner.rules

# Add rule (replace VID/PID with your scanner's values)
SUBSYSTEM=="hidraw", ATTRS{idVendor}=="060e", ATTRS{idProduct}=="16c7", MODE="0666", GROUP="plugdev"

# Reload udev rules
sudo udevadm control --reload-rules
sudo udevadm trigger
```

## Home Assistant Integration

### Automatic Discovery

The application automatically creates Home Assistant sensor entities via MQTT discovery:

- **Entity ID**: `sensor.{instance_id}_{scanner_id}`
- **State**: Last scanned barcode value
- **Attributes**: Scanner ID, keyboard layout, termination character, device info

## Troubleshooting

### Scanner Not Detected

1. Run `--list-devices` to verify the scanner is visible
2. Check USB permissions (Linux udev rules)
3. Verify VID/PID values in configuration
4. Try different USB ports or cables

### MQTT Connection Issues

1. Verify broker URL and credentials
2. Check network connectivity to MQTT broker
3. Review MQTT broker logs for authentication errors
4. Test with a simple MQTT client

### Home Assistant Discovery

1. Ensure MQTT integration is configured in Home Assistant
2. Verify `discovery_prefix` matches Home Assistant configuration
3. Check MQTT broker logs for discovery messages
4. Restart Home Assistant if entities don't appear

### Debug Logging

Enable debug logging for detailed troubleshooting:

```bash
homeassistant-barcode-scanner --log-level debug
```

## Development

### Requirements

- Go 1.24.0+
- golangci-lint (for linting)
- Docker (for containerized builds)

### Building

```bash
# Local development build
go build

# Release build with GoReleaser
goreleaser --clean --snapshot --skip sign
```

### Testing

```bash
# Run tests
go test ./...

# Run with coverage
go test -cover ./...

# Lint code
golangci-lint run
```

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests if applicable
5. Run tests and linting
6. Submit a pull request

## Support

- 🐛 **Bug Reports**: [GitHub Issues](https://github.com/miguelangel-nubla/homeassistant-barcode-scanner/issues)
- 💡 **Feature Requests**: [GitHub Discussions](https://github.com/miguelangel-nubla/homeassistant-barcode-scanner/discussions)
- 📖 **Documentation**: [Project Wiki](https://github.com/miguelangel-nubla/homeassistant-barcode-scanner/wiki)
