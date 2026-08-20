package homeassistant

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/karalabe/hid"
	"github.com/sirupsen/logrus"

	"github.com/miguelangel-nubla/homeassistant-barcode-scanner/pkg/config"
)

// fakePublisher records published MQTT messages in place of a real broker
// connection.
type fakePublisher struct {
	mu           sync.Mutex
	connected    bool
	messages     []fakeMessage
	onConnect    func()
	onDisconnect func()
}

type fakeMessage struct {
	Topic   string
	Payload string
	Retain  bool
}

func newFakePublisher() *fakePublisher {
	return &fakePublisher{connected: true}
}

func (f *fakePublisher) Publish(topic, payload string, retain bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.connected {
		return fmt.Errorf("not connected")
	}
	f.messages = append(f.messages, fakeMessage{topic, payload, retain})
	return nil
}

func (f *fakePublisher) IsConnected() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.connected
}

func (f *fakePublisher) SetOnConnectCallback(callback func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.onConnect = callback
}

func (f *fakePublisher) SetOnDisconnectCallback(callback func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.onDisconnect = callback
}

// lastPayload returns the most recent payload published to a topic.
func (f *fakePublisher) lastPayload(topic string) (fakeMessage, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.messages) - 1; i >= 0; i-- {
		if f.messages[i].Topic == topic {
			return f.messages[i], true
		}
	}
	return fakeMessage{}, false
}

func (f *fakePublisher) payloadsFor(topic string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var payloads []string
	for _, msg := range f.messages {
		if msg.Topic == topic {
			payloads = append(payloads, msg.Payload)
		}
	}
	return payloads
}

func testLogger() *logrus.Logger {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	return logger
}

func testHAConfig() *config.HomeAssistantConfig {
	return &config.HomeAssistantConfig{
		DiscoveryPrefix: "homeassistant",
		InstanceID:      "testhost",
	}
}

func testDeviceInfo() *hid.DeviceInfo {
	return &hid.DeviceInfo{
		VendorID:     0x60e,
		ProductID:    0x16c7,
		Manufacturer: "ACME",
		Product:      "Scanner 3000",
		Serial:       "SN123",
	}
}

// newTestIntegration returns an integration with one registered and
// hardware-detected scanner named "front_desk".
func newTestIntegration(t *testing.T) (*Integration, *fakePublisher) {
	t.Helper()

	publisher := newFakePublisher()
	integration := NewIntegration(publisher, testHAConfig(), "1.2.3", testLogger())

	scannerCfg := &config.ScannerConfig{
		ID:              "front_desk",
		Name:            "Front Desk",
		KeyboardLayout:  "us",
		TerminationChar: "enter",
	}
	integration.AddScanner("front_desk", "Front Desk", scannerCfg)
	integration.SetScannerDeviceInfo("front_desk", testDeviceInfo())

	return integration, publisher
}

const statusOnline = "online"

const (
	scannerStateTopic        = "homeassistant/sensor/ha-barcode-bridge-testhost-scanner-front_desk/state"
	scannerConfigTopic       = "homeassistant/sensor/ha-barcode-bridge-testhost-scanner-front_desk/config"
	scannerAvailabilityTopic = "homeassistant/sensor/ha-barcode-bridge-testhost-scanner-front_desk/availability"
	scannerAttributesTopic   = "homeassistant/sensor/ha-barcode-bridge-testhost-scanner-front_desk/attributes"
	healthStateTopic         = "homeassistant/sensor/ha-barcode-bridge-testhost-scanner-front_desk-health/state"
	healthAttributesTopic    = "homeassistant/sensor/ha-barcode-bridge-testhost-scanner-front_desk-health/attributes"
	bridgeAvailabilityTopic  = "homeassistant/sensor/ha-barcode-bridge-testhost/availability"
)

