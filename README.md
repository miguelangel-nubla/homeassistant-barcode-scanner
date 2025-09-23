# Home Assistant Barcode Scanner Client

A Go application that reads USB barcode scanner input (HID) and publishes the scanned values to Home Assistant via MQTT with auto-discovery support.

## Features

- **USB HID Barcode Scanner Support**: Automatically detects and reads from USB barcode scanners
- **Home Assistant Integration**: Full MQTT auto-discovery with device registration
- **Multiple MQTT Protocols**: Support for TCP, SSL/TLS, WebSocket, and Secure WebSocket connections
- **Auto-Reconnection**: Robust MQTT connection handling with automatic reconnection
- **Availability Tracking**: Birth/will messages and availability status for Home Assistant
- **Device Auto-Detection**: Automatically finds barcode scanner devices or use specific device paths
- **Configurable Logging**: Structured logging with configurable levels and formats

## Installation

### From Source

```bash
git clone https://github.com/miguelangel-nubla/homeassistant-barcode-scanner.git
cd homeassistant-barcode-scanner
go build -o homeassistant-barcode-scanner
```

### Using Go Install

```bash
go install github.com/miguelangel-nubla/homeassistant-barcode-scanner@latest
```

## Configuration

Create a configuration file `config.yaml` based on the example:

```bash
cp config.example.yaml config.yaml
```

### Required Configuration

The minimum required configuration:

```yaml
mqtt:
  broker_url: "mqtt://your-mqtt-broker.local:1883"

homeassistant:
  entity_id: "barcode_scanner"
```

### Complete Configuration Options

See `config.example.yaml` for all available configuration options including:

- MQTT broker settings (broker URL, authentication)
- Scanner device configuration (vendor/product ID, device path)
- Home Assistant integration settings
- Logging configuration

## Usage

### Basic Usage

```bash
./homeassistant-barcode-scanner -c config.yaml
```

### List Available Devices

To see all HID devices that might be barcode scanners:

```bash
./homeassistant-barcode-scanner --list-devices
```

### Command Line Options

```bash
./homeassistant-barcode-scanner --help
```

- `-c, --config FILE`: Specify configuration file (default: config.yaml)
- `--list-devices`: List available HID devices that might be barcode scanners
- `--log-level LEVEL`: Set log level (debug, info, warn, error)
- `--version`: Show version information

## MQTT Protocols

The application supports multiple MQTT connection protocols:

- **mqtt://**: Standard MQTT over TCP (default, typically port 1883)
- **mqtts://**: MQTT over SSL/TLS (typically port 8883)
- **ws://**: MQTT over WebSocket (typically port 1883)
- **wss://**: MQTT over Secure WebSocket (typically port 8883)

## Home Assistant Integration

The application automatically registers with Home Assistant using MQTT auto-discovery:

1. **Device Registration**: Creates a device in Home Assistant with model and version info
2. **Sensor Entity**: Creates a sensor entity for barcode values
3. **Availability Tracking**: Reports online/offline status
4. **Auto-Expiration**: Sensor values expire after 5 minutes to prevent stale data

### Entity Details

- **Entity ID**: Configurable via `homeassistant.entity_id`
- **Device Class**: Barcode scanner with appropriate icon
- **State**: JSON payload with barcode value and timestamp
- **Availability**: Online/offline status with birth/will messages

## Barcode Scanner Compatibility

The application works with USB HID barcode scanners that emulate keyboard input. It supports:

- Standard keyboard emulation scanners
- Most commercial barcode scanner brands (Honeywell, Symbol, Datalogic, Zebra, etc.)
- Auto-detection based on device characteristics
- Manual device specification via vendor/product ID or device path

## Troubleshooting

### Scanner Not Detected

1. Run with `--list-devices` to see available devices
2. Check if your scanner appears in the list
3. If found, note the vendor/product ID and add to config
4. Ensure proper USB permissions (may require udev rules on Linux)

### MQTT Connection Issues

1. Verify MQTT broker URL and protocol
2. Check authentication credentials
3. Ensure network connectivity
4. Review logs with `--log-level debug`

### Permission Issues (Linux)

Add udev rules for HID device access:

```bash
# Create udev rule file
sudo tee /etc/udev/rules.d/99-hid-barcode.rules > /dev/null << 'EOF'
# Allow access to HID devices for barcode scanners
SUBSYSTEM=="hidraw", MODE="0666"
KERNEL=="hidraw*", MODE="0666"
EOF

# Reload udev rules
sudo udevadm control --reload-rules
sudo udevadm trigger
```

## Development

### Building

```bash
make build
```

### Testing

```bash
make test
```

### Linting

```bash
make lint
```

## License

[License information would go here]