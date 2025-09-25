package app

import (
	"fmt"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/miguelangel-nubla/homeassistant-barcode-scanner/pkg/homeassistant"
	"github.com/miguelangel-nubla/homeassistant-barcode-scanner/pkg/mqtt"
	"github.com/miguelangel-nubla/homeassistant-barcode-scanner/pkg/scanner"
)

type Service interface {
	Start() error
	Stop() error
}

type ServiceManager struct {
	services map[string]Service
	order    []string
	logger   *logrus.Logger
}

func NewServiceManager(logger *logrus.Logger) *ServiceManager {
	return &ServiceManager{
		services: make(map[string]Service),
		logger:   logger,
	}
}

func (sm *ServiceManager) Register(name string, service Service) {
	sm.services[name] = service
	sm.order = append(sm.order, name)
	sm.logger.WithField("service", name).Debug("Service registered")
}

func (sm *ServiceManager) Get(name string) Service {
	if service, ok := sm.services[name]; ok {
		return service
	}
	return nil
}

func (sm *ServiceManager) GetMQTTClient() *mqtt.Client {
	service := sm.Get("mqtt")
	if service == nil {
		return nil
	}
	if mqttClient, ok := service.(*mqtt.Client); ok {
		return mqttClient
	}
	sm.logger.WithField("service", "mqtt").Error("Service type assertion failed")
	return nil
}

func (sm *ServiceManager) GetHomeAssistantIntegration() *homeassistant.Integration {
	service := sm.Get("homeassistant")
	if service == nil {
		return nil
	}
	if haIntegration, ok := service.(*homeassistant.Integration); ok {
		return haIntegration
	}
	sm.logger.WithField("service", "homeassistant").Error("Service type assertion failed")
	return nil
}

func (sm *ServiceManager) GetScannerManager() *scanner.ScannerManager {
	service := sm.Get("scanner")
	if service == nil {
		return nil
	}
	if scannerManager, ok := service.(*scanner.ScannerManager); ok {
		return scannerManager
	}
	sm.logger.WithField("service", "scanner").Error("Service type assertion failed")
	return nil
}

func (sm *ServiceManager) StartAll() error {
	sm.logger.Info("Starting application services...")

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

	for _, name := range sm.order {
		if name == "mqtt" {
			continue
		}

		service := sm.services[name]
		logger := sm.logger.WithField("service", name)
		logger.Debug("Starting service")
		if err := service.Start(); err != nil {
			return fmt.Errorf("failed to start service %s: %w", name, err)
		}
		logger.Debug("Service started")
	}

	sm.logger.Info("All services started successfully")
	return nil
}

func (sm *ServiceManager) StopAll() error {
	sm.logger.Info("Stopping application services...")

	for i := len(sm.order) - 1; i >= 0; i-- {
		name := sm.order[i]
		service := sm.services[name]

		logger := sm.logger.WithField("service", name)
		logger.Debug("Stopping service")
		if err := service.Stop(); err != nil {
			logger.WithError(err).Error("Failed to stop service")
		} else {
			logger.Debug("Service stopped")
		}
	}

	mqttClient := sm.GetMQTTClient()
	if mqttClient != nil {
		mqttClient.Disconnect()
		sm.logger.Debug("MQTT service disconnected")
	}

	sm.logger.Info("All services stopped")
	return nil
}
