package config

import (
	"fmt"
	"net/url"
	"os"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config represents the application configuration
type Config struct {
	MQTT          MQTTConfig               `yaml:"mqtt"`
	Scanners      map[string]ScannerConfig `yaml:"scanners"`
	HomeAssistant HomeAssistantConfig      `yaml:"homeassistant"`
	Logging       LoggingConfig            `yaml:"logging"`
}

// MQTTConfig contains MQTT broker settings
type MQTTConfig struct {
	BrokerURL          string `yaml:"broker_url"`
	Username           string `yaml:"username,omitempty"`
	Password           string `yaml:"password,omitempty"`
	ClientID           string `yaml:"client_id"`
	QoS                byte   `yaml:"qos"`
	KeepAlive          int    `yaml:"keep_alive"`
	InsecureSkipVerify bool   `yaml:"insecure_skip_verify"`
}

// ScannerIdentification identifies a scanner by USB vendor/product ID and optional serial
type ScannerIdentification struct {
	VendorID  uint16 `yaml:"vendor_id"`        // USB Vendor ID (required)
	ProductID uint16 `yaml:"product_id"`       // USB Product ID (required)
	Serial    string `yaml:"serial,omitempty"` // Optional serial number for device matching
}

// ScannerConfig contains barcode scanner settings
type ScannerConfig struct {
	ID              string                `yaml:"id"`                         // Required unique identifier for this scanner
	Name            string                `yaml:"name,omitempty"`             // Optional friendly name
	Identification  ScannerIdentification `yaml:"identification"`             // How to identify this specific scanner
	TerminationChar string                `yaml:"termination_char,omitempty"` // "enter", "tab", "none", or empty for auto-timeout
}

// HomeAssistantConfig contains Home Assistant specific settings
type HomeAssistantConfig struct {
	DiscoveryPrefix string `yaml:"discovery_prefix"`
	InstanceID      string `yaml:"instance_id,omitempty"` // Unique identifier for this instance
}

// LoggingConfig contains logging settings
type LoggingConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
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

	// Set scanner IDs from map keys
	for id, scanner := range config.Scanners {
		scanner.ID = id
		config.Scanners[id] = scanner
	}

	if err := config.validate(); err != nil {
		return nil, err
	}

	return config, nil
}

// setDefaults sets default values for configuration fields
func (c *Config) setDefaults() {
	c.setMQTTDefaults()
	c.setHomeAssistantDefaults()
	c.setLoggingDefaults()
}

func (c *Config) setMQTTDefaults() {
	defaults := map[string]interface{}{
		"broker_url": "mqtt://localhost:1883",
		"client_id":  "ha-barcode-bridge",
		"qos":        byte(1),
		"keep_alive": 60,
	}

	if c.MQTT.BrokerURL == "" {
		c.MQTT.BrokerURL = defaults["broker_url"].(string)
	}
	if c.MQTT.ClientID == "" {
		c.MQTT.ClientID = defaults["client_id"].(string)
	}
	if c.MQTT.QoS == 0 {
		c.MQTT.QoS = defaults["qos"].(byte)
	}
	if c.MQTT.KeepAlive == 0 {
		c.MQTT.KeepAlive = defaults["keep_alive"].(int)
	}
}

func (c *Config) setHomeAssistantDefaults() {
	if c.HomeAssistant.DiscoveryPrefix == "" {
		c.HomeAssistant.DiscoveryPrefix = "homeassistant"
	}
}

func (c *Config) setLoggingDefaults() {
	if c.Logging.Level == "" {
		c.Logging.Level = "info"
	}
	if c.Logging.Format == "" {
		c.Logging.Format = "text"
	}
}

// validate checks if the configuration is valid
func (c *Config) validate() error {
	if err := c.validateMQTT(); err != nil {
		return err
	}
	if err := c.validateScanners(); err != nil {
		return err
	}
	if err := c.validateHomeAssistant(); err != nil {
		return err
	}
	return c.validateLogging()
}

// validateMQTT validates MQTT configuration
func (c *Config) validateMQTT() error {
	if c.MQTT.BrokerURL == "" {
		return fmt.Errorf("mqtt.broker_url is required")
	}

	if _, err := url.Parse(c.MQTT.BrokerURL); err != nil {
		return fmt.Errorf("invalid mqtt.broker_url '%s': %w", c.MQTT.BrokerURL, err)
	}

	validSchemes := []string{"mqtt://", "mqtts://", "ws://", "wss://"}
	for _, scheme := range validSchemes {
		if strings.HasPrefix(c.MQTT.BrokerURL, scheme) {
			return c.validateMQTTParams()
		}
	}

	return fmt.Errorf("mqtt.broker_url '%s' must use one of: %s", c.MQTT.BrokerURL, strings.Join(validSchemes, ", "))
}

func (c *Config) validateMQTTParams() error {
	if c.MQTT.QoS > 2 {
		return fmt.Errorf("mqtt.qos must be 0, 1, or 2 (got %d)", c.MQTT.QoS)
	}
	if c.MQTT.KeepAlive < 10 {
		return fmt.Errorf("mqtt.keep_alive must be at least 10 seconds (got %d)", c.MQTT.KeepAlive)
	}
	return nil
}

// validateScanners validates scanner configurations
func (c *Config) validateScanners() error {
	if len(c.Scanners) == 0 {
		return fmt.Errorf("at least one scanner must be configured")
	}

	validTermChars := []string{"", "enter", "return", "tab", "none"}

	for id, scanner := range c.Scanners {
		if err := c.validateScannerIdentification(id, scanner); err != nil {
			return err
		}
		if err := c.validateTerminationChar(id, scanner, validTermChars); err != nil {
			return err
		}
	}
	return nil
}

func (c *Config) validateScannerIdentification(id string, scanner ScannerConfig) error {
	if scanner.Identification.VendorID == 0 {
		return fmt.Errorf("scanners[%s].identification.vendor_id is required", id)
	}
	if scanner.Identification.ProductID == 0 {
		return fmt.Errorf("scanners[%s].identification.product_id is required", id)
	}
	return nil
}

func (c *Config) validateTerminationChar(id string, scanner ScannerConfig, validChars []string) error {
	termChar := strings.ToLower(scanner.TerminationChar)
	if !slices.Contains(validChars, termChar) {
		return fmt.Errorf("scanners[%s].termination_char '%s' must be one of: %s",
			id, scanner.TerminationChar, strings.Join(validChars, ", "))
	}
	return nil
}

// validateHomeAssistant validates Home Assistant configuration
func (c *Config) validateHomeAssistant() error {
	if c.HomeAssistant.DiscoveryPrefix == "" {
		return fmt.Errorf("homeassistant.discovery_prefix is required")
	}
	return nil
}

// validateLogging validates logging configuration
func (c *Config) validateLogging() error {
	validLogLevels := []string{"debug", "info", "warn", "warning", "error", "fatal", "panic"}
	logLevel := strings.ToLower(c.Logging.Level)
	if !slices.Contains(validLogLevels, logLevel) {
		return fmt.Errorf("logging.level '%s' must be one of: %s",
			c.Logging.Level, strings.Join(validLogLevels, ", "))
	}

	validLogFormats := []string{"text", "json"}
	logFormat := strings.ToLower(c.Logging.Format)
	if !slices.Contains(validLogFormats, logFormat) {
		return fmt.Errorf("logging.format '%s' must be one of: %s",
			c.Logging.Format, strings.Join(validLogFormats, ", "))
	}

	return nil
}
