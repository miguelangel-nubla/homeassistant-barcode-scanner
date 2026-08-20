package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const minimalScannerYAML = `
scanners:
  test_scanner:
    name: "Test Scanner"
    identification:
      vendor_id: 0x60e
      product_id: 0x16c7
    termination_char: "enter"
`

func loadConfigFromString(t *testing.T, content string) (*Config, error) {
	t.Helper()

	tempFile := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(tempFile, []byte(content), 0o600); err != nil {
		t.Fatalf("failed to create temp config file: %v", err)
	}
	return LoadConfig(tempFile)
}

func mustLoadConfig(t *testing.T, content string) *Config {
	t.Helper()

	cfg, err := loadConfigFromString(t, content)
	if err != nil {
		t.Fatalf("LoadConfig() error: %v", err)
	}
	return cfg
}

func TestLoadConfig_Defaults(t *testing.T) {
	cfg := mustLoadConfig(t, minimalScannerYAML)

	if cfg.MQTT.BrokerURL != DefaultBrokerURL {
		t.Errorf("expected default broker URL %q, got %q", DefaultBrokerURL, cfg.MQTT.BrokerURL)
	}
	if cfg.MQTT.ClientID != DefaultClientID {
		t.Errorf("expected default client ID %q, got %q", DefaultClientID, cfg.MQTT.ClientID)
	}
	if cfg.MQTT.GetQoS() != DefaultQoS {
		t.Errorf("expected default QoS %d, got %d", DefaultQoS, cfg.MQTT.GetQoS())
	}
	if cfg.MQTT.KeepAlive != DefaultKeepAlive {
		t.Errorf("expected default keep alive %d, got %d", DefaultKeepAlive, cfg.MQTT.KeepAlive)
	}
	if cfg.HomeAssistant.DiscoveryPrefix != "homeassistant" {
		t.Errorf("expected default discovery prefix 'homeassistant', got %q", cfg.HomeAssistant.DiscoveryPrefix)
	}
	if cfg.HomeAssistant.InstanceID == "" {
		t.Error("expected instance ID to default to hostname")
	}
	if cfg.Logging.Level != "info" || cfg.Logging.Format != "text" {
		t.Errorf("expected default logging info/text, got %s/%s", cfg.Logging.Level, cfg.Logging.Format)
	}
}

func TestLoadConfig_ExplicitQoSZeroIsPreserved(t *testing.T) {
	cfg := mustLoadConfig(t, `
mqtt:
  qos: 0
`+minimalScannerYAML)

	if cfg.MQTT.GetQoS() != 0 {
		t.Errorf("expected explicit QoS 0 to be preserved, got %d", cfg.MQTT.GetQoS())
	}
}

func TestLoadConfig_InvalidQoSRejected(t *testing.T) {
	_, err := loadConfigFromString(t, `
mqtt:
  qos: 3
`+minimalScannerYAML)

	if err == nil || !strings.Contains(err.Error(), "qos") {
		t.Errorf("expected QoS validation error, got: %v", err)
	}
}

func TestLoadConfig_ScannerIDDerivedFromMapKey(t *testing.T) {
	cfg := mustLoadConfig(t, minimalScannerYAML)

	scanner := cfg.Scanners["test_scanner"]
	if scanner.ID != "test_scanner" {
		t.Errorf("expected scanner ID 'test_scanner' from map key, got %q", scanner.ID)
	}
}

func TestLoadConfig_KeyboardLayoutDefaultIsPersisted(t *testing.T) {
	cfg := mustLoadConfig(t, minimalScannerYAML)

	scanner := cfg.Scanners["test_scanner"]
	if scanner.KeyboardLayout != DefaultKeyboardLayout {
		t.Errorf("expected default keyboard layout %q persisted in config map, got %q",
			DefaultKeyboardLayout, scanner.KeyboardLayout)
	}
}

func TestLoadConfig_ScannerOptionsAreNormalized(t *testing.T) {
	cfg := mustLoadConfig(t, `
scanners:
  test_scanner:
    identification:
      vendor_id: 0x60e
      product_id: 0x16c7
    termination_char: "ENTER"
    keyboard_layout: "ES"
`)

	scanner := cfg.Scanners["test_scanner"]
	if scanner.TerminationChar != "enter" {
		t.Errorf("expected termination char normalized to 'enter', got %q", scanner.TerminationChar)
	}
	if scanner.KeyboardLayout != "es" {
		t.Errorf("expected keyboard layout normalized to 'es', got %q", scanner.KeyboardLayout)
	}
}

