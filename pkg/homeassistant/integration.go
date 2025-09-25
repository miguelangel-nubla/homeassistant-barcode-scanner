package homeassistant

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/karalabe/hid"
	"github.com/sirupsen/logrus"

	"github.com/miguelangel-nubla/homeassistant-barcode-scanner/pkg/config"
	"github.com/miguelangel-nubla/homeassistant-barcode-scanner/pkg/mqtt"
)

type DeviceInfo struct {
	Identifiers  []string `json:"identifiers"`
	Name         string   `json:"name"`
	Model        string   `json:"model,omitempty"`
	Manufacturer string   `json:"manufacturer,omitempty"`
	SWVersion    string   `json:"sw_version,omitempty"`
	ViaDevice    string   `json:"via_device,omitempty"`
}

type AvailabilityConfig struct {
	Topic string `json:"topic"`
}

type SensorConfig struct {
	Name              string               `json:"name"`
	ObjectID          string               `json:"object_id,omitempty"`
	UniqueID          string               `json:"unique_id"`
	TildeTopic        string               `json:"~,omitempty"`
	StateTopic        string               `json:"state_topic"`
	AttributesToopic  string               `json:"json_attributes_topic,omitempty"`
	AvailabilityTopic string               `json:"availability_topic,omitempty"`
	Availability      []AvailabilityConfig `json:"availability,omitempty"`
	AvailabilityMode  string               `json:"availability_mode,omitempty"`
	Device            *DeviceInfo          `json:"device,omitempty"`
	Icon              string               `json:"icon,omitempty"`
	ForceUpdate       bool                 `json:"force_update,omitempty"`
}

type Integration struct {
	mqtt             *mqtt.Client
	config           *config.HomeAssistantConfig
	logger           *logrus.Logger
	version          string
	scanners         map[string]*ScannerDevice
	scannerConfigs   map[string]*config.ScannerConfig
	bridgeDeviceInfo *DeviceInfo
}

type ScannerDevice struct {
	ID         string
	Name       string
	Connected  bool
	DeviceInfo *DeviceInfo
	Topics     *ScannerTopics
}

type ScannerTopics struct {
	ConfigTopic       string
	StateTopic        string
	AvailabilityTopic string
	AttributesTopic   string
}

func NewIntegration(
	mqttClient *mqtt.Client,
	haConfig *config.HomeAssistantConfig,
	version string,
	logger *logrus.Logger,
) *Integration {
	integration := &Integration{
		mqtt:           mqttClient,
		config:         haConfig,
		logger:         logger,
		version:        version,
		scanners:       make(map[string]*ScannerDevice),
		scannerConfigs: make(map[string]*config.ScannerConfig),
	}

	bridgeID := integration.generateBridgeDeviceID()
	integration.bridgeDeviceInfo = &DeviceInfo{
		Identifiers:  []string{bridgeID},
		Name:         "HA Barcode Bridge",
		Model:        "https://github.com/miguelangel-nubla/homeassistant-barcode-scanner",
		Manufacturer: "Miguel Angel Nubla",
		SWVersion:    version,
	}

	return integration
}

func (integration *Integration) Start() error {
	integration.logger.Info("Starting Home Assistant integration")

	integration.mqtt.SetOnConnectCallback(integration.handleConnect)
	integration.mqtt.SetOnDisconnectCallback(integration.handleDisconnect)

	if integration.mqtt.IsConnected() {
		integration.handleConnect()
	}

	return nil
}

func (integration *Integration) Stop() error {
	integration.logger.Info("Stopping Home Assistant integration")

	if integration.mqtt.IsConnected() {
		for scannerID := range integration.scanners {
			if err := integration.publishScannerAvailability(scannerID, "offline"); err != nil {
				integration.logger.WithField("scanner_id", scannerID).WithError(err).Error("Failed to publish offline status")
			}
		}

		// Publish offline status for bridge availability (for graceful shutdown)
		// LWT only triggers on unexpected disconnections, so we need manual publish for graceful shutdown
		if err := integration.publishBridgeAvailability("offline"); err != nil {
			integration.logger.WithError(err).Error("Failed to publish bridge offline status")
		}
	}

	return nil
}

