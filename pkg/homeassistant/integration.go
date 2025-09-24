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

// NewIntegration creates a new multi-scanner Home Assistant integration
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
func (mi *Integration) Start() error {
	mi.logger.Info("Starting Home Assistant multi-scanner integration")

	// Set up MQTT callbacks
	mi.mqtt.SetOnConnectCallback(mi.handleConnect)
	mi.mqtt.SetOnDisconnectCallback(mi.handleDisconnect)

	// If already connected, perform initial setup
	if mi.mqtt.IsConnected() {
		mi.handleConnect()
	}

	return nil
}

// Stop shuts down the Home Assistant integration
func (mi *Integration) Stop() error {
	mi.logger.Info("Stopping Home Assistant multi-scanner integration")

	// Publish offline status for all scanners and bridge
	if mi.mqtt.IsConnected() {
		for scannerID := range mi.scanners {
			mi.logger.Debugf("Publishing offline status for scanner: %s", scannerID)
			if err := mi.publishScannerAvailability(scannerID, "offline"); err != nil {
				mi.logger.Errorf("Failed to publish offline status for scanner %s: %v", scannerID, err)
			}
		}

		// Publish offline status for bridge entities
		// Note: We don't publish offline status for bridge entities since availability
		// is handled by Home Assistant when the bridge goes offline
	}

	return nil
}

// AddScanner registers a new scanner configuration
func (mi *Integration) AddScanner(scannerID, scannerName string, scannerConfig *config.ScannerConfig) {
	mi.logger.Infof("Registering scanner configuration: %s", scannerID)

	// Store scanner configuration for later use when hardware connects
	mi.scannerConfigs[scannerID] = scannerConfig
	mi.logger.Debugf("Stored config for scanner %s, will create HA device when hardware connects", scannerID)
}

// RemoveScanner removes a scanner from Home Assistant integration
func (mi *Integration) RemoveScanner(scannerID string) {
	mi.logger.Infof("Removing scanner from Home Assistant integration: %s", scannerID)

	if mi.mqtt.IsConnected() {
		// Publish offline status before removing
		scanner := mi.scanners[scannerID]
		if scanner != nil {
			if err := mi.publishScannerAvailability(scannerID, "offline"); err != nil {
				mi.logger.Errorf("Failed to publish offline status for removed scanner %s: %v", scannerID, err)
			}
		}
	}

	delete(mi.scanners, scannerID)
	delete(mi.scannerConfigs, scannerID)
}

