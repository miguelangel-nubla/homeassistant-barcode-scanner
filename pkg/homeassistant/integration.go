package homeassistant

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/karalabe/hid"
	"github.com/sirupsen/logrus"

	"github.com/miguelangel-nubla/homeassistant-barcode-scanner/pkg/config"
	"github.com/miguelangel-nubla/homeassistant-barcode-scanner/pkg/mqtt"
)

const (
	StatusOffline = "offline"
	StatusUnknown = "unknown"
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
	AttributesTopic   string               `json:"json_attributes_topic,omitempty"`
	AvailabilityTopic string               `json:"availability_topic,omitempty"`
	Availability      []AvailabilityConfig `json:"availability,omitempty"`
	AvailabilityMode  string               `json:"availability_mode,omitempty"`
	Device            *DeviceInfo          `json:"device,omitempty"`
	Icon              string               `json:"icon,omitempty"`
	ForceUpdate       bool                 `json:"force_update,omitempty"`
	EntityCategory    string               `json:"entity_category,omitempty"`
}

type Integration struct {
	mqtt             *mqtt.Client
	config           *config.HomeAssistantConfig
	logger           *logrus.Logger
	version          string
	scanners         map[string]*ScannerDevice
	scannerConfigs   map[string]*config.ScannerConfig
	bridgeDeviceInfo *DeviceInfo
	bridgeEntities   *BridgeEntityManager
}

type ScannerHealthMetrics struct {
	LastSeen       time.Time
	ConnectedAt    *time.Time
	DisconnectedAt *time.Time
	ReconnectCount int
	ErrorCount     int
	TotalScans     int
	LastScanTime   *time.Time
}

type ScannerDevice struct {
	ID           string
	Name         string
	Connected    bool
	DeviceInfo   *DeviceInfo
	Topics       *ScannerTopics
	HealthTopics *ScannerTopics
	Health       *ScannerHealthMetrics
}

type ScannerTopics struct {
	ConfigTopic       string
	StateTopic        string
	AvailabilityTopic string
	AttributesTopic   string
}

type BridgeEntity struct {
	EntityType       string
	Name             string
	Icon             string
	GetStatus        func(*Integration) string
	GetAttributes    func(*Integration) map[string]any
	GetShutdownState func(*Integration) string
}

type BridgeEntityManager struct {
	integration *Integration
	entities    []BridgeEntity
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

	integration.bridgeEntities = &BridgeEntityManager{
		integration: integration,
		entities: []BridgeEntity{
			{
				EntityType: "diagnostics",
				Name:       "Diagnostics",
				Icon:       "mdi:stethoscope",
				GetStatus:  (*Integration).getScannerSummaryStatus,
				GetAttributes: func(i *Integration) map[string]any {
					return map[string]any{
						"connected_scanners": i.getConnectedScannerCount(),
						"total_scanners":     len(i.scanners),
						"scanner_list":       i.getScannerList(),
					}
				},
				GetShutdownState: func(i *Integration) string { return StatusOffline },
			},
		},
	}

	return integration
}

func (bem *BridgeEntityManager) publishAllDiscoveryConfigs() error {
	for _, entity := range bem.entities {
		if err := bem.integration.publishBridgeEntityDiscoveryConfig(entity.EntityType, entity.Name, entity.Icon); err != nil {
			bem.integration.logger.WithError(err).Errorf("Failed to publish %s discovery config", entity.Name)
			return err
		}
	}
	return nil
}

func (bem *BridgeEntityManager) publishAllStates() {
	for _, entity := range bem.entities {
		if err := bem.publishEntityState(entity); err != nil {
			bem.integration.logger.WithError(err).Errorf("Failed to update %s", entity.Name)
		}
	}
}

func (bem *BridgeEntityManager) publishEntityState(entity BridgeEntity) error {
	topics, _ := bem.integration.generateBridgeEntityTopics(entity.EntityType)
	status := entity.GetStatus(bem.integration)

	if err := bem.integration.mqtt.Publish(topics.StateTopic, status, false); err != nil {
		return err
	}

	attributes := entity.GetAttributes(bem.integration)
	attributesJSON, err := json.Marshal(attributes)
	if err != nil {
		return fmt.Errorf("failed to marshal %s attributes: %w", entity.Name, err)
	}

	return bem.integration.mqtt.Publish(topics.AttributesTopic, string(attributesJSON), false)
}