func (integration *Integration) AddScanner(scannerID, scannerName string, scannerConfig *config.ScannerConfig) {
	integration.logger.Infof("Registering scanner configuration: %s", scannerID)

	integration.scannerConfigs[scannerID] = scannerConfig
	integration.logger.Debugf("Stored config for scanner %s, will create HA device when hardware connects", scannerID)
}

func (integration *Integration) RemoveScanner(scannerID string) {
	integration.logger.Infof("Removing scanner from Home Assistant integration: %s", scannerID)

	if integration.mqtt.IsConnected() {
		scanner := integration.scanners[scannerID]
		if scanner != nil {
			if err := integration.publishScannerAvailability(scannerID, "offline"); err != nil {
				integration.logger.Errorf("Failed to publish offline status for removed scanner %s: %v", scannerID, err)
			}
		}
	}

	delete(integration.scanners, scannerID)
	delete(integration.scannerConfigs, scannerID)
}

func (integration *Integration) SetScannerDeviceInfo(scannerID string, deviceInfo *hid.DeviceInfo) {
	if _, exists := integration.scannerConfigs[scannerID]; !exists {
		integration.logger.Errorf("Scanner config %s not found, cannot create HA device", scannerID)
		return
	}

	displayName := strings.TrimSpace(deviceInfo.Manufacturer)
	if deviceInfo.Product != "" {
		if displayName != "" {
			displayName = fmt.Sprintf("%s %s", displayName, strings.TrimSpace(deviceInfo.Product))
		} else {
			displayName = strings.TrimSpace(deviceInfo.Product)
		}
	}

	if displayName == "" {
		displayName = fmt.Sprintf("Scanner %s", scannerID)
	}

	bridgeID := integration.generateBridgeDeviceID()
	scannerDeviceID := integration.generateScannerDeviceID(scannerID)

	scanner := &ScannerDevice{
		ID:        scannerID,
		Name:      displayName,
		Connected: false, // Will be set by SetScannerConnected
		Topics:    integration.generateScannerTopics(scannerID),
		DeviceInfo: &DeviceInfo{
			Identifiers:  []string{scannerDeviceID},
			Name:         displayName,
			Model:        strings.TrimSpace(deviceInfo.Product),
			Manufacturer: strings.TrimSpace(deviceInfo.Manufacturer),
			ViaDevice:    bridgeID,
		},
	}

	integration.scanners[scannerID] = scanner

	integration.logger.Infof("Created HA device for scanner %s: %s %s (VID:PID %04x:%04x)",
		scannerID, deviceInfo.Manufacturer, deviceInfo.Product, deviceInfo.VendorID, deviceInfo.ProductID)

	// Publish availability first (required for HA auto-discovery timing)
	if integration.mqtt.IsConnected() {
		if err := integration.publishScannerAvailability(scannerID, "offline"); err != nil {
			integration.logger.Errorf("Failed to publish initial availability for scanner %s: %v", scannerID, err)
		}

		if err := integration.publishScannerDiscoveryConfig(scannerID); err != nil {
			integration.logger.Errorf("Failed to publish discovery config for scanner %s: %v", scannerID, err)
		}
	}
}

func (integration *Integration) SetScannerConnected(scannerID string, connected bool) error {
	scanner, exists := integration.scanners[scannerID]
	if !exists {
		return fmt.Errorf("scanner %s not found", scannerID)
	}

	scanner.Connected = connected

	if connected {
		if err := integration.publishScannerAvailability(scannerID, "online"); err != nil {
			return err
		}
		if err := integration.publishScannerState(scannerID, "unknown"); err != nil {
			return err
		}
		if err := integration.publishScannerAttributes(scannerID); err != nil {
			return err
		}
	} else {
		if err := integration.publishScannerAvailability(scannerID, "offline"); err != nil {
			return err
		}
	}

	if err := integration.publishScannerSummary(); err != nil {
		integration.logger.WithError(err).Error("Failed to update scanner summary")
	}
	if err := integration.publishDiagnostics(); err != nil {
		integration.logger.WithError(err).Error("Failed to update diagnostics")
	}
	return nil
}