// SetScannerDeviceInfo creates Home Assistant device when hardware scanner connects
func (mi *Integration) SetScannerDeviceInfo(scannerID string, deviceInfo *hid.DeviceInfo) {
	// Verify scanner config exists (must exist from AddScanner)
	if _, exists := mi.scannerConfigs[scannerID]; !exists {
		mi.logger.Errorf("Scanner config %s not found, cannot create HA device", scannerID)
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
	bridgeID := mi.generateBridgeDeviceID()
	scannerDeviceID := mi.generateScannerDeviceID(scannerID)

	scanner := &ScannerDevice{
		ID:        scannerID,
		Name:      displayName,
		Connected: false, // Will be set by SetScannerConnected
		Topics:    mi.generateScannerTopics(scannerID),
		DeviceInfo: &DeviceInfo{
			Identifiers:  []string{scannerDeviceID},
			Name:         displayName,
			Model:        strings.TrimSpace(deviceInfo.Product),
			Manufacturer: strings.TrimSpace(deviceInfo.Manufacturer),
			ViaDevice:    bridgeID,
		},
	}

	mi.scanners[scannerID] = scanner

	mi.logger.Infof("Created HA device for scanner %s: %s %s (VID:PID %04x:%04x)",
		scannerID, deviceInfo.Manufacturer, deviceInfo.Product, deviceInfo.VendorID, deviceInfo.ProductID)

	// Publish availability first (required for HA auto-discovery timing)
	if mi.mqtt.IsConnected() {
		// Publish offline initially - will be updated by SetScannerConnected when actually connected
		if err := mi.publishScannerAvailability(scannerID, "offline"); err != nil {
			mi.logger.Errorf("Failed to publish initial availability for scanner %s: %v", scannerID, err)
		}

		// Now publish discovery config after availability is set
		if err := mi.publishScannerDiscoveryConfig(scannerID); err != nil {
			mi.logger.Errorf("Failed to publish discovery config for scanner %s: %v", scannerID, err)
		}
	}
}

// SetScannerConnected updates scanner connection state
func (mi *Integration) SetScannerConnected(scannerID string, connected bool) error {
	scanner, exists := mi.scanners[scannerID]
	if !exists {
		return fmt.Errorf("scanner %s not found", scannerID)
	}

	// Topics are always available since they're created in AddScanner

	scanner.Connected = connected

	if connected {
		mi.logger.Debugf("Scanner %s connected, publishing availability 'online' and state 'unknown'", scannerID)
		if err := mi.publishScannerAvailability(scannerID, "online"); err != nil {
			return err
		}
		if err := mi.publishScannerState(scannerID, "unknown"); err != nil {
			return err
		}
	} else {
		mi.logger.Debugf("Scanner %s disconnected, publishing 'offline' availability", scannerID)
		if err := mi.publishScannerAvailability(scannerID, "offline"); err != nil {
			return err
		}
	}

	// Update bridge entities
	if err := mi.publishScannerSummary(); err != nil {
		mi.logger.Errorf("Failed to update scanner summary: %v", err)
	}
	if err := mi.publishDiagnostics(); err != nil {
		mi.logger.Errorf("Failed to update diagnostics: %v", err)
	}
	return nil
}

// PublishBarcode publishes a scanned barcode for a specific scanner
func (mi *Integration) PublishBarcode(scannerID, barcode string) error {
	scanner, exists := mi.scanners[scannerID]
	if !exists {
		return fmt.Errorf("scanner %s not found", scannerID)
	}

	// Topics are always available

	if !mi.mqtt.IsConnected() {
		return fmt.Errorf("MQTT not connected")
	}

	// Publish barcode as sensor state
	if err := mi.publishScannerState(scannerID, barcode); err != nil {
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

	if err := mi.mqtt.Publish(scanner.Topics.AttributesTopic, string(attributesJSON), false); err != nil {
		return fmt.Errorf("failed to publish attributes: %w", err)
	}

	mi.logger.Debug("Barcode published successfully")
	return nil
}

// generateBridgeDeviceID creates a unique bridge device identifier
func (mi *Integration) generateBridgeDeviceID() string {
	if mi.config.InstanceID != "" {
		return fmt.Sprintf("ha-barcode-bridge-%s", mi.config.InstanceID)
	}
	// Use hostname as fallback for multi-machine support
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}
	return fmt.Sprintf("ha-barcode-bridge-%s", hostname)
}

// GenerateBridgeAvailabilityTopic creates the bridge availability topic for MQTT will/testament
func (mi *Integration) GenerateBridgeAvailabilityTopic() string {
	bridgeID := mi.generateBridgeDeviceID()
	return fmt.Sprintf("%s/sensor/%s/availability", mi.config.DiscoveryPrefix, bridgeID)
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
	// Use hostname as fallback for multi-machine support
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}
	return fmt.Sprintf("ha-barcode-bridge-%s", hostname)
}

// generateScannerDeviceID creates a unique scanner device identifier based on configured ID
func (mi *Integration) generateScannerDeviceID(scannerID string) string {
	bridgeID := mi.generateBridgeDeviceID()
	return fmt.Sprintf("%s-scanner-%s", bridgeID, scannerID)
}

// generateScannerTopics creates MQTT topics for a specific scanner based on configured ID
func (mi *Integration) generateScannerTopics(scannerID string) *ScannerTopics {
	bridgeID := mi.generateBridgeDeviceID()
	entityID := fmt.Sprintf("%s-scanner-%s", bridgeID, scannerID)

	return &ScannerTopics{
		ConfigTopic:       fmt.Sprintf("%s/sensor/%s/config", mi.config.DiscoveryPrefix, entityID),
		StateTopic:        fmt.Sprintf("%s/sensor/%s/state", mi.config.DiscoveryPrefix, entityID),
		AvailabilityTopic: fmt.Sprintf("%s/sensor/%s/availability", mi.config.DiscoveryPrefix, entityID),
		AttributesTopic:   fmt.Sprintf("%s/sensor/%s/attributes", mi.config.DiscoveryPrefix, entityID),
	}
}

// getScannerSummaryStatus returns scanner overview status
func (mi *Integration) getScannerSummaryStatus() string {
	connectedCount := mi.getConnectedScannerCount()
	totalCount := len(mi.scanners)

	if totalCount == 0 {
		return "no_scanners"
	}
	if connectedCount == totalCount {
		return "ready"
	}
	return "partial"
}

// getDiagnosticsStatus returns bridge health status
func (mi *Integration) getDiagnosticsStatus() string {
	// For now, simple logic - can be expanded later
	if mi.mqtt.IsConnected() {
		return "healthy"
	}
	return "error"
}