func (bem *BridgeEntityManager) publishOfflineStates() {
	for _, entity := range bem.entities {
		topics, _ := bem.integration.generateBridgeEntityTopics(entity.EntityType)
		shutdownState := entity.GetShutdownState(bem.integration)
		if err := bem.integration.mqtt.Publish(topics.StateTopic, shutdownState, false); err != nil {
			bem.integration.logger.WithError(err).Errorf("Failed to publish %s shutdown state", entity.Name)
		}
	}
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
			if err := integration.publishScannerState(scannerID, StatusUnknown); err != nil {
				integration.logger.WithField("scanner_id", scannerID).WithError(err).Error("Failed to publish unknown state")
			}
		}

		if err := integration.publishBridgeAvailability("offline"); err != nil {
			integration.logger.WithError(err).Error("Failed to publish bridge offline status")
		}

		integration.bridgeEntities.publishOfflineStates()
	}

	return nil
}

func (integration *Integration) AddScanner(scannerID, scannerName string, scannerConfig *config.ScannerConfig) {
	integration.logger.Debugf("Registering scanner configuration: %s", scannerID)

	integration.scannerConfigs[scannerID] = scannerConfig
	integration.logger.Debugf("Stored config for scanner %s, will create HA device when hardware connects", scannerID)
}

func (integration *Integration) RemoveScanner(scannerID string) {
	integration.logger.Debugf("Removing scanner from Home Assistant integration: %s", scannerID)

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

	now := time.Now()
	scanner := &ScannerDevice{
		ID:           scannerID,
		Name:         displayName,
		Connected:    false,
		Topics:       integration.generateScannerTopics(scannerID),
		HealthTopics: integration.generateScannerHealthTopics(scannerID),
		DeviceInfo: &DeviceInfo{
			Identifiers:  []string{scannerDeviceID},
			Name:         displayName,
			Model:        strings.TrimSpace(deviceInfo.Product),
			Manufacturer: strings.TrimSpace(deviceInfo.Manufacturer),
			ViaDevice:    bridgeID,
		},
		Health: &ScannerHealthMetrics{
			LastSeen:       now,
			ReconnectCount: 0,
			ErrorCount:     0,
			TotalScans:     0,
		},
	}

	integration.scanners[scannerID] = scanner

	integration.logger.Infof("Created HA device for scanner %s: %s %s (VID:PID %04x:%04x)",
		scannerID, deviceInfo.Manufacturer, deviceInfo.Product, deviceInfo.VendorID, deviceInfo.ProductID)

	if integration.mqtt.IsConnected() {
		if err := integration.publishScannerAvailability(scannerID, "offline"); err != nil {
			integration.logger.Errorf("Failed to publish initial availability for scanner %s: %v", scannerID, err)
		}

		if err := integration.publishScannerDiscoveryConfig(scannerID); err != nil {
			integration.logger.Errorf("Failed to publish discovery config for scanner %s: %v", scannerID, err)
		}
		if err := integration.publishScannerHealthDiscoveryConfig(scannerID); err != nil {
			integration.logger.Errorf("Failed to publish health discovery config for scanner %s: %v", scannerID, err)
		}
	}
}

