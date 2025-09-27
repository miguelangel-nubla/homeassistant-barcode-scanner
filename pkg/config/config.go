package config

import (
	"fmt"
	"net/url"
	"os"
	"slices"
	"strings"

	"github.com/miguelangel-nubla/homeassistant-barcode-scanner/pkg/layouts"
	"gopkg.in/yaml.v3"
)

type Config struct {
	MQTT          MQTTConfig               `yaml:"mqtt"`
	Scanners      map[string]ScannerConfig `yaml:"scanners"`
	HomeAssistant HomeAssistantConfig      `yaml:"homeassistant"`
	Logging       LoggingConfig            `yaml:"logging"`
}

type MQTTConfig struct {
	BrokerURL          string `yaml:"broker_url"`
	Username           string `yaml:"username,omitempty"`
	Password           string `yaml:"password,omitempty"`
	ClientID           string `yaml:"client_id"`
	QoS                byte   `yaml:"qos"`
	KeepAlive          int    `yaml:"keep_alive"`
	InsecureSkipVerify bool   `yaml:"insecure_skip_verify"`
}

type ScannerIdentification struct {
	VendorID  uint16 `yaml:"vendor_id"`
	ProductID uint16 `yaml:"product_id"`
	Serial    string `yaml:"serial,omitempty"`
}

type ScannerConfig struct {
	ID              string                `yaml:"id"`
	Name            string                `yaml:"name,omitempty"`
	Identification  ScannerIdentification `yaml:"identification"`
	TerminationChar string                `yaml:"termination_char,omitempty"`
	KeyboardLayout  string                `yaml:"keyboard_layout,omitempty"`
}

type HomeAssistantConfig struct {
	DiscoveryPrefix string `yaml:"discovery_prefix"`
	InstanceID      string `yaml:"instance_id,omitempty"` // Unique identifier for this instance
}

type LoggingConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

func (m *MQTTConfig) IsSecure() bool {
	return strings.HasPrefix(m.BrokerURL, "mqtts://") || strings.HasPrefix(m.BrokerURL, "wss://")
}

func LoadConfig(configPath string) (*Config, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	config := &Config{}
	if err := yaml.Unmarshal(data, config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	config.setDefaults()

	for id, scanner := range config.Scanners {
		scanner.ID = id
		config.Scanners[id] = scanner
	}

	if err := config.validate(); err != nil {
		return nil, err
	}

	return config, nil
}

func (c *Config) setDefaults() {
	c.setMQTTDefaults()
	c.setHomeAssistantDefaults()
	c.setLoggingDefaults()
}

func (c *Config) setMQTTDefaults() {
	defaults := map[string]any{
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

func (c *Config) validateScanners() error {
	if len(c.Scanners) == 0 {
		return fmt.Errorf("at least one scanner must be configured")
	}

	validTermChars := []string{"enter", "tab", "none"}

	for id, scanner := range c.Scanners {
		if err := c.validateScannerIdentification(id, &scanner); err != nil {
			return err
		}
		if err := c.validateTerminationChar(id, &scanner, validTermChars); err != nil {
			return err
		}
		if err := c.validateKeyboardLayout(id, &scanner); err != nil {
			return err
		}
	}
	return nil
}

func (c *Config) validateScannerIdentification(id string, scanner *ScannerConfig) error {
	if scanner.Identification.VendorID == 0 {
		return fmt.Errorf("scanners[%s].identification.vendor_id is required", id)
	}
	if scanner.Identification.ProductID == 0 {
		return fmt.Errorf("scanners[%s].identification.product_id is required", id)
	}
	return nil
}

func (c *Config) validateTerminationChar(id string, scanner *ScannerConfig, validChars []string) error {
	termChar := strings.ToLower(scanner.TerminationChar)
	if !slices.Contains(validChars, termChar) {
		return fmt.Errorf("scanners[%s].termination_char '%s' must be one of: %s",
			id, scanner.TerminationChar, strings.Join(validChars, ", "))
	}
	return nil
}

func (c *Config) validateKeyboardLayout(id string, scanner *ScannerConfig) error {
	if scanner.KeyboardLayout == "" {
		scanner.KeyboardLayout = "us" // Set default
	}

	availableLayouts, err := getAvailableKeyboardLayouts()
	if err != nil {
		return fmt.Errorf("failed to scan available keyboard layouts: %w", err)
	}

	layoutName := strings.ToLower(scanner.KeyboardLayout)
	if !slices.Contains(availableLayouts, layoutName) {
		return fmt.Errorf("scanners[%s].keyboard_layout '%s' is not available. Available layouts: %s",
			id, scanner.KeyboardLayout, strings.Join(availableLayouts, ", "))
	}

	return nil
}

func getAvailableKeyboardLayouts() ([]string, error) {
	return layouts.GetAvailableLayouts()
}

func (c *Config) validateHomeAssistant() error {
	if c.HomeAssistant.DiscoveryPrefix == "" {
		return fmt.Errorf("homeassistant.discovery_prefix is required")
	}

	if c.HomeAssistant.InstanceID == "" {
		hostname, err := os.Hostname()
		if err != nil {
			return fmt.Errorf("failed to get hostname for instance_id: %w", err)
		}
		c.HomeAssistant.InstanceID = hostname
	}

	return nil
}

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
