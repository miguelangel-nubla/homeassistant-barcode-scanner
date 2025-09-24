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

// DeviceInfo represents Home Assistant device information
type DeviceInfo struct {
	Identifiers  []string `json:"identifiers"`
	Name         string   `json:"name"`
	Model        string   `json:"model,omitempty"`
	Manufacturer string   `json:"manufacturer,omitempty"`
	SWVersion    string   `json:"sw_version,omitempty"`
	ViaDevice    string   `json:"via_device,omitempty"`
}

// AvailabilityConfig represents a single availability configuration
type AvailabilityConfig struct {
	Topic string `json:"topic"`
}

// SensorConfig represents Home Assistant MQTT sensor discovery configuration
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

// Integration handles Home Assistant MQTT integration for multiple scanners
type Integration struct {
	mqtt             *mqtt.Client
	config           *config.HomeAssistantConfig
	logger           *logrus.Logger
	version          string
	scanners         map[string]*ScannerDevice
	scannerConfigs   map[string]*config.ScannerConfig
	bridgeDeviceInfo *DeviceInfo
}

// ScannerDevice represents a single scanner's Home Assistant integration state
type ScannerDevice struct {
	ID         string
	Name       string
	Connected  bool
	DeviceInfo *DeviceInfo
	Topics     *ScannerTopics
}

// ScannerTopics holds all MQTT topics for a specific scanner
type ScannerTopics struct {
	ConfigTopic       string
	StateTopic        string
	AvailabilityTopic string
	AttributesTopic   string
}

// NewIntegration creates a new Home Assistant integration
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

	// Create bridge device info
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

// Start initializes the Home Assistant integration
func (integration *Integration) Start() error {
	integration.logger.Info("Starting Home Assistant integration")

	// Set up MQTT callbacks
	integration.mqtt.SetOnConnectCallback(integration.handleConnect)
	integration.mqtt.SetOnDisconnectCallback(integration.handleDisconnect)

	// If already connected, perform initial setup
	if integration.mqtt.IsConnected() {
		integration.handleConnect()
	}

	return nil
}

// Stop shuts down the Home Assistant integration
func (integration *Integration) Stop() error {
	integration.logger.Info("Stopping Home Assistant integration")

	// Publish offline status for all scanners
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

// AddScanner registers a new scanner configuration
func (integration *Integration) AddScanner(scannerID, scannerName string, scannerConfig *config.ScannerConfig) {
	integration.logger.Infof("Registering scanner configuration: %s", scannerID)

	// Store scanner configuration for later use when hardware connects
	integration.scannerConfigs[scannerID] = scannerConfig
	integration.logger.Debugf("Stored config for scanner %s, will create HA device when hardware connects", scannerID)
}

// RemoveScanner removes a scanner from Home Assistant integration
func (integration *Integration) RemoveScanner(scannerID string) {
	integration.logger.Infof("Removing scanner from Home Assistant integration: %s", scannerID)

	if integration.mqtt.IsConnected() {
		// Publish offline status before removing
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

// SetScannerDeviceInfo creates Home Assistant device when hardware scanner connects
func (integration *Integration) SetScannerDeviceInfo(scannerID string, deviceInfo *hid.DeviceInfo) {
	// Verify scanner config exists (must exist from AddScanner)
	if _, exists := integration.scannerConfigs[scannerID]; !exists {
		integration.logger.Errorf("Scanner config %s not found, cannot create HA device", scannerID)
		return
	}

	// Generate device name from hardware info: "Manufacturer Model"
	displayName := strings.TrimSpace(deviceInfo.Manufacturer)
	if deviceInfo.Product != "" {
		if displayName != "" {
			displayName = fmt.Sprintf("%s %s", displayName, strings.TrimSpace(deviceInfo.Product))
		} else {
			displayName = strings.TrimSpace(deviceInfo.Product)
		}
	}

	// Fallback if no hardware info available
	if displayName == "" {
		displayName = fmt.Sprintf("Scanner %s", scannerID)
	}

	// Create Home Assistant device entry NOW with real hardware details
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
		// Publish offline initially - will be updated by SetScannerConnected when actually connected
		if err := integration.publishScannerAvailability(scannerID, "offline"); err != nil {
			integration.logger.Errorf("Failed to publish initial availability for scanner %s: %v", scannerID, err)
		}

		// Now publish discovery config after availability is set
		if err := integration.publishScannerDiscoveryConfig(scannerID); err != nil {
			integration.logger.Errorf("Failed to publish discovery config for scanner %s: %v", scannerID, err)
		}
	}
}

// SetScannerConnected updates scanner connection state
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
	} else {
		if err := integration.publishScannerAvailability(scannerID, "offline"); err != nil {
			return err
		}
	}

	// Update bridge entities
	if err := integration.publishScannerSummary(); err != nil {
		integration.logger.WithError(err).Error("Failed to update scanner summary")
	}
	if err := integration.publishDiagnostics(); err != nil {
		integration.logger.WithError(err).Error("Failed to update diagnostics")
	}
	return nil
}

