package mqtt

import (
	"crypto/tls"
	"fmt"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/sirupsen/logrus"

	"github.com/miguelangel-nubla/homeassistant-barcode-scanner/pkg/config"
)

// Client represents an MQTT client with auto-reconnection capabilities
type Client struct {
	client       mqtt.Client
	config       *config.MQTTConfig
	logger       *logrus.Logger
	connected    bool
	mutex        sync.RWMutex
	willTopic    string
	onConnect    func()
	onDisconnect func()
}

// NewClient creates a new MQTT client
func NewClient(cfg *config.MQTTConfig, willTopic string, logger *logrus.Logger) (*Client, error) {
	c := &Client{
		config:    cfg,
		logger:    logger,
		willTopic: willTopic,
	}

	opts := c.buildClientOptions()
	c.client = mqtt.NewClient(opts)

	return c, nil
}

// buildClientOptions creates and configures MQTT client options
func (c *Client) buildClientOptions() *mqtt.ClientOptions {
	opts := mqtt.NewClientOptions()
	opts.AddBroker(c.config.BrokerURL)
	opts.SetClientID(c.config.ClientID)
	opts.SetKeepAlive(time.Duration(c.config.KeepAlive) * time.Second)
	opts.SetCleanSession(true)
	opts.SetAutoReconnect(true)
	opts.SetMaxReconnectInterval(30 * time.Second)
	opts.SetConnectRetryInterval(5 * time.Second)

	// Set credentials
	if c.config.Username != "" {
		opts.SetUsername(c.config.Username)
		if c.config.Password != "" {
			opts.SetPassword(c.config.Password)
		}
	}

	// Configure TLS for secure connections
	if c.config.IsSecure() {
		opts.SetTLSConfig(&tls.Config{
			InsecureSkipVerify: c.config.InsecureSkipVerify,
		})
	}

	// Set will message
	if c.willTopic != "" {
		opts.SetWill(c.willTopic, "offline", c.config.QoS, c.config.Retained)
	}

	// Set handlers
	opts.SetOnConnectHandler(c.handleConnect)
	opts.SetConnectionLostHandler(c.handleDisconnect)

	return opts
}

// SetOnConnectCallback sets the callback function to be called when connected
func (c *Client) SetOnConnectCallback(callback func()) {
	c.onConnect = callback
}

// SetOnDisconnectCallback sets the callback function to be called when disconnected
func (c *Client) SetOnDisconnectCallback(callback func()) {
	c.onDisconnect = callback
}

// Connect connects to the MQTT broker
func (c *Client) Connect() error {
	c.logger.Infof("Connecting to MQTT broker: %s", c.config.BrokerURL)

	token := c.client.Connect()
	if token.Wait() && token.Error() != nil {
		return fmt.Errorf("failed to connect to MQTT broker: %w", token.Error())
	}

	return nil
}

// Disconnect disconnects from the MQTT broker
func (c *Client) Disconnect() {
	c.logger.Info("Disconnecting from MQTT broker")

	// Publish offline status before disconnecting
	if c.willTopic != "" && c.IsConnected() {
		_ = c.Publish(c.willTopic, "offline", true)
	}

	c.client.Disconnect(250)
	c.setConnected(false)
}

// Publish publishes a message to the specified topic
func (c *Client) Publish(topic, payload string, wait bool) error {
	if !c.IsConnected() {
		return fmt.Errorf("MQTT client is not connected")
	}

	c.logger.Debugf("Publishing to topic %s: %s", topic, payload)

	token := c.client.Publish(topic, c.config.QoS, c.config.Retained, payload)
	if wait {
		token.Wait()
		return token.Error()
	}

	return nil
}

// IsConnected returns true if the client is connected to the broker
func (c *Client) IsConnected() bool {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.connected && c.client.IsConnected()
}

// setConnected sets the connection status
func (c *Client) setConnected(connected bool) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.connected = connected
}

// handleConnect is called when the client connects to the broker
func (c *Client) handleConnect(client mqtt.Client) {
	c.logger.Info("MQTT client connected")
	c.setConnected(true)

	// Publish online status
	if c.willTopic != "" {
		if err := c.Publish(c.willTopic, "online", true); err != nil {
			c.logger.Errorf("Failed to publish online status: %v", err)
		}
	}

	// Call user callback
	if c.onConnect != nil {
		c.onConnect()
	}
}

// handleDisconnect is called when the connection to the broker is lost
func (c *Client) handleDisconnect(client mqtt.Client, err error) {
	c.logger.Errorf("MQTT connection lost: %v", err)
	c.setConnected(false)

	// Call user callback
	if c.onDisconnect != nil {
		c.onDisconnect()
	}
}

// WaitForConnection waits for the client to connect, with a timeout
func (c *Client) WaitForConnection(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if c.IsConnected() {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for MQTT connection")
}