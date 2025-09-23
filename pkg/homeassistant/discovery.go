package homeassistant

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/miguelangel-nubla/homeassistant-barcode-scanner/pkg/config"
	"github.com/miguelangel-nubla/homeassistant-barcode-scanner/pkg/mqtt"
)

// Integration handles Home Assistant MQTT integration
type Integration struct {
	mqtt   *mqtt.Client
	config *config.HomeAssistantConfig
	logger *logrus.Logger
	version string
}

// DeviceInfo represents device information for Home Assistant
type DeviceInfo struct {
	Identifiers  []string `json:"identifiers"`
	Name         string   `json:"name"`
	Model        string   `json:"model"`
	Manufacturer string   `json:"manufacturer"`
	SWVersion    string   `json:"sw_version,omitempty"`
}

// SensorConfig represents the sensor configuration for Home Assistant auto-discovery
type SensorConfig struct {
	Name              string      `json:"name"`
	UniqueID          string      `json:"unique_id"`
	StateTopic        string      `json:"state_topic"`
	AvailabilityTopic string      `json:"availability_topic,omitempty"`
	Device            *DeviceInfo `json:"device,omitempty"`
	Icon              string      `json:"icon,omitempty"`
	ExpireAfter       int         `json:"expire_after,omitempty"`
	ForceUpdate       bool        `json:"force_update"`
}

// BarcodePayload represents the payload structure for barcode values
type BarcodePayload struct {
	Value     string `json:"value"`
	Timestamp string `json:"timestamp"`
}

// NewIntegration creates a new Home Assistant integration instance
func NewIntegration(mqttClient *mqtt.Client, config *config.HomeAssistantConfig, version string, logger *logrus.Logger) *Integration {
	return &Integration{
		mqtt:   mqttClient,
		config: config,
		logger: logger,
		version: version,
	}
}

// Start initializes the Home Assistant integration
func (i *Integration) Start() error {
	i.logger.Info("Starting Home Assistant integration")

	// Set up MQTT callbacks
	i.mqtt.SetOnConnectCallback(i.handleConnect)
	i.mqtt.SetOnDisconnectCallback(i.handleDisconnect)

	// If already connected, perform initial setup
	if i.mqtt.IsConnected() {
		i.handleConnect()
	}

	return nil
}

// Stop shuts down the Home Assistant integration
func (i *Integration) Stop() error {
	i.logger.Info("Stopping Home Assistant integration")

	// Publish offline status
	if i.mqtt.IsConnected() {
		_ = i.publishAvailability("offline")
	}

	return nil
}

// PublishBarcode publishes a scanned barcode to Home Assistant
func (i *Integration) PublishBarcode(barcode string) error {
	payload := &BarcodePayload{
		Value:     barcode,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal barcode payload: %w", err)
	}

	i.logger.Infof("Publishing barcode to Home Assistant: %s", barcode)
	return i.mqtt.Publish(i.config.GetStateTopic(), string(payloadJSON), false)
}

// publishDiscoveryConfig publishes the auto-discovery configuration
func (i *Integration) publishDiscoveryConfig() error {
	device := &DeviceInfo{
		Identifiers:  []string{i.config.EntityID},
		Name:         i.config.DeviceName,
		Model:        "USB Barcode Scanner",
		Manufacturer: "Barcode Scanner Client",
		SWVersion:    i.version,
	}

	sensorConfig := &SensorConfig{
		Name:              fmt.Sprintf("%s Barcode", i.config.DeviceName),
		UniqueID:          i.config.EntityID,
		StateTopic:        i.config.GetStateTopic(),
		AvailabilityTopic: i.config.GetAvailabilityTopic(),
		Device:            device,
		Icon:              "mdi:barcode-scan",
		ForceUpdate:       true,
		ExpireAfter:       300,
	}

	configJSON, err := json.Marshal(sensorConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal sensor config: %w", err)
	}

	i.logger.Infof("Publishing discovery config to: %s", i.config.GetDiscoveryTopic())
	return i.mqtt.Publish(i.config.GetDiscoveryTopic(), string(configJSON), true)
}

// publishAvailability publishes the availability status
func (i *Integration) publishAvailability(status string) error {
	i.logger.Debugf("Publishing availability status: %s", status)
	return i.mqtt.Publish(i.config.GetAvailabilityTopic(), status, false)
}

// handleConnect is called when MQTT connection is established
func (i *Integration) handleConnect() {
	i.logger.Info("MQTT connected, setting up Home Assistant integration")

	if err := i.publishDiscoveryConfig(); err != nil {
		i.logger.Errorf("Failed to publish discovery config: %v", err)
		return
	}

	if err := i.publishAvailability("online"); err != nil {
		i.logger.Errorf("Failed to publish online status: %v", err)
	}
}

// handleDisconnect is called when MQTT connection is lost
func (i *Integration) handleDisconnect() {
	i.logger.Info("MQTT disconnected, Home Assistant integration offline")
}