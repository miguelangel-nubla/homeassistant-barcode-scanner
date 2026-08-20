package mqtt

import (
	"io"
	"testing"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/miguelangel-nubla/homeassistant-barcode-scanner/pkg/config"
)

func testLogger() *logrus.Logger {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	return logger
}

func testClient() *Client {
	cfg := &config.MQTTConfig{
		BrokerURL: "mqtt://localhost:1883",
		ClientID:  "test-client",
		KeepAlive: 60,
	}
	return NewClient(cfg, "test/will", testLogger())
}

func TestNewClient(t *testing.T) {
	cfg := &config.MQTTConfig{
		BrokerURL: "mqtt://localhost:1883",
		ClientID:  "test-client",
		KeepAlive: 60,
	}

	client := NewClient(cfg, "test/will", testLogger())
	if client == nil {
		t.Fatal("expected client to be created")
	}

	if client.config != cfg {
		t.Error("expected config to be stored")
	}
	if client.willTopic != "test/will" {
		t.Errorf("expected will topic 'test/will', got %q", client.willTopic)
	}
}

func TestClient_IsConnected_InitiallyFalse(t *testing.T) {
	client := testClient()

	if client.IsConnected() {
		t.Error("expected client to initially not be connected")
	}
}

func TestClient_SetCallbacks(t *testing.T) {
	client := testClient()

	connectCalled := false
	disconnectCalled := false

	client.SetOnConnectCallback(func() { connectCalled = true })
	client.SetOnDisconnectCallback(func() { disconnectCalled = true })

	if client.onConnect == nil || client.onDisconnect == nil {
		t.Fatal("expected callbacks to be set")
	}

	client.onConnect()
	client.onDisconnect()

	if !connectCalled || !disconnectCalled {
		t.Error("expected callbacks to be invoked")
	}
}

func TestClient_WaitForConnection_Timeout(t *testing.T) {
	client := testClient()

	start := time.Now()
	err := client.WaitForConnection(100 * time.Millisecond)
	elapsed := time.Since(start)

	if err == nil {
		t.Error("expected connection wait to time out")
	}
	if elapsed < 100*time.Millisecond {
		t.Error("expected to wait at least 100ms")
	}
	if elapsed > 500*time.Millisecond {
		t.Error("expected timeout around 100ms, but waited too long")
	}
}

func TestClient_Publish_NotConnected(t *testing.T) {
	client := testClient()

	if err := client.Publish("test/topic", "message", false); err == nil {
		t.Error("expected error when publishing while not connected")
	}
	if err := client.Publish("test/topic", "message", true); err == nil {
		t.Error("expected error when publishing retained while not connected")
	}
}

func TestClient_DisconnectAndStop_Safe(t *testing.T) {
	client := testClient()

	if err := client.Stop(); err != nil {
		t.Errorf("expected no error stopping client, got: %v", err)
	}
	if client.IsConnected() {
		t.Error("expected client to not be connected after stop")
	}

	// Repeated disconnects must be safe.
	client.Disconnect()
	client.Disconnect()
}

func TestClient_QoSFromConfig(t *testing.T) {
	zero := byte(0)
	cfg := &config.MQTTConfig{
		BrokerURL: "mqtt://localhost:1883",
		ClientID:  "test-client",
		QoS:       &zero,
		KeepAlive: 60,
	}

	client := NewClient(cfg, "test/will", testLogger())
	if got := client.config.GetQoS(); got != 0 {
		t.Errorf("expected client to use explicit QoS 0, got %d", got)
	}
}
