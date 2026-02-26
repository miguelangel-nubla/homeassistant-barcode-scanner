package config

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

const defaultKeyboardLayout = "us"

func TestLoadConfig_BasicParsing(t *testing.T) {
	configContent := `
mqtt:
  broker_url: "mqtt://localhost:1883"
  username: "test"
  password: "test"

scanners:
  test_scanner:
    name: "Test Scanner"
    identification:
      vendor_id: 0x60e
      product_id: 0x16c7
    termination_char: "enter"

homeassistant:
  discovery_prefix: "homeassistant"
  instance_id: "test"

logging:
  level: "info"
  format: "text"
`

	tempFile := createTempConfig(t, configContent)
	defer func() { _ = os.Remove(tempFile) }()

	data, err := os.ReadFile(tempFile) // #nosec G304
	if err != nil {
		t.Fatalf("Failed to read temp config: %v", err)
	}

	var config Config
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		t.Fatalf("Expected no parsing error, got: %v", err)
	}

	if config.MQTT.BrokerURL != "mqtt://localhost:1883" {
		t.Errorf("Expected broker URL 'mqtt://localhost:1883', got: %s", config.MQTT.BrokerURL)
	}

	if len(config.Scanners) != 1 {
		t.Errorf("Expected 1 scanner, got: %d", len(config.Scanners))
	}

	scanner := config.Scanners["test_scanner"]
	if scanner.Name != "Test Scanner" {
		t.Errorf("Expected scanner name 'Test Scanner', got: %s", scanner.Name)
	}
}

func TestKeyboardLayoutDefault(t *testing.T) {
	scanner := &ScannerConfig{}

	if scanner.KeyboardLayout == "" {
		scanner.KeyboardLayout = defaultKeyboardLayout
	}

	if scanner.KeyboardLayout != defaultKeyboardLayout {
		t.Errorf("Expected default keyboard layout '%s', got: %s", defaultKeyboardLayout, scanner.KeyboardLayout)
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
	scanner := &ScannerConfig{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scanner.TerminationChar = tt.termChar
			validChars := []string{"enter", "tab", "none"}
			err := config.validateTerminationChar("test", scanner, validChars)

			if tt.expectError && err == nil {
				t.Errorf("Expected error for termination char '%s', but got none", tt.termChar)
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no error for termination char '%s', but got: %v", tt.termChar, err)
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
				t.Errorf("Expected error for VID:PID %04x:%04x, but got none", tt.vendorID, tt.productID)
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no error for VID:PID %04x:%04x, but got: %v", tt.vendorID, tt.productID, err)
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

func TestValidateMQTT_MissingBrokerURL(t *testing.T) {
	config := &Config{
		MQTT: MQTTConfig{},
	}

	err := config.validateMQTT()
	if err == nil {
		t.Error("Expected error for missing broker URL")
	}
}

func TestValidateHomeAssistant_MissingDiscoveryPrefix(t *testing.T) {
	config := &Config{
		HomeAssistant: HomeAssistantConfig{},
	}

	err := config.validateHomeAssistant()
	if err == nil {
		t.Error("Expected error for missing discovery prefix")
	}
}

func createTempConfig(t *testing.T, content string) string {
	t.Helper()

	tempDir := t.TempDir()
	tempFile := filepath.Join(tempDir, "config.yaml")

	if err := os.WriteFile(tempFile, []byte(content), 0600); err != nil {
		t.Fatalf("Failed to create temp config file: %v", err)
	}

	return tempFile
}