func TestSetScannerDeviceInfo_PublishesDiscoveryConfig(t *testing.T) {
	_, publisher := newTestIntegration(t)

	msg, ok := publisher.lastPayload(scannerConfigTopic)
	if !ok {
		t.Fatal("expected discovery config to be published")
	}
	if !msg.Retain {
		t.Error("expected discovery config to be retained")
	}

	var sensorConfig SensorConfig
	if err := json.Unmarshal([]byte(msg.Payload), &sensorConfig); err != nil {
		t.Fatalf("discovery config is not valid JSON: %v", err)
	}

	if sensorConfig.Name != "ACME Scanner 3000" {
		t.Errorf("expected sensor name 'ACME Scanner 3000', got %q", sensorConfig.Name)
	}
	if sensorConfig.UniqueID != "ha-barcode-bridge-testhost-scanner-front_desk" {
		t.Errorf("unexpected unique_id %q", sensorConfig.UniqueID)
	}
	if sensorConfig.StateTopic != "~/state" {
		t.Errorf("expected state topic '~/state', got %q", sensorConfig.StateTopic)
	}
	if sensorConfig.AvailabilityMode != "all" {
		t.Errorf("expected availability mode 'all', got %q", sensorConfig.AvailabilityMode)
	}
	if len(sensorConfig.Availability) != 2 {
		t.Fatalf("expected 2 availability topics (scanner + bridge), got %d", len(sensorConfig.Availability))
	}
	if sensorConfig.Device == nil || sensorConfig.Device.ViaDevice != "ha-barcode-bridge-testhost" {
		t.Error("expected device to reference the bridge via via_device")
	}

	// The scanner starts offline until the connection event arrives.
	availability, ok := publisher.lastPayload(scannerAvailabilityTopic)
	if !ok || availability.Payload != StatusOffline {
		t.Errorf("expected initial availability 'offline', got %+v", availability)
	}
}

func TestSetScannerDeviceInfo_PublishesStaticAttributes(t *testing.T) {
	_, publisher := newTestIntegration(t)

	msg, ok := publisher.lastPayload(scannerAttributesTopic)
	if !ok {
		t.Fatal("expected scanner attributes to be published")
	}

	var attributes map[string]any
	if err := json.Unmarshal([]byte(msg.Payload), &attributes); err != nil {
		t.Fatalf("attributes are not valid JSON: %v", err)
	}
	if attributes["scanner_id"] != "front_desk" {
		t.Errorf("expected scanner_id 'front_desk', got %v", attributes["scanner_id"])
	}
	if attributes["keyboard_layout"] != "us" {
		t.Errorf("expected keyboard_layout 'us', got %v", attributes["keyboard_layout"])
	}
	if attributes["termination_char"] != "enter" {
		t.Errorf("expected termination_char 'enter', got %v", attributes["termination_char"])
	}
}

func TestSetScannerDeviceInfo_UnknownScannerIsIgnored(t *testing.T) {
	publisher := newFakePublisher()
	integration := NewIntegration(publisher, testHAConfig(), "1.2.3", testLogger())

	integration.SetScannerDeviceInfo("ghost", testDeviceInfo())

	if len(publisher.messages) != 0 {
		t.Errorf("expected no messages for unregistered scanner, got %d", len(publisher.messages))
	}
}

func TestSetScannerConnected_PublishesAvailability(t *testing.T) {
	integration, publisher := newTestIntegration(t)

	if err := integration.SetScannerConnected("front_desk", true); err != nil {
		t.Fatalf("SetScannerConnected(true) error: %v", err)
	}
	msg, _ := publisher.lastPayload(scannerAvailabilityTopic)
	if msg.Payload != statusOnline {
		t.Errorf("expected availability 'online', got %q", msg.Payload)
	}

	health, _ := publisher.lastPayload(healthStateTopic)
	if health.Payload != "healthy" {
		t.Errorf("expected health 'healthy', got %q", health.Payload)
	}

	if err := integration.SetScannerConnected("front_desk", false); err != nil {
		t.Fatalf("SetScannerConnected(false) error: %v", err)
	}
	msg, _ = publisher.lastPayload(scannerAvailabilityTopic)
	if msg.Payload != StatusOffline {
		t.Errorf("expected availability 'offline', got %q", msg.Payload)
	}

	health, _ = publisher.lastPayload(healthStateTopic)
	if health.Payload != "disconnected" {
		t.Errorf("expected health 'disconnected', got %q", health.Payload)
	}
}