func TestLoadConfig_UnknownKeyboardLayoutRejected(t *testing.T) {
	_, err := loadConfigFromString(t, `
scanners:
  test_scanner:
    identification:
      vendor_id: 0x60e
      product_id: 0x16c7
    termination_char: "enter"
    keyboard_layout: "klingon"
`)

	if err == nil || !strings.Contains(err.Error(), "keyboard_layout") {
		t.Errorf("expected keyboard layout validation error, got: %v", err)
	}
}

func TestLoadConfig_NoScannersRejected(t *testing.T) {
	_, err := loadConfigFromString(t, `
mqtt:
  broker_url: "mqtt://localhost:1883"
`)

	if err == nil || !strings.Contains(err.Error(), "scanner") {
		t.Errorf("expected error for missing scanners, got: %v", err)
	}
}

func TestLoadConfig_InvalidBrokerSchemeRejected(t *testing.T) {
	_, err := loadConfigFromString(t, `
mqtt:
  broker_url: "http://localhost:1883"
`+minimalScannerYAML)

	if err == nil || !strings.Contains(err.Error(), "broker_url") {
		t.Errorf("expected broker URL validation error, got: %v", err)
	}
}

func TestLoadConfig_MissingFile(t *testing.T) {
	_, err := LoadConfig(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	if err == nil {
		t.Error("expected error for missing config file")
	}
}

func TestLoadConfig_MalformedYAML(t *testing.T) {
	_, err := loadConfigFromString(t, "scanners: [not: valid")
	if err == nil {
		t.Error("expected error for malformed YAML")
	}
}

func TestMQTTConfig_GetQoS_NilDefaults(t *testing.T) {
	cfg := &MQTTConfig{}
	if cfg.GetQoS() != DefaultQoS {
		t.Errorf("expected nil QoS to default to %d, got %d", DefaultQoS, cfg.GetQoS())
	}

	zero := byte(0)
	cfg.QoS = &zero
	if cfg.GetQoS() != 0 {
		t.Errorf("expected explicit QoS 0, got %d", cfg.GetQoS())
	}
}

func TestValidateTerminationChar(t *testing.T) {
	tests := []struct {
		name        string
		termChar    string
		expectError bool
	}{
		{"Valid enter", "enter", false},
		{"Valid tab", "tab", false},
		{"Valid none", "none", false},
		{"Invalid char", "invalid", true},
		{"Empty string", "", true},
	}

	config := &Config{}
	validChars := []string{"enter", "tab", "none"}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scanner := &ScannerConfig{TerminationChar: tt.termChar}
			err := config.validateTerminationChar("test", scanner, validChars)

			if tt.expectError && err == nil {
				t.Errorf("expected error for termination char %q", tt.termChar)
			}
			if !tt.expectError && err != nil {
				t.Errorf("expected no error for termination char %q, got: %v", tt.termChar, err)
			}
		})
	}
}

func TestValidateScannerIdentification(t *testing.T) {
	tests := []struct {
		name        string
		vendorID    uint16
		productID   uint16
		expectError bool
	}{
		{"Valid IDs", 0x60e, 0x16c7, false},
		{"Zero vendor ID", 0, 0x16c7, true},
		{"Zero product ID", 0x60e, 0, true},
		{"Both zero", 0, 0, true},
	}

	config := &Config{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scanner := &ScannerConfig{
				Identification: ScannerIdentification{
					VendorID:  tt.vendorID,
					ProductID: tt.productID,
				},
			}

			err := config.validateScannerIdentification("test", scanner)

			if tt.expectError && err == nil {
				t.Errorf("expected error for VID:PID %04x:%04x", tt.vendorID, tt.productID)
			}
			if !tt.expectError && err != nil {
				t.Errorf("expected no error for VID:PID %04x:%04x, got: %v", tt.vendorID, tt.productID, err)
			}
		})
	}
}

func TestMQTTConfig_IsSecure(t *testing.T) {
	tests := []struct {
		brokerURL string
		expected  bool
	}{
		{"mqtt://localhost:1883", false},
		{"mqtts://localhost:8883", true},
		{"ws://localhost:9001", false},
		{"wss://localhost:9002", true},
		{"tcp://localhost:1883", false},
	}

	for _, tt := range tests {
		t.Run(tt.brokerURL, func(t *testing.T) {
			config := &MQTTConfig{BrokerURL: tt.brokerURL}
			if got := config.IsSecure(); got != tt.expected {
				t.Errorf("IsSecure() = %v, expected %v for URL %s", got, tt.expected, tt.brokerURL)
			}
		})
	}
}

func TestLoadConfig_InvalidLoggingRejected(t *testing.T) {
	_, err := loadConfigFromString(t, `
logging:
  level: "verbose"
`+minimalScannerYAML)

	if err == nil || !strings.Contains(err.Error(), "logging.level") {
		t.Errorf("expected logging level validation error, got: %v", err)
	}
}