func (integration *Integration) PublishBarcode(scannerID, barcode string) error {
	scanner, exists := integration.scanners[scannerID]
	if !exists {
		return fmt.Errorf("scanner %s not found", scannerID)
	}

	if !integration.mqtt.IsConnected() {
		return fmt.Errorf("MQTT not connected")
	}

	if err := integration.publishScannerState(scannerID, barcode); err != nil {
		return err
	}

	attributes := map[string]any{
		"scanner_id": scannerID,
	}

	if scannerCfg, exists := integration.scannerConfigs[scannerID]; exists {
		attributes["keyboard_layout"] = scannerCfg.KeyboardLayout
		attributes["termination_char"] = scannerCfg.TerminationChar
	}

	attributesJSON, err := json.Marshal(attributes)
	if err != nil {
		return fmt.Errorf("failed to marshal attributes: %w", err)
	}

	if err := integration.mqtt.Publish(scanner.Topics.AttributesTopic, string(attributesJSON), false); err != nil {
		return fmt.Errorf("failed to publish attributes: %w", err)
	}

	return nil
}

func (integration *Integration) generateBridgeDeviceID() string {
	if integration.config.InstanceID != "" {
		return fmt.Sprintf("ha-barcode-bridge-%s", integration.config.InstanceID)
	}
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}
	return fmt.Sprintf("ha-barcode-bridge-%s", hostname)
}

func (integration *Integration) GenerateBridgeAvailabilityTopic() string {
	bridgeID := integration.generateBridgeDeviceID()
	return fmt.Sprintf("%s/sensor/%s/availability", integration.config.DiscoveryPrefix, bridgeID)
}

func GenerateBridgeAvailabilityTopic(haConfig *config.HomeAssistantConfig) string {
	bridgeID := generateBridgeDeviceID(haConfig)
	return fmt.Sprintf("%s/sensor/%s/availability", haConfig.DiscoveryPrefix, bridgeID)
}

func generateBridgeDeviceID(haConfig *config.HomeAssistantConfig) string {
	if haConfig.InstanceID != "" {
		return fmt.Sprintf("ha-barcode-bridge-%s", haConfig.InstanceID)
	}
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}
	return fmt.Sprintf("ha-barcode-bridge-%s", hostname)
}

func (integration *Integration) generateScannerDeviceID(scannerID string) string {
	bridgeID := integration.generateBridgeDeviceID()
	return fmt.Sprintf("%s-scanner-%s", bridgeID, scannerID)
}

func (integration *Integration) generateScannerTopics(scannerID string) *ScannerTopics {
	bridgeID := integration.generateBridgeDeviceID()
	entityID := fmt.Sprintf("%s-scanner-%s", bridgeID, scannerID)

	return &ScannerTopics{
		ConfigTopic:       fmt.Sprintf("%s/sensor/%s/config", integration.config.DiscoveryPrefix, entityID),
		StateTopic:        fmt.Sprintf("%s/sensor/%s/state", integration.config.DiscoveryPrefix, entityID),
		AvailabilityTopic: fmt.Sprintf("%s/sensor/%s/availability", integration.config.DiscoveryPrefix, entityID),
		AttributesTopic:   fmt.Sprintf("%s/sensor/%s/attributes", integration.config.DiscoveryPrefix, entityID),
	}
}

func (integration *Integration) getScannerSummaryStatus() string {
	connectedCount := integration.getConnectedScannerCount()
	totalCount := len(integration.scanners)

	if connectedCount == 0 {
		return "offline"
	}
	if connectedCount == totalCount {
		return "online"
	}
	return "partial"
}

func (integration *Integration) getDiagnosticsStatus() string {
	if integration.mqtt.IsConnected() {
		return "healthy"
	}
	return "error"
}