func TestSetScannerConnected_TracksReconnects(t *testing.T) {
	integration, publisher := newTestIntegration(t)

	for _, connected := range []bool{true, false, true} {
		if err := integration.SetScannerConnected("front_desk", connected); err != nil {
			t.Fatalf("SetScannerConnected(%v) error: %v", connected, err)
		}
	}

	msg, ok := publisher.lastPayload(healthAttributesTopic)
	if !ok {
		t.Fatal("expected health attributes to be published")
	}

	var attributes map[string]any
	if err := json.Unmarshal([]byte(msg.Payload), &attributes); err != nil {
		t.Fatalf("health attributes are not valid JSON: %v", err)
	}
	if attributes["reconnect_count"] != float64(1) {
		t.Errorf("expected reconnect_count 1, got %v", attributes["reconnect_count"])
	}
}

func TestSetScannerConnected_UnknownScanner(t *testing.T) {
	integration, _ := newTestIntegration(t)

	if err := integration.SetScannerConnected("ghost", true); err == nil {
		t.Error("expected error for unknown scanner")
	}
}

func TestPublishBarcode(t *testing.T) {
	integration, publisher := newTestIntegration(t)
	_ = integration.SetScannerConnected("front_desk", true)

	if err := integration.PublishBarcode("front_desk", "4006381333931"); err != nil {
		t.Fatalf("PublishBarcode() error: %v", err)
	}

	msg, ok := publisher.lastPayload(scannerStateTopic)
	if !ok {
		t.Fatal("expected barcode to be published to state topic")
	}
	if msg.Payload != "4006381333931" {
		t.Errorf("expected barcode payload '4006381333931', got %q", msg.Payload)
	}
	if msg.Retain {
		t.Error("expected barcode state to not be retained")
	}

	attrs, _ := publisher.lastPayload(healthAttributesTopic)
	var attributes map[string]any
	if err := json.Unmarshal([]byte(attrs.Payload), &attributes); err != nil {
		t.Fatalf("health attributes are not valid JSON: %v", err)
	}
	if attributes["total_scans"] != float64(1) {
		t.Errorf("expected total_scans 1, got %v", attributes["total_scans"])
	}
}

func TestPublishBarcode_UTF8Barcode(t *testing.T) {
	integration, publisher := newTestIntegration(t)
	_ = integration.SetScannerConnected("front_desk", true)

	if err := integration.PublishBarcode("front_desk", "añcç·123"); err != nil {
		t.Fatalf("PublishBarcode() error: %v", err)
	}

	msg, _ := publisher.lastPayload(scannerStateTopic)
	if msg.Payload != "añcç·123" {
		t.Errorf("expected UTF-8 barcode to pass through unchanged, got %q", msg.Payload)
	}
}

func TestPublishBarcode_UnknownScanner(t *testing.T) {
	integration, _ := newTestIntegration(t)

	if err := integration.PublishBarcode("ghost", "123"); err == nil {
		t.Error("expected error for unknown scanner")
	}
}

func TestPublishBarcode_NotConnected(t *testing.T) {
	integration, publisher := newTestIntegration(t)

	publisher.mu.Lock()
	publisher.connected = false
	publisher.mu.Unlock()

	if err := integration.PublishBarcode("front_desk", "123"); err == nil {
		t.Error("expected error when MQTT is not connected")
	}
}

func TestStart_PublishesBridgeAvailabilityAndDiagnostics(t *testing.T) {
	integration, publisher := newTestIntegration(t)

	if err := integration.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	msg, ok := publisher.lastPayload(bridgeAvailabilityTopic)
	if !ok || msg.Payload != statusOnline {
		t.Errorf("expected bridge availability 'online', got %+v", msg)
	}
	if !msg.Retain {
		t.Error("expected bridge availability to be retained")
	}

	diagConfig, ok := publisher.lastPayload("homeassistant/sensor/ha-barcode-bridge-testhost-diagnostics/config")
	if !ok {
		t.Fatal("expected diagnostics discovery config to be published")
	}
	var sensorConfig SensorConfig
	if err := json.Unmarshal([]byte(diagConfig.Payload), &sensorConfig); err != nil {
		t.Fatalf("diagnostics config is not valid JSON: %v", err)
	}
	if sensorConfig.EntityCategory != "diagnostic" {
		t.Errorf("expected diagnostics entity category 'diagnostic', got %q", sensorConfig.EntityCategory)
	}
}