// handleConnect is called when MQTT connects
func (mi *Integration) handleConnect() {
	mi.logger.Info("MQTT connected, publishing bridge availability and discovery configs")

	// Publish bridge availability first
	if err := mi.publishBridgeAvailability("online"); err != nil {
		mi.logger.Errorf("Failed to publish bridge availability: %v", err)
	}

	// Publish bridge entities availability and states
	if err := mi.publishScannerSummary(); err != nil {
		mi.logger.Errorf("Failed to update scanner summary: %v", err)
	}
	if err := mi.publishDiagnostics(); err != nil {
		mi.logger.Errorf("Failed to update diagnostics: %v", err)
	}

	// Now publish bridge discovery configs after availability is set
	if err := mi.publishScannerSummaryDiscoveryConfig(); err != nil {
		mi.logger.Errorf("Failed to publish scanner summary discovery config: %v", err)
	}
	if err := mi.publishDiagnosticsDiscoveryConfig(); err != nil {
		mi.logger.Errorf("Failed to publish diagnostics discovery config: %v", err)
	}

	// Publish discovery config for all scanners that exist (only connected hardware)
	// Note: availability for individual scanners is already handled by SetScannerDeviceInfo/SetScannerConnected
	for scannerID := range mi.scanners {
		if err := mi.publishScannerDiscoveryConfig(scannerID); err != nil {
			mi.logger.Errorf("Failed to publish discovery config for scanner %s: %v", scannerID, err)
		}
	}
}

// handleDisconnect is called when MQTT disconnects
func (mi *Integration) handleDisconnect() {
	mi.logger.Warn("MQTT disconnected")
}

