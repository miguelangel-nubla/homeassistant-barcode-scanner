package homeassistant

import (
	"testing"

	"github.com/miguelangel-nubla/homeassistant-barcode-scanner/pkg/config"
)

func TestGenerateBridgeAvailabilityTopic(t *testing.T) {
	tests := []struct {
		name           string
		config         *config.HomeAssistantConfig
		expectedSuffix string
	}{
		{
			name: "Basic config",
			config: &config.HomeAssistantConfig{
				DiscoveryPrefix: "homeassistant",
				InstanceID:      "test",
			},
			expectedSuffix: "availability",
		},
		{
			name: "Custom prefix",
			config: &config.HomeAssistantConfig{
				DiscoveryPrefix: "custom",
				InstanceID:      "instance1",
			},
			expectedSuffix: "availability",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			topic := GenerateBridgeAvailabilityTopic(tt.config)

			if topic == "" {
				t.Error("Expected topic to be generated")
			}

			if topic[:len(tt.config.DiscoveryPrefix)] != tt.config.DiscoveryPrefix {
				t.Errorf("Expected topic to start with '%s'", tt.config.DiscoveryPrefix)
			}

			if topic[len(topic)-len(tt.expectedSuffix):] != tt.expectedSuffix {
				t.Errorf("Expected topic to end with '%s'", tt.expectedSuffix)
			}
		})
	}
}

func TestDeviceInfo_Structure(t *testing.T) {
	deviceInfo := &DeviceInfo{
		Identifiers:  []string{"test-id"},
		Name:         "Test Device",
		Model:        "Test Model",
		Manufacturer: "Test Manufacturer",
		SWVersion:    "1.0.0",
		ViaDevice:    "bridge-id",
	}

	if len(deviceInfo.Identifiers) != 1 {
		t.Errorf("Expected 1 identifier, got %d", len(deviceInfo.Identifiers))
	}

	if deviceInfo.Identifiers[0] != "test-id" {
		t.Errorf("Expected identifier 'test-id', got %s", deviceInfo.Identifiers[0])
	}

	if deviceInfo.Name != "Test Device" {
		t.Errorf("Expected name 'Test Device', got %s", deviceInfo.Name)
	}
}

func TestSensorConfig_Structure(t *testing.T) {
	availability := []AvailabilityConfig{
		{Topic: "test/availability"},
	}

	sensorConfig := &SensorConfig{
		Name:              "Test Sensor",
		ObjectID:          "test_sensor",
		StateTopic:        "test/state",
		AttributesTopic:   "test/attributes",
		AvailabilityTopic: "test/availability",
		Availability:      availability,
		ForceUpdate:       true,
	}

	if sensorConfig.Name != "Test Sensor" {
		t.Errorf("Expected name 'Test Sensor', got %s", sensorConfig.Name)
	}

	if sensorConfig.ObjectID != "test_sensor" {
		t.Errorf("Expected object ID 'test_sensor', got %s", sensorConfig.ObjectID)
	}

	if len(sensorConfig.Availability) != 1 {
		t.Errorf("Expected 1 availability config, got %d", len(sensorConfig.Availability))
	}

	if sensorConfig.Availability[0].Topic != "test/availability" {
		t.Errorf("Expected availability topic 'test/availability', got %s", sensorConfig.Availability[0].Topic)
	}

	if !sensorConfig.ForceUpdate {
		t.Error("Expected ForceUpdate to be true")
	}
}

func TestScannerTopics_Structure(t *testing.T) {
	topics := &ScannerTopics{
		ConfigTopic:       "homeassistant/sensor/test/config",
		StateTopic:        "homeassistant/sensor/test/state",
		AvailabilityTopic: "homeassistant/sensor/test/availability",
		AttributesTopic:   "homeassistant/sensor/test/attributes",
	}

	if topics.ConfigTopic == "" {
		t.Error("Expected config topic to be set")
	}

	if topics.StateTopic == "" {
		t.Error("Expected state topic to be set")
	}

	if topics.AvailabilityTopic == "" {
		t.Error("Expected availability topic to be set")
	}

	if topics.AttributesTopic == "" {
		t.Error("Expected attributes topic to be set")
	}
}

func TestScannerDevice_Structure(t *testing.T) {
	topics := &ScannerTopics{
		ConfigTopic:       "test/config",
		StateTopic:        "test/state",
		AvailabilityTopic: "test/availability",
		AttributesTopic:   "test/attributes",
	}

	device := &ScannerDevice{
		ID:        "test_scanner",
		Name:      "Test Scanner",
		Connected: true,
		Topics:    topics,
	}

	if device.ID != "test_scanner" {
		t.Errorf("Expected ID 'test_scanner', got %s", device.ID)
	}

	if device.Name != "Test Scanner" {
		t.Errorf("Expected name 'Test Scanner', got %s", device.Name)
	}

	if !device.Connected {
		t.Error("Expected device to be connected")
	}

	if device.Topics != topics {
		t.Error("Expected topics to match")
	}
}