func (integration *Integration) handleConnect() {
	integration.logger.Info("MQTT connected, publishing bridge availability and discovery configs")

	if err := integration.publishBridgeAvailability("online"); err != nil {
		integration.logger.WithError(err).Error("Failed to publish bridge availability")
	}

	if err := integration.publishScannerSummary(); err != nil {
		integration.logger.WithError(err).Error("Failed to update scanner summary")
	}
	if err := integration.publishDiagnostics(); err != nil {
		integration.logger.WithError(err).Error("Failed to update diagnostics")
	}

	if err := integration.publishScannerSummaryDiscoveryConfig(); err != nil {
		integration.logger.WithError(err).Error("Failed to publish scanner summary discovery config")
	}
	if err := integration.publishDiagnosticsDiscoveryConfig(); err != nil {
		integration.logger.WithError(err).Error("Failed to publish diagnostics discovery config")
	}

	// Publish discovery config for all scanners that exist (only connected hardware)
	// Note: availability for individual scanners is already handled by SetScannerDeviceInfo/SetScannerConnected
	for scannerID := range integration.scanners {
		if err := integration.publishScannerDiscoveryConfig(scannerID); err != nil {
			integration.logger.WithField("scanner_id", scannerID).WithError(err).Error("Failed to publish discovery config")
		}
	}
}

func (integration *Integration) handleDisconnect() {
	integration.logger.Warn("MQTT disconnected")
}

func (integration *Integration) publishScannerDiscoveryConfig(scannerID string) error {
	scanner, exists := integration.scanners[scannerID]
	if !exists || scanner.DeviceInfo == nil {
		return fmt.Errorf("scanner %s not found or device info not set", scannerID)
	}

	bridgeID := integration.generateBridgeDeviceID()

	sensorName := scanner.Name
	if sensorName == "" {
		sensorName = scannerID
	}

	baseTopic := fmt.Sprintf("%s/sensor/%s-scanner-%s", integration.config.DiscoveryPrefix, bridgeID, scannerID)

	sensorConfig := SensorConfig{
		Name:             sensorName,
		ObjectID:         fmt.Sprintf("%s_%s", integration.config.InstanceID, scannerID),
		UniqueID:         fmt.Sprintf("%s-scanner-%s", bridgeID, scannerID),
		TildeTopic:       baseTopic,
		StateTopic:       "~/state",
		AttributesToopic: "~/attributes",
		// Use tiered availability: scanner must be online AND bridge must be online
		Availability: []AvailabilityConfig{
			{
				Topic: "~/availability",
			},
			{
				Topic: integration.GenerateBridgeAvailabilityTopic(),
			},
		},
		AvailabilityMode: "all", // Both must be online for entity to be available
		Device:           scanner.DeviceInfo,
		Icon:             "mdi:barcode-scan",
		ForceUpdate:      true,
	}

	configJSON, err := json.Marshal(sensorConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal discovery config: %w", err)
	}

	// Use retain=true for discovery configs so they persist for HA restarts
	return integration.mqtt.Publish(scanner.Topics.ConfigTopic, string(configJSON), true)
}

func (integration *Integration) publishBridgeAvailability(status string) error {
	topic := integration.GenerateBridgeAvailabilityTopic()
	// Use retain=true for bridge availability messages so HA sees the last known status
	return integration.mqtt.Publish(topic, status, true)
}

func (integration *Integration) publishScannerAvailability(scannerID, status string) error {
	scanner, exists := integration.scanners[scannerID]
	if !exists {
		return fmt.Errorf("scanner %s not found", scannerID)
	}

	// Use retain=true for availability messages so HA sees the last known status when subscribing
	return integration.mqtt.Publish(scanner.Topics.AvailabilityTopic, status, true)
}

func (integration *Integration) publishScannerState(scannerID, state string) error {
	scanner, exists := integration.scanners[scannerID]
	if !exists {
		return fmt.Errorf("scanner %s not found", scannerID)
	}

	return integration.mqtt.Publish(scanner.Topics.StateTopic, state, false)
}

func (integration *Integration) publishScannerAttributes(scannerID string) error {
	scanner, exists := integration.scanners[scannerID]
	if !exists {
		return fmt.Errorf("scanner %s not found", scannerID)
	}

	attributes := map[string]any{
		"scanner_id": scannerID,
	}

	if scannerCfg, exists := integration.scannerConfigs[scannerID]; exists {
		attributes["keyboard_layout"] = scannerCfg.KeyboardLayout
		attributes["termination_char"] = scannerCfg.TerminationChar
	}

	attributesJSON, err := json.Marshal(attributes)
	if err != nil {
		return fmt.Errorf("failed to marshal attributes: %w", err)
	}

	return integration.mqtt.Publish(scanner.Topics.AttributesTopic, string(attributesJSON), false)
}

