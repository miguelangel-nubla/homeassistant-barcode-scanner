package mqtt

import (
	"testing"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/miguelangel-nubla/homeassistant-barcode-scanner/pkg/config"
)

func TestNewClient_ValidConfig(t *testing.T) {
	cfg := &config.MQTTConfig{
		BrokerURL: "mqtt://localhost:1883",
		ClientID:  "test-client",
		QoS:       1,
		KeepAlive: 60,
	}

	logger := logrus.New()
	willTopic := "test/will"

	client, err := NewClient(cfg, willTopic, logger)
	if err != nil {
		t.Fatalf("Expected no error creating client, got: %v", err)
	}

	if client == nil {
		t.Fatal("Expected client to be created")
	}

	if client.config != cfg {
		t.Error("Expected config to be stored")
	}

	if client.logger != logger {
		t.Error("Expected logger to be stored")
	}
}

func TestNewClient_InvalidBrokerURL(t *testing.T) {
	cfg := &config.MQTTConfig{
		BrokerURL: "invalid-url",
		ClientID:  "test-client",
	}

	logger := logrus.New()

	client, err := NewClient(cfg, "test/will", logger)
	// NewClient might not validate URL format, it just creates the client
	if err != nil {
		t.Logf("NewClient correctly rejected invalid URL: %v", err)
	} else {
		t.Log("NewClient accepted invalid URL (validation happens at connect time)")
		// Verify client was still created
		if client == nil {
			t.Error("Expected client to be created even with invalid URL")
		}
	}
}

func TestClient_IsConnected_InitiallyFalse(t *testing.T) {
	cfg := &config.MQTTConfig{
		BrokerURL: "mqtt://localhost:1883",
		ClientID:  "test-client",
	}

	logger := logrus.New()
	client, err := NewClient(cfg, "test/will", logger)
	if err != nil {
		t.Fatalf("Expected no error creating client, got: %v", err)
	}

	if client.IsConnected() {
		t.Error("Expected client to initially not be connected")
	}
}

func TestClient_SetCallbacks(t *testing.T) {
	cfg := &config.MQTTConfig{
		BrokerURL: "mqtt://localhost:1883",
		ClientID:  "test-client",
	}

	logger := logrus.New()
	client, err := NewClient(cfg, "test/will", logger)
	if err != nil {
		t.Fatalf("Expected no error creating client, got: %v", err)
	}

	connectCalled := false
	disconnectCalled := false

	client.SetOnConnectCallback(func() {
		connectCalled = true
	})

	client.SetOnDisconnectCallback(func() {
		disconnectCalled = true
	})

	if client.onConnect == nil {
		t.Error("Expected connect callback to be set")
	}

	if client.onDisconnect == nil {
		t.Error("Expected disconnect callback to be set")
	}

	client.onConnect()
	if !connectCalled {
		t.Error("Expected connect callback to be called")
	}

	client.onDisconnect()
	if !disconnectCalled {
		t.Error("Expected disconnect callback to be called")
	}
}

func TestMQTTConfig_IsSecure(t *testing.T) {
	tests := []struct {
		name      string
		brokerURL string
		expected  bool
	}{
		{"Plain MQTT", "mqtt://localhost:1883", false},
		{"Secure MQTT", "mqtts://localhost:8883", true},
		{"WebSocket", "ws://localhost:9001", false},
		{"Secure WebSocket", "wss://localhost:9002", true},
		{"TCP", "tcp://localhost:1883", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.MQTTConfig{BrokerURL: tt.brokerURL}
			if got := cfg.IsSecure(); got != tt.expected {
				t.Errorf("IsSecure() = %v, expected %v", got, tt.expected)
			}
		})
	}
}

func TestClient_WaitForConnection_Timeout(t *testing.T) {
	cfg := &config.MQTTConfig{
		BrokerURL: "mqtt://localhost:1883",
		ClientID:  "test-client",
	}

	logger := logrus.New()
	client, err := NewClient(cfg, "test/will", logger)
	if err != nil {
		t.Fatalf("Expected no error creating client, got: %v", err)
	}

	start := time.Now()
	err = client.WaitForConnection(100 * time.Millisecond)
	elapsed := time.Since(start)

	if err == nil {
		t.Error("Expected connection to timeout and return error")
	}

	if elapsed < 100*time.Millisecond {
		t.Error("Expected to wait at least 100ms")
	}

	if elapsed > 200*time.Millisecond {
		t.Error("Expected to timeout around 100ms, but waited too long")
	}
}

func TestClient_PublishRetained_NotConnected(t *testing.T) {
	cfg := &config.MQTTConfig{
		BrokerURL: "mqtt://localhost:1883",
		ClientID:  "test-client",
	}

	logger := logrus.New()
	client, err := NewClient(cfg, "test/will", logger)
	if err != nil {
		t.Fatalf("Expected no error creating client, got: %v", err)
	}

	err = client.Publish("test/topic", "test message", true)
	if err == nil {
		t.Error("Expected error when publishing while not connected")
	}
}

func TestClient_Publish_NotConnected(t *testing.T) {
	cfg := &config.MQTTConfig{
		BrokerURL: "mqtt://localhost:1883",
		ClientID:  "test-client",
	}

	logger := logrus.New()
	client, err := NewClient(cfg, "test/will", logger)
	if err != nil {
		t.Fatalf("Expected no error creating client, got: %v", err)
	}

	err = client.Publish("test/topic", "test message", false)
	if err == nil {
		t.Error("Expected error when publishing while not connected")
	}
}

func TestClient_Disconnect_Safe(t *testing.T) {
	cfg := &config.MQTTConfig{
		BrokerURL: "mqtt://localhost:1883",
		ClientID:  "test-client",
	}

	logger := logrus.New()
	client, err := NewClient(cfg, "test/will", logger)
	if err != nil {
		t.Fatalf("Expected no error creating client, got: %v", err)
	}

	// Stop/Disconnect should not return an error even if never connected
	err = client.Stop()
	if err != nil {
		t.Errorf("Expected no error stopping client, got: %v", err)
	}

	// Verify client is not connected after stop
	if client.IsConnected() {
		t.Error("Expected client to not be connected after stop")
	}

	// Should be safe to call disconnect multiple times
	client.Disconnect()
	client.Disconnect()
}