// publishScannerDiscoveryConfig publishes discovery configuration for a scanner
func (mi *Integration) publishScannerDiscoveryConfig(scannerID string) error {
	scanner, exists := mi.scanners[scannerID]
	if !exists || scanner.DeviceInfo == nil {
		return fmt.Errorf("scanner %s not found or device info not set", scannerID)
	}

	bridgeID := mi.generateBridgeDeviceID()

	// Generate sensor name: use scanner friendly name if provided, otherwise use scanner ID
	sensorName := scanner.Name
	if sensorName == "" {
		sensorName = scannerID
	}

	// Extract base topic for tilde optimization
	baseTopic := fmt.Sprintf("%s/sensor/%s-scanner-%s", mi.config.DiscoveryPrefix, bridgeID, scannerID)

	sensorConfig := SensorConfig{
		Name:             sensorName,
		ObjectID:         scannerID,
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
				Topic: mi.GenerateBridgeAvailabilityTopic(),
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

	mi.logger.Debugf("Publishing scanner discovery config to: %s", scanner.Topics.ConfigTopic)
	// Use retain=true for discovery configs so they persist for HA restarts
	return mi.mqtt.Publish(scanner.Topics.ConfigTopic, string(configJSON), true)
}

// publishBridgeAvailability publishes bridge availability status
func (mi *Integration) publishBridgeAvailability(status string) error {
	topic := mi.GenerateBridgeAvailabilityTopic()
	mi.logger.Debugf("Publishing bridge availability: %s", status)
	// Use retain=true for bridge availability messages so HA sees the last known status
	return mi.mqtt.Publish(topic, status, true)
}

// publishScannerAvailability publishes availability status for a scanner
func (mi *Integration) publishScannerAvailability(scannerID, status string) error {
	scanner, exists := mi.scanners[scannerID]
	if !exists {
		return fmt.Errorf("scanner %s not found", scannerID)
	}

	mi.logger.Debugf("Publishing availability status for scanner %s: %s", scannerID, status)
	// Use retain=true for availability messages so HA sees the last known status when subscribing
	return mi.mqtt.Publish(scanner.Topics.AvailabilityTopic, status, true)
}

// publishScannerState publishes sensor state for a scanner
func (mi *Integration) publishScannerState(scannerID, state string) error {
	scanner, exists := mi.scanners[scannerID]
	if !exists {
		return fmt.Errorf("scanner %s not found", scannerID)
	}

	mi.logger.Debugf("Publishing sensor state for scanner %s: %s", scannerID, state)
	return mi.mqtt.Publish(scanner.Topics.StateTopic, state, false)
}

// publishBridgeEntityDiscoveryConfig publishes discovery config for bridge-level entities
func (mi *Integration) publishBridgeEntityDiscoveryConfig(entityType, name, icon string) error {
	bridgeID := mi.generateBridgeDeviceID()
	entityID := fmt.Sprintf("%s-%s", bridgeID, entityType)

	topics := &ScannerTopics{
		ConfigTopic:       fmt.Sprintf("%s/sensor/%s/config", mi.config.DiscoveryPrefix, entityID),
		StateTopic:        fmt.Sprintf("%s/sensor/%s/state", mi.config.DiscoveryPrefix, entityID),
		AvailabilityTopic: fmt.Sprintf("%s/sensor/%s/availability", mi.config.DiscoveryPrefix, entityID),
		AttributesTopic:   fmt.Sprintf("%s/sensor/%s/attributes", mi.config.DiscoveryPrefix, entityID),
	}

	// Extract base topic for tilde optimization
	baseTopic := fmt.Sprintf("%s/sensor/%s", mi.config.DiscoveryPrefix, entityID)

	sensorConfig := SensorConfig{
		Name:             name,
		UniqueID:         entityID,
		TildeTopic:       baseTopic,
		StateTopic:       "~/state",
		AttributesToopic: "~/attributes",
		// Bridge entities depend only on bridge availability
		Availability: []AvailabilityConfig{
			{
				Topic: mi.GenerateBridgeAvailabilityTopic(),
			},
		},
		Device:      mi.bridgeDeviceInfo,
		Icon:        icon,
		ForceUpdate: false,
	}

	configJSON, err := json.Marshal(sensorConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal %s discovery config: %w", entityType, err)
	}

	mi.logger.Debugf("Publishing %s discovery config to: %s", entityType, topics.ConfigTopic)
	// Use retain=true for discovery configs so they persist for HA restarts
	return mi.mqtt.Publish(topics.ConfigTopic, string(configJSON), true)
}

// publishScannerSummaryDiscoveryConfig publishes scanner summary discovery configuration
func (mi *Integration) publishScannerSummaryDiscoveryConfig() error {
	return mi.publishBridgeEntityDiscoveryConfig("scanners", "Scanner Summary", "mdi:barcode-scan")
}

// publishDiagnosticsDiscoveryConfig publishes diagnostics discovery configuration
func (mi *Integration) publishDiagnosticsDiscoveryConfig() error {
	return mi.publishBridgeEntityDiscoveryConfig("diagnostics", "Diagnostics", "mdi:stethoscope")
}

// publishScannerSummary publishes scanner summary status
func (mi *Integration) publishScannerSummary() error {
	bridgeID := mi.generateBridgeDeviceID()
	entityID := fmt.Sprintf("%s-scanners", bridgeID)
	stateTopic := fmt.Sprintf("%s/sensor/%s/state", mi.config.DiscoveryPrefix, entityID)
	attributesTopic := fmt.Sprintf("%s/sensor/%s/attributes", mi.config.DiscoveryPrefix, entityID)

	status := mi.getScannerSummaryStatus()

	// Publish state (availability handled by bridge availability)
	mi.logger.Debugf("Publishing scanner summary: %s", status)
	if err := mi.mqtt.Publish(stateTopic, status, false); err != nil {
		return err
	}

	// Publish attributes
	attributes := map[string]any{
		"connected_scanners": mi.getConnectedScannerCount(),
		"total_scanners":     len(mi.scanners),
		"scanner_list":       mi.getScannerList(),
	}

	attributesJSON, err := json.Marshal(attributes)
	if err != nil {
		return fmt.Errorf("failed to marshal scanner summary attributes: %w", err)
	}

	return mi.mqtt.Publish(attributesTopic, string(attributesJSON), false)
}

// publishDiagnostics publishes diagnostics
func (mi *Integration) publishDiagnostics() error {
	bridgeID := mi.generateBridgeDeviceID()
	entityID := fmt.Sprintf("%s-diagnostics", bridgeID)
	stateTopic := fmt.Sprintf("%s/sensor/%s/state", mi.config.DiscoveryPrefix, entityID)
	attributesTopic := fmt.Sprintf("%s/sensor/%s/attributes", mi.config.DiscoveryPrefix, entityID)

	status := mi.getDiagnosticsStatus()

	// Publish state (availability handled by bridge availability)
	mi.logger.Debugf("Publishing diagnostics: %s", status)
	if err := mi.mqtt.Publish(stateTopic, status, false); err != nil {
		return err
	}

	// Publish attributes
	attributes := map[string]any{
		"version":      mi.version,
		"config_count": len(mi.scannerConfigs),
	}

	attributesJSON, err := json.Marshal(attributes)
	if err != nil {
		return fmt.Errorf("failed to marshal diagnostics attributes: %w", err)
	}

	return mi.mqtt.Publish(attributesTopic, string(attributesJSON), false)
}

// getScannerList returns a list of scanner IDs for attributes
func (mi *Integration) getScannerList() []string {
	scanners := make([]string, 0, len(mi.scanners))
	for scannerID := range mi.scanners {
		scanners = append(scanners, scannerID)
	}
	return scanners
}

// getConnectedScannerCount returns the number of connected scanners
func (mi *Integration) getConnectedScannerCount() int {
	count := 0
	for _, scanner := range mi.scanners {
		if scanner.Connected {
			count++
		}
	}
	return count
}