func TestStop_PublishesOfflineStates(t *testing.T) {
	integration, publisher := newTestIntegration(t)
	_ = integration.SetScannerConnected("front_desk", true)

	if err := integration.Stop(); err != nil {
		t.Fatalf("Stop() error: %v", err)
	}

	availability, _ := publisher.lastPayload(scannerAvailabilityTopic)
	if availability.Payload != StatusOffline {
		t.Errorf("expected scanner availability 'offline' after stop, got %q", availability.Payload)
	}

	bridge, _ := publisher.lastPayload(bridgeAvailabilityTopic)
	if bridge.Payload != StatusOffline {
		t.Errorf("expected bridge availability 'offline' after stop, got %q", bridge.Payload)
	}

	state, _ := publisher.lastPayload(scannerStateTopic)
	if state.Payload != StatusUnknown {
		t.Errorf("expected scanner state %q after stop, got %q", StatusUnknown, state.Payload)
	}
}

func TestOnConnectCallback_RepublishesDiscovery(t *testing.T) {
	integration, publisher := newTestIntegration(t)

	if err := integration.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	before := len(publisher.payloadsFor(scannerConfigTopic))

	// Simulate an MQTT reconnect: the broker may have lost retained state.
	publisher.mu.Lock()
	callback := publisher.onConnect
	publisher.mu.Unlock()
	if callback == nil {
		t.Fatal("expected integration to register an on-connect callback")
	}
	callback()

	after := len(publisher.payloadsFor(scannerConfigTopic))
	if after != before+1 {
		t.Errorf("expected discovery config republished on reconnect (%d -> %d)", before, after)
	}
}

func TestGenerateBridgeAvailabilityTopic(t *testing.T) {
	haConfig := &config.HomeAssistantConfig{
		DiscoveryPrefix: "custom",
		InstanceID:      "instance1",
	}

	topic := GenerateBridgeAvailabilityTopic(haConfig)
	expected := "custom/sensor/ha-barcode-bridge-instance1/availability"
	if topic != expected {
		t.Errorf("expected topic %q, got %q", expected, topic)
	}

	integration := NewIntegration(newFakePublisher(), haConfig, "1.0.0", testLogger())
	if integration.GenerateBridgeAvailabilityTopic() != expected {
		t.Error("expected method and package function to generate the same topic")
	}
}

// TestConcurrentCallbacks exercises the integration from multiple goroutines
// the way per-scanner callbacks and MQTT client callbacks fire in production.
// Run with -race to validate the locking.
func TestConcurrentCallbacks(t *testing.T) {
	publisher := newFakePublisher()
	integration := NewIntegration(publisher, testHAConfig(), "1.2.3", testLogger())

	const scannerCount = 4
	for i := range scannerCount {
		id := fmt.Sprintf("scanner_%d", i)
		integration.AddScanner(id, id, &config.ScannerConfig{
			ID:              id,
			KeyboardLayout:  "us",
			TerminationChar: "enter",
		})
	}

	if err := integration.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	var wg sync.WaitGroup
	for i := range scannerCount {
		id := fmt.Sprintf("scanner_%d", i)

		wg.Add(1)
		go func() {
			defer wg.Done()
			integration.SetScannerDeviceInfo(id, testDeviceInfo())
			for range 25 {
				_ = integration.SetScannerConnected(id, true)
				_ = integration.PublishBarcode(id, "code-"+id)
				_ = integration.SetScannerConnected(id, false)
			}
		}()
	}

	// Simulate MQTT reconnects arriving concurrently from the client's
	// goroutine.
	wg.Add(1)
	go func() {
		defer wg.Done()
		publisher.mu.Lock()
		callback := publisher.onConnect
		publisher.mu.Unlock()
		for range 25 {
			callback()
		}
	}()

	wg.Wait()

	if err := integration.Stop(); err != nil {
		t.Fatalf("Stop() error: %v", err)
	}

	// Sanity check: every scanner published at least one barcode.
	for i := range scannerCount {
		id := fmt.Sprintf("scanner_%d", i)
		topic := fmt.Sprintf("homeassistant/sensor/ha-barcode-bridge-testhost-scanner-%s/state", id)
		payloads := publisher.payloadsFor(topic)
		found := false
		for _, payload := range payloads {
			if strings.HasPrefix(payload, "code-") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected at least one barcode published for %s", id)
		}
	}
}