func (integration *Integration) SetScannerConnected(scannerID string, connected bool) error {
	scanner, exists := integration.scanners[scannerID]
	if !exists {
		return fmt.Errorf("scanner %s not found", scannerID)
	}

	now := time.Now()
	scanner.Health.LastSeen = now
	if connected && !scanner.Connected {
		scanner.Health.ConnectedAt = &now
		if scanner.Health.DisconnectedAt != nil {
			scanner.Health.ReconnectCount++
		}
		scanner.Health.DisconnectedAt = nil
	} else if !connected && scanner.Connected {
		scanner.Health.DisconnectedAt = &now
		scanner.Health.ConnectedAt = nil
	}

	scanner.Connected = connected

	if err := integration.publishScannerState(scannerID, StatusUnknown); err != nil {
		return err
	}
	if err := integration.publishScannerAttributes(scannerID); err != nil {
		return err
	}

	var availabilityStatus string
	if connected {
		availabilityStatus = "online"
	} else {
		availabilityStatus = "offline"
	}

	if err := integration.publishScannerAvailability(scannerID, availabilityStatus); err != nil {
		return err
	}

	if err := integration.publishScannerHealthState(scannerID); err != nil {
		integration.logger.WithError(err).Errorf("Failed to publish health state for scanner %s", scannerID)
	}

	integration.bridgeEntities.publishAllStates()
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

	now := time.Now()
	scanner.Health.LastSeen = now
	scanner.Health.LastScanTime = &now
	scanner.Health.TotalScans++

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

	if err := integration.publishScannerHealthState(scannerID); err != nil {
		integration.logger.WithError(err).Errorf("Failed to update health state after scan for scanner %s", scannerID)
	}

	return nil
}

