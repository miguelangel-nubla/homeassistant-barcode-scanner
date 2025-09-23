package config

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config represents the application configuration
type Config struct {
	MQTT          MQTTConfig          `yaml:"mqtt"`
	Scanner       ScannerConfig       `yaml:"scanner"`
	HomeAssistant HomeAssistantConfig `yaml:"homeassistant"`
	Logging       LoggingConfig       `yaml:"logging"`
}

// MQTTConfig contains MQTT broker settings
type MQTTConfig struct {
	BrokerURL          string `yaml:"broker_url"`
	Username           string `yaml:"username,omitempty"`
	Password           string `yaml:"password,omitempty"`
	ClientID           string `yaml:"client_id"`
	QoS                byte   `yaml:"qos"`
	Retained           bool   `yaml:"retained"`
	KeepAlive          int    `yaml:"keep_alive"`
	InsecureSkipVerify bool   `yaml:"insecure_skip_verify"`
}

// ScannerConfig contains barcode scanner settings
type ScannerConfig struct {
	VendorID        uint16 `yaml:"vendor_id,omitempty"`
	ProductID       uint16 `yaml:"product_id,omitempty"`
	DevicePath      string `yaml:"device_path,omitempty"`
	TerminationChar string `yaml:"termination_char,omitempty"` // "enter", "tab", "none", or empty for auto-timeout
}

// HomeAssistantConfig contains Home Assistant specific settings
type HomeAssistantConfig struct {
	EntityID        string `yaml:"entity_id"`
	DeviceName      string `yaml:"device_name"`
	DiscoveryPrefix string `yaml:"discovery_prefix"`
}

// LoggingConfig contains logging settings
type LoggingConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

// GetStateTopic returns the state topic for the entity
func (h *HomeAssistantConfig) GetStateTopic() string {
	return fmt.Sprintf("%s/sensor/%s/state", h.DiscoveryPrefix, h.EntityID)
}

// GetAvailabilityTopic returns the availability topic for the entity
func (h *HomeAssistantConfig) GetAvailabilityTopic() string {
	return fmt.Sprintf("%s/sensor/%s/availability", h.DiscoveryPrefix, h.EntityID)
}

// GetDiscoveryTopic returns the discovery topic for the entity
func (h *HomeAssistantConfig) GetDiscoveryTopic() string {
	return fmt.Sprintf("%s/sensor/%s/config", h.DiscoveryPrefix, h.EntityID)
}

// IsSecure returns true if the MQTT broker URL uses a secure protocol
func (m *MQTTConfig) IsSecure() bool {
	return strings.HasPrefix(m.BrokerURL, "mqtts://") || strings.HasPrefix(m.BrokerURL, "wss://")
}

// LoadConfig loads configuration from a YAML file
func LoadConfig(configPath string) (*Config, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	config := &Config{}
	if err := yaml.Unmarshal(data, config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Set defaults and validate
	config.setDefaults()
	if err := config.validate(); err != nil {
		return nil, err
	}

	return config, nil
}

// setDefaults sets default values for configuration fields
func (c *Config) setDefaults() {
	// MQTT defaults
	if c.MQTT.BrokerURL == "" {
		c.MQTT.BrokerURL = "mqtt://localhost:1883"
	}
	if c.MQTT.ClientID == "" {
		c.MQTT.ClientID = "homeassistant-barcode-scanner"
	}
	if c.MQTT.QoS == 0 {
		c.MQTT.QoS = 1
	}
	if c.MQTT.KeepAlive == 0 {
		c.MQTT.KeepAlive = 60
	}

	// Home Assistant defaults
	if c.HomeAssistant.DiscoveryPrefix == "" {
		c.HomeAssistant.DiscoveryPrefix = "homeassistant"
	}
	if c.HomeAssistant.DeviceName == "" {
		c.HomeAssistant.DeviceName = "Barcode Scanner"
	}

	// Logging defaults
	if c.Logging.Level == "" {
		c.Logging.Level = "info"
	}
	if c.Logging.Format == "" {
		c.Logging.Format = "text"
	}
}

// validate checks if the configuration is valid
func (c *Config) validate() error {
	// Validate required fields
	if c.MQTT.BrokerURL == "" {
		return fmt.Errorf("mqtt.broker_url is required")
	}
	if c.HomeAssistant.EntityID == "" {
		return fmt.Errorf("homeassistant.entity_id is required")
	}

	// Validate broker URL
	if _, err := url.Parse(c.MQTT.BrokerURL); err != nil {
		return fmt.Errorf("invalid mqtt.broker_url: %w", err)
	}

	validSchemes := []string{"mqtt://", "mqtts://", "ws://", "wss://"}
	validScheme := false
	for _, scheme := range validSchemes {
		if strings.HasPrefix(c.MQTT.BrokerURL, scheme) {
			validScheme = true
			break
		}
	}
	if !validScheme {
		return fmt.Errorf("invalid mqtt.broker_url scheme: must start with mqtt://, mqtts://, ws://, or wss://")
	}

	// Validate QoS
	if c.MQTT.QoS > 2 {
		return fmt.Errorf("invalid mqtt.qos: %d (must be 0, 1, or 2)", c.MQTT.QoS)
	}

	return nil
}

