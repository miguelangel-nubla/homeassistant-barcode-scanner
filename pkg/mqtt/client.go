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

const (
	DefaultMaxReconnectInterval = 60 * time.Second
	DefaultConnectRetryInterval = 2 * time.Second
	DefaultConnectTimeout       = 10 * time.Second
	DefaultPingTimeout          = 5 * time.Second
	DefaultWriteTimeout         = 5 * time.Second
	DefaultWaitForConnTimeout   = 100 * time.Millisecond
	DefaultDisconnectTimeout    = 250 // milliseconds
)

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

func (c *Client) buildClientOptions() *mqtt.ClientOptions {
	opts := mqtt.NewClientOptions().
		AddBroker(c.config.BrokerURL).
		SetClientID(c.config.ClientID).
		SetKeepAlive(time.Duration(c.config.KeepAlive) * time.Second).
		SetCleanSession(true).
		SetAutoReconnect(true).
		SetMaxReconnectInterval(DefaultMaxReconnectInterval).
		SetConnectRetryInterval(DefaultConnectRetryInterval).
		SetConnectRetry(true).
		SetConnectTimeout(DefaultConnectTimeout).
		SetPingTimeout(DefaultPingTimeout).
		SetWriteTimeout(DefaultWriteTimeout).
		SetOnConnectHandler(c.handleConnect).
		SetConnectionLostHandler(c.handleDisconnect)

	if c.config.Username != "" {
		opts.SetUsername(c.config.Username)
		if c.config.Password != "" {
			opts.SetPassword(c.config.Password)
		}
	}

	if c.config.IsSecure() {
		opts.SetTLSConfig(&tls.Config{
			InsecureSkipVerify: c.config.InsecureSkipVerify, // #nosec G402 - configurable for dev environments
		})
	}

	if c.willTopic != "" {
		opts.SetWill(c.willTopic, "offline", c.config.QoS, true)
	}

	return opts
}

func (c *Client) SetOnConnectCallback(callback func()) {
	c.onConnect = callback
}

func (c *Client) SetOnDisconnectCallback(callback func()) {
	c.onDisconnect = callback
}

func (c *Client) Start() error {
	return c.Connect()
}

func (c *Client) Connect() error {
	c.logger.Infof("Connecting to MQTT broker: %s", c.config.BrokerURL)

	token := c.client.Connect()
	if token.Wait() && token.Error() != nil {
		return fmt.Errorf("failed to connect to MQTT broker: %w", token.Error())
	}

	return nil
}

func (c *Client) Stop() error {
	c.Disconnect()
	return nil
}

func (c *Client) Disconnect() {
	c.logger.Debug("Disconnecting from MQTT broker")

	c.client.Disconnect(DefaultDisconnectTimeout)
	c.setConnected(false)
}

func (c *Client) Publish(topic, payload string, retain bool) error {
	if !c.IsConnected() {
		return fmt.Errorf("MQTT client is not connected")
	}

	token := c.client.Publish(topic, c.config.QoS, retain, payload)
	token.Wait()
	if err := token.Error(); err != nil {
		c.logger.WithFields(map[string]any{
			"topic":  topic,
			"retain": retain,
		}).WithError(err).Error("MQTT publish failed")
		return err
	}

	return nil
}

func (c *Client) PublishWithRetry(topic, payload string, maxRetries int, retryDelay time.Duration) error {
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if err := c.attemptPublish(topic, payload, attempt, maxRetries); err == nil {
			return nil
		}

		if attempt < maxRetries {
			time.Sleep(retryDelay)
		}
	}

	return fmt.Errorf("failed to publish to %s after %d attempts", topic, maxRetries+1)
}

func (c *Client) attemptPublish(topic, payload string, attempt, maxRetries int) error {
	if !c.IsConnected() {
		return fmt.Errorf("not connected")
	}

	if err := c.Publish(topic, payload, false); err != nil {
		if attempt == maxRetries {
			c.logger.WithFields(map[string]any{
				"topic":    topic,
				"attempts": maxRetries + 1,
			}).WithError(err).Error("MQTT publish failed after all retries")
		}
		return err
	}

	return nil
}

func (c *Client) IsConnected() bool {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.connected && c.client.IsConnected()
}

func (c *Client) setConnected(connected bool) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.connected = connected
}

func (c *Client) handleConnect(client mqtt.Client) {
	c.logger.Debug("MQTT client connected")
	c.setConnected(true)

	if c.willTopic != "" {
		if err := c.Publish(c.willTopic, "online", true); err != nil {
			c.logger.Errorf("Failed to publish online status: %v", err)
		}
	}

	if c.onConnect != nil {
		c.onConnect()
	}
}

func (c *Client) handleDisconnect(client mqtt.Client, err error) {
	c.logger.Errorf("MQTT connection lost: %v", err)
	c.logger.Info("MQTT client will attempt automatic reconnection...")
	c.setConnected(false)

	if c.onDisconnect != nil {
		c.onDisconnect()
	}
}

func (c *Client) WaitForConnection(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if c.IsConnected() {
			return nil
		}
		time.Sleep(DefaultWaitForConnTimeout)
	}
	return fmt.Errorf("timeout waiting for MQTT connection")
}
