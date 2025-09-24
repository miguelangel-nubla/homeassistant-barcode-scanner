package app

import (
	"fmt"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/miguelangel-nubla/homeassistant-barcode-scanner/pkg/homeassistant"
	"github.com/miguelangel-nubla/homeassistant-barcode-scanner/pkg/mqtt"
	"github.com/miguelangel-nubla/homeassistant-barcode-scanner/pkg/scanner"
)

// Service represents a manageable service component
type Service interface {
	Start() error
	Stop() error
}

// ServiceManager manages the lifecycle of application services
type ServiceManager struct {
	services map[string]Service
	order    []string
	logger   *logrus.Logger
}

// NewServiceManager creates a new service manager
func NewServiceManager(logger *logrus.Logger) *ServiceManager {
	return &ServiceManager{
		services: make(map[string]Service),
		logger:   logger,
	}
}

// Register registers a service with the manager
func (sm *ServiceManager) Register(name string, service Service) {
	sm.services[name] = service
	sm.order = append(sm.order, name)
	sm.logger.Debugf("Registered service: %s", name)
}

// Get returns a service by name with type assertion
func (sm *ServiceManager) Get(name string) Service {
	if service, ok := sm.services[name]; ok {
		return service
	}
	return nil
}

// GetMQTTClient returns the MQTT client service
func (sm *ServiceManager) GetMQTTClient() *mqtt.Client {
	service := sm.Get("mqtt")
	if service == nil {
		return nil
	}
	mqttClient, _ := service.(*mqtt.Client)
	return mqttClient
}

// GetHomeAssistantIntegration returns the Home Assistant multi-integration service
func (sm *ServiceManager) GetHomeAssistantIntegration() *homeassistant.Integration {
	service := sm.Get("homeassistant")
	if service == nil {
		return nil
	}
	haIntegration, _ := service.(*homeassistant.Integration)
	return haIntegration
}

// GetScannerManager returns the scanner manager service
func (sm *ServiceManager) GetScannerManager() *scanner.ScannerManager {
	service := sm.Get("scanner")
	if service == nil {
		return nil
	}
	scannerManager, _ := service.(*scanner.ScannerManager)
	return scannerManager
}

// StartAll starts all registered services in order
func (sm *ServiceManager) StartAll() error {
	sm.logger.Info("Starting application services...")

	// Special handling for MQTT - need to connect and wait
	mqttClient := sm.GetMQTTClient()
	if mqttClient != nil {
		if err := mqttClient.Connect(); err != nil {
			return fmt.Errorf("MQTT connection failed: %w", err)
		}
		if err := mqttClient.WaitForConnection(10 * time.Second); err != nil {
			return fmt.Errorf("MQTT connection timeout: %w", err)
		}
		sm.logger.Info("MQTT service started")
	}

	// Start other services
	for _, name := range sm.order {
		if name == "mqtt" {
			continue // Already started
		}

		service := sm.services[name]
		sm.logger.Infof("Starting service: %s", name)
		if err := service.Start(); err != nil {
			return fmt.Errorf("failed to start service %s: %w", name, err)
		}
		sm.logger.Infof("Service %s started successfully", name)
	}

	sm.logger.Info("All services started successfully")
	return nil
}

// StopAll stops all registered services in reverse order
func (sm *ServiceManager) StopAll() error {
	sm.logger.Info("Stopping application services...")

	// Stop services in reverse order
	for i := len(sm.order) - 1; i >= 0; i-- {
		name := sm.order[i]
		service := sm.services[name]

		sm.logger.Infof("Stopping service: %s", name)
		if err := service.Stop(); err != nil {
			sm.logger.Errorf("Error stopping service %s: %v", name, err)
			// Continue stopping other services
		} else {
			sm.logger.Infof("Service %s stopped successfully", name)
		}
	}

	// Special handling for MQTT disconnect
	mqttClient := sm.GetMQTTClient()
	if mqttClient != nil {
		mqttClient.Disconnect()
		sm.logger.Info("MQTT service disconnected")
	}

	sm.logger.Info("All services stopped")
	return nil
}