func (integration *Integration) generateBridgeDeviceID() string {
	if integration.config.InstanceID != "" {
		return fmt.Sprintf("ha-barcode-bridge-%s", integration.config.InstanceID)
	}
	hostname, err := os.Hostname()
	if err != nil {
		hostname = StatusUnknown
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
		hostname = StatusUnknown
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

func (integration *Integration) generateScannerHealthTopics(scannerID string) *ScannerTopics {
	bridgeID := integration.generateBridgeDeviceID()
	entityID := fmt.Sprintf("%s-scanner-%s-health", bridgeID, scannerID)

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

func (integration *Integration) handleConnect() {
	integration.logger.Info("MQTT connected, publishing bridge availability and discovery configs")

	if err := integration.bridgeEntities.publishAllDiscoveryConfigs(); err != nil {
		integration.logger.WithError(err).Error("Failed to publish bridge entity discovery configs")
	}

	for scannerID := range integration.scanners {
		if err := integration.publishScannerDiscoveryConfig(scannerID); err != nil {
			integration.logger.WithField("scanner_id", scannerID).WithError(err).Error("Failed to publish discovery config")
		}
		if err := integration.publishScannerHealthDiscoveryConfig(scannerID); err != nil {
			integration.logger.WithField("scanner_id", scannerID).WithError(err).Error("Failed to publish health discovery config")
		}
	}

	if err := integration.publishBridgeAvailability("online"); err != nil {
		integration.logger.WithError(err).Error("Failed to publish bridge availability")
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
		Name:            sensorName,
		ObjectID:        fmt.Sprintf("%s_%s", integration.config.InstanceID, scannerID),
		UniqueID:        fmt.Sprintf("%s-scanner-%s", bridgeID, scannerID),
		TildeTopic:      baseTopic,
		StateTopic:      "~/state",
		AttributesTopic: "~/attributes",
		Availability: []AvailabilityConfig{
			{
				Topic: "~/availability",
			},
			{
				Topic: integration.GenerateBridgeAvailabilityTopic(),
			},
		},
		AvailabilityMode: "all",
		Device:           scanner.DeviceInfo,
		Icon:             "mdi:barcode-scan",
		ForceUpdate:      true,
	}

	configJSON, err := json.Marshal(sensorConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal discovery config: %w", err)
	}

	return integration.mqtt.Publish(scanner.Topics.ConfigTopic, string(configJSON), true)
}

func (integration *Integration) publishScannerHealthDiscoveryConfig(scannerID string) error {
	scanner, exists := integration.scanners[scannerID]
	if !exists || scanner.DeviceInfo == nil {
		return fmt.Errorf("scanner %s not found or device info not set", scannerID)
	}

	bridgeID := integration.generateBridgeDeviceID()
	healthName := fmt.Sprintf("%s Health", scanner.Name)
	baseTopic := fmt.Sprintf("%s/sensor/%s-scanner-%s-health", integration.config.DiscoveryPrefix, bridgeID, scannerID)

	sensorConfig := SensorConfig{
		Name:            healthName,
		ObjectID:        fmt.Sprintf("%s_%s_health", integration.config.InstanceID, scannerID),
		UniqueID:        fmt.Sprintf("%s-scanner-%s-health", bridgeID, scannerID),
		TildeTopic:      baseTopic,
		StateTopic:      "~/state",
		AttributesTopic: "~/attributes",
		Availability: []AvailabilityConfig{
			{
				Topic: integration.GenerateBridgeAvailabilityTopic(),
			},
		},
		Device:         scanner.DeviceInfo,
		Icon:           "mdi:heart-pulse",
		ForceUpdate:    false,
		EntityCategory: "diagnostic",
	}

	configJSON, err := json.Marshal(sensorConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal health discovery config: %w", err)
	}

	return integration.mqtt.Publish(scanner.HealthTopics.ConfigTopic, string(configJSON), true)
}

func (integration *Integration) publishBridgeAvailability(status string) error {
	topic := integration.GenerateBridgeAvailabilityTopic()
	return integration.mqtt.Publish(topic, status, true)
}

func (integration *Integration) publishScannerAvailability(scannerID, status string) error {
	scanner, exists := integration.scanners[scannerID]
	if !exists {
		return fmt.Errorf("scanner %s not found", scannerID)
	}

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

func (integration *Integration) publishScannerHealthState(scannerID string) error {
	scanner, exists := integration.scanners[scannerID]
	if !exists {
		return fmt.Errorf("scanner %s not found", scannerID)
	}

	healthStatus := integration.getScannerHealthStatus(scannerID)
	if err := integration.mqtt.Publish(scanner.HealthTopics.StateTopic, healthStatus, false); err != nil {
		return err
	}

	attributes := integration.getScannerHealthAttributes(scannerID)
	attributesJSON, err := json.Marshal(attributes)
	if err != nil {
		return fmt.Errorf("failed to marshal health attributes: %w", err)
	}

	return integration.mqtt.Publish(scanner.HealthTopics.AttributesTopic, string(attributesJSON), false)
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
		Name:            name,
		UniqueID:        entityID,
		TildeTopic:      baseTopic,
		StateTopic:      "~/state",
		AttributesTopic: "~/attributes",
		Availability: []AvailabilityConfig{
			{
				Topic: integration.GenerateBridgeAvailabilityTopic(),
			},
		},
		Device:         integration.bridgeDeviceInfo,
		Icon:           icon,
		ForceUpdate:    false,
		EntityCategory: "diagnostic",
	}

	configJSON, err := json.Marshal(sensorConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal %s discovery config: %w", entityType, err)
	}

	return integration.mqtt.Publish(topics.ConfigTopic, string(configJSON), true)
}

func (integration *Integration) getScannerList() []string {
	scanners := make([]string, 0, len(integration.scanners))
	for scannerID := range integration.scanners {
		scanners = append(scanners, scannerID)
	}
	return scanners
}

func (integration *Integration) getScannerHealthStatus(scannerID string) string {
	scanner, exists := integration.scanners[scannerID]
	if !exists || scanner.Health == nil {
		return StatusUnknown
	}

	if !scanner.Connected {
		if time.Since(scanner.Health.LastSeen) > 5*time.Minute {
			return "stale"
		}
		return "disconnected"
	}

	if scanner.Health.ErrorCount > 10 {
		return "degraded"
	}

	if scanner.Health.ReconnectCount > 5 {
		return "unstable"
	}

	return "healthy"
}

func (integration *Integration) getScannerHealthAttributes(scannerID string) map[string]any {
	scanner, exists := integration.scanners[scannerID]
	if !exists || scanner.Health == nil {
		return map[string]any{}
	}

	attributes := map[string]any{
		"last_seen":       scanner.Health.LastSeen.Format(time.RFC3339),
		"reconnect_count": scanner.Health.ReconnectCount,
		"error_count":     scanner.Health.ErrorCount,
		"total_scans":     scanner.Health.TotalScans,
	}

	if scanner.Health.ConnectedAt != nil {
		attributes["connected_at"] = scanner.Health.ConnectedAt.Format(time.RFC3339)
	}

	if scanner.Health.DisconnectedAt != nil {
		attributes["disconnected_at"] = scanner.Health.DisconnectedAt.Format(time.RFC3339)
	}

	return attributes
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