func (integration *Integration) generateBridgeEntityTopics(entityType string) (topics *ScannerTopics, baseTopic string) {
	bridgeID := integration.generateBridgeDeviceID()
	entityID := fmt.Sprintf("%s-%s", bridgeID, entityType)
	baseTopic = fmt.Sprintf("%s/sensor/%s", integration.config.DiscoveryPrefix, entityID)

	topics = &ScannerTopics{
		ConfigTopic:       fmt.Sprintf("%s/config", baseTopic),
		StateTopic:        fmt.Sprintf("%s/state", baseTopic),
		AvailabilityTopic: fmt.Sprintf("%s/availability", baseTopic),
		AttributesTopic:   fmt.Sprintf("%s/attributes", baseTopic),
	}

	return
}

func (integration *Integration) publishBridgeEntityDiscoveryConfig(entityType, name, icon string) error {
	topics, baseTopic := integration.generateBridgeEntityTopics(entityType)
	bridgeID := integration.generateBridgeDeviceID()
	entityID := fmt.Sprintf("%s-%s", bridgeID, entityType)

	sensorConfig := SensorConfig{
		Name:             name,
		UniqueID:         entityID,
		TildeTopic:       baseTopic,
		StateTopic:       "~/state",
		AttributesToopic: "~/attributes",
		Availability: []AvailabilityConfig{
			{
				Topic: integration.GenerateBridgeAvailabilityTopic(),
			},
		},
		Device:      integration.bridgeDeviceInfo,
		Icon:        icon,
		ForceUpdate: false,
	}

	configJSON, err := json.Marshal(sensorConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal %s discovery config: %w", entityType, err)
	}

	// Use retain=true for discovery configs so they persist for HA restarts
	return integration.mqtt.Publish(topics.ConfigTopic, string(configJSON), true)
}

func (integration *Integration) publishScannerSummaryDiscoveryConfig() error {
	return integration.publishBridgeEntityDiscoveryConfig("scanners", "Scanner Summary", "mdi:barcode-scan")
}

func (integration *Integration) publishDiagnosticsDiscoveryConfig() error {
	return integration.publishBridgeEntityDiscoveryConfig("diagnostics", "Diagnostics", "mdi:stethoscope")
}

func (integration *Integration) publishScannerSummary() error {
	topics, _ := integration.generateBridgeEntityTopics("scanners")
	status := integration.getScannerSummaryStatus()

	if err := integration.mqtt.Publish(topics.StateTopic, status, false); err != nil {
		return err
	}

	attributes := map[string]any{
		"connected_scanners": integration.getConnectedScannerCount(),
		"total_scanners":     len(integration.scanners),
		"scanner_list":       integration.getScannerList(),
	}

	attributesJSON, err := json.Marshal(attributes)
	if err != nil {
		return fmt.Errorf("failed to marshal scanner summary attributes: %w", err)
	}

	return integration.mqtt.Publish(topics.AttributesTopic, string(attributesJSON), false)
}

func (integration *Integration) publishDiagnostics() error {
	topics, _ := integration.generateBridgeEntityTopics("diagnostics")
	status := integration.getDiagnosticsStatus()

	if err := integration.mqtt.Publish(topics.StateTopic, status, false); err != nil {
		return err
	}

	attributes := map[string]any{
		"version":      integration.version,
		"config_count": len(integration.scannerConfigs),
	}

	attributesJSON, err := json.Marshal(attributes)
	if err != nil {
		return fmt.Errorf("failed to marshal diagnostics attributes: %w", err)
	}

	return integration.mqtt.Publish(topics.AttributesTopic, string(attributesJSON), false)
}

func (integration *Integration) getScannerList() []string {
	scanners := make([]string, 0, len(integration.scanners))
	for scannerID := range integration.scanners {
		scanners = append(scanners, scannerID)
	}
	return scanners
}

func (integration *Integration) getConnectedScannerCount() int {
	count := 0
	for _, scanner := range integration.scanners {
		if scanner.Connected {
			count++
		}
	}
	return count
}
