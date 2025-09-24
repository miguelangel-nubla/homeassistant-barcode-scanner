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
	opts := mqtt.NewClientOptions().
		AddBroker(c.config.BrokerURL).
		SetClientID(c.config.ClientID).
		SetKeepAlive(time.Duration(c.config.KeepAlive) * time.Second).
		SetCleanSession(true).
		SetAutoReconnect(true).
		SetMaxReconnectInterval(60 * time.Second).
		SetConnectRetryInterval(2 * time.Second).
		SetConnectRetry(true).
		SetConnectTimeout(10 * time.Second).
		SetPingTimeout(5 * time.Second).
		SetWriteTimeout(5 * time.Second).
		SetOnConnectHandler(c.handleConnect).
		SetConnectionLostHandler(c.handleDisconnect)

	// Credentials
	if c.config.Username != "" {
		opts.SetUsername(c.config.Username)
		if c.config.Password != "" {
			opts.SetPassword(c.config.Password)
		}
	}

	// TLS for secure connections
	if c.config.IsSecure() {
		opts.SetTLSConfig(&tls.Config{
			InsecureSkipVerify: c.config.InsecureSkipVerify, // #nosec G402 - configurable for dev environments
		})
	}

	// Will message (retained for availability)
	if c.willTopic != "" {
		opts.SetWill(c.willTopic, "offline", c.config.QoS, true)
	}

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

// Start starts the MQTT client (implements Service interface)
func (c *Client) Start() error {
	return c.Connect()
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

// Stop stops the MQTT client (implements Service interface)
func (c *Client) Stop() error {
	c.Disconnect()
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

// Publish publishes a message to the specified topic with retain flag
func (c *Client) Publish(topic, payload string, retain bool) error {
	if !c.IsConnected() {
		c.logger.Debugf("MQTT not connected, cannot publish to %s", topic)
		return fmt.Errorf("MQTT client is not connected")
	}

	c.logger.Debugf("Publishing to topic %s: %s", topic, payload)

	token := c.client.Publish(topic, c.config.QoS, retain, payload)
	token.Wait()
	if err := token.Error(); err != nil {
		c.logger.Errorf("Failed to publish to %s: %v", topic, err)
		return err
	}
	c.logger.Debugf("Successfully published to %s", topic)

	return nil
}

// PublishWithRetry publishes a message with retry logic for critical messages
func (c *Client) PublishWithRetry(topic, payload string, maxRetries int, retryDelay time.Duration) error {
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if err := c.attemptPublish(topic, payload, attempt, maxRetries); err == nil {
			return nil
		}

		if attempt < maxRetries {
			c.logger.Debugf("Waiting %v before retry %d for topic %s", retryDelay, attempt+2, topic)
			time.Sleep(retryDelay)
		}
	}

	return fmt.Errorf("failed to publish to %s after %d attempts", topic, maxRetries+1)
}

func (c *Client) attemptPublish(topic, payload string, attempt, maxRetries int) error {
	if !c.IsConnected() {
		c.logger.Debugf("MQTT not connected during publish attempt %d/%d for topic %s", attempt+1, maxRetries+1, topic)
		return fmt.Errorf("not connected")
	}

	if err := c.Publish(topic, payload, false); err != nil {
		c.logger.Warnf("Publish attempt %d/%d failed for topic %s: %v", attempt+1, maxRetries+1, topic, err)
		return err
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

	// Publish online status (retained for will message)
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
	c.logger.Info("MQTT client will attempt automatic reconnection...")
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