// PublishBarcode publishes a scanned barcode for a specific scanner
func (integration *Integration) PublishBarcode(scannerID, barcode string) error {
	scanner, exists := integration.scanners[scannerID]
	if !exists {
		return fmt.Errorf("scanner %s not found", scannerID)
	}

	// Topics are always available

	if !integration.mqtt.IsConnected() {
		return fmt.Errorf("MQTT not connected")
	}

	// Publish barcode as sensor state
	if err := integration.publishScannerState(scannerID, barcode); err != nil {
		return err
	}

	// Publish attributes
	attributes := map[string]any{
		"scanner_id": scannerID,
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

// generateBridgeDeviceID creates a unique bridge device identifier
func (integration *Integration) generateBridgeDeviceID() string {
	if integration.config.InstanceID != "" {
		return fmt.Sprintf("ha-barcode-bridge-%s", integration.config.InstanceID)
	}
	// Use hostname as fallback for multiple machine support
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}
	return fmt.Sprintf("ha-barcode-bridge-%s", hostname)
}

// GenerateBridgeAvailabilityTopic creates the bridge availability topic for MQTT will/testament
func (integration *Integration) GenerateBridgeAvailabilityTopic() string {
	bridgeID := integration.generateBridgeDeviceID()
	return fmt.Sprintf("%s/sensor/%s/availability", integration.config.DiscoveryPrefix, bridgeID)
}

// GenerateBridgeAvailabilityTopic creates the bridge availability topic for MQTT will/testament
// Utility function that can be used without creating a full Integration instance
func GenerateBridgeAvailabilityTopic(haConfig *config.HomeAssistantConfig) string {
	bridgeID := generateBridgeDeviceID(haConfig)
	return fmt.Sprintf("%s/sensor/%s/availability", haConfig.DiscoveryPrefix, bridgeID)
}

// generateBridgeDeviceID creates a unique bridge device identifier (utility function)
func generateBridgeDeviceID(haConfig *config.HomeAssistantConfig) string {
	if haConfig.InstanceID != "" {
		return fmt.Sprintf("ha-barcode-bridge-%s", haConfig.InstanceID)
	}
	// Use hostname as fallback for multiple machine support
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}
	return fmt.Sprintf("ha-barcode-bridge-%s", hostname)
}

// generateScannerDeviceID creates a unique scanner device identifier based on configured ID
func (integration *Integration) generateScannerDeviceID(scannerID string) string {
	bridgeID := integration.generateBridgeDeviceID()
	return fmt.Sprintf("%s-scanner-%s", bridgeID, scannerID)
}

// generateScannerTopics creates MQTT topics for a specific scanner based on configured ID
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

// getScannerSummaryStatus returns scanner overview status
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

// getDiagnosticsStatus returns bridge health status
func (integration *Integration) getDiagnosticsStatus() string {
	// For now, simple logic - can be expanded later
	if integration.mqtt.IsConnected() {
		return "healthy"
	}
	return "error"
}

// handleConnect is called when MQTT connects
func (integration *Integration) handleConnect() {
	integration.logger.Info("MQTT connected, publishing bridge availability and discovery configs")

	// Publish bridge availability first
	if err := integration.publishBridgeAvailability("online"); err != nil {
		integration.logger.WithError(err).Error("Failed to publish bridge availability")
	}

	// Publish bridge entities availability and states
	if err := integration.publishScannerSummary(); err != nil {
		integration.logger.WithError(err).Error("Failed to update scanner summary")
	}
	if err := integration.publishDiagnostics(); err != nil {
		integration.logger.WithError(err).Error("Failed to update diagnostics")
	}

	// Now publish bridge discovery configs after availability is set
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

// handleDisconnect is called when MQTT disconnects
func (integration *Integration) handleDisconnect() {
	integration.logger.Warn("MQTT disconnected")
}

// publishScannerDiscoveryConfig publishes discovery configuration for a scanner
func (integration *Integration) publishScannerDiscoveryConfig(scannerID string) error {
	scanner, exists := integration.scanners[scannerID]
	if !exists || scanner.DeviceInfo == nil {
		return fmt.Errorf("scanner %s not found or device info not set", scannerID)
	}

	bridgeID := integration.generateBridgeDeviceID()

	// Generate sensor name: use scanner friendly name if provided, otherwise use scanner ID
	sensorName := scanner.Name
	if sensorName == "" {
		sensorName = scannerID
	}

	// Extract base topic for tilde optimization
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

// publishBridgeAvailability publishes bridge availability status
func (integration *Integration) publishBridgeAvailability(status string) error {
	topic := integration.GenerateBridgeAvailabilityTopic()
	// Use retain=true for bridge availability messages so HA sees the last known status
	return integration.mqtt.Publish(topic, status, true)
}

// publishScannerAvailability publishes availability status for a scanner
func (integration *Integration) publishScannerAvailability(scannerID, status string) error {
	scanner, exists := integration.scanners[scannerID]
	if !exists {
		return fmt.Errorf("scanner %s not found", scannerID)
	}

	// Use retain=true for availability messages so HA sees the last known status when subscribing
	return integration.mqtt.Publish(scanner.Topics.AvailabilityTopic, status, true)
}

// publishScannerState publishes sensor state for a scanner
func (integration *Integration) publishScannerState(scannerID, state string) error {
	scanner, exists := integration.scanners[scannerID]
	if !exists {
		return fmt.Errorf("scanner %s not found", scannerID)
	}

	return integration.mqtt.Publish(scanner.Topics.StateTopic, state, false)
}

// generateBridgeEntityTopics creates MQTT topics for bridge-level entities
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

// publishBridgeEntityDiscoveryConfig publishes discovery config for bridge-level entities
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
		// Bridge entities depend only on bridge availability
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

// publishScannerSummaryDiscoveryConfig publishes scanner summary discovery configuration
func (integration *Integration) publishScannerSummaryDiscoveryConfig() error {
	return integration.publishBridgeEntityDiscoveryConfig("scanners", "Scanner Summary", "mdi:barcode-scan")
}

// publishDiagnosticsDiscoveryConfig publishes diagnostics discovery configuration
func (integration *Integration) publishDiagnosticsDiscoveryConfig() error {
	return integration.publishBridgeEntityDiscoveryConfig("diagnostics", "Diagnostics", "mdi:stethoscope")
}

// publishScannerSummary publishes scanner summary status
func (integration *Integration) publishScannerSummary() error {
	topics, _ := integration.generateBridgeEntityTopics("scanners")
	status := integration.getScannerSummaryStatus()

	// Publish state (availability handled by bridge availability)
	if err := integration.mqtt.Publish(topics.StateTopic, status, false); err != nil {
		return err
	}

	// Publish attributes
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

// publishDiagnostics publishes diagnostics
func (integration *Integration) publishDiagnostics() error {
	topics, _ := integration.generateBridgeEntityTopics("diagnostics")
	status := integration.getDiagnosticsStatus()

	// Publish state (availability handled by bridge availability)
	if err := integration.mqtt.Publish(topics.StateTopic, status, false); err != nil {
		return err
	}

	// Publish attributes
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

// getScannerList returns a list of scanner IDs for attributes
func (integration *Integration) getScannerList() []string {
	scanners := make([]string, 0, len(integration.scanners))
	for scannerID := range integration.scanners {
		scanners = append(scanners, scannerID)
	}
	return scanners
}

// getConnectedScannerCount returns the number of connected scanners
func (integration *Integration) getConnectedScannerCount() int {
	count := 0
	for _, scanner := range integration.scanners {
		if scanner.Connected {
			count++
		}
	}
	return count
}
