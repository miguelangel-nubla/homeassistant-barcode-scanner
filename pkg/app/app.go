package app

import (
	"time"

	"github.com/sirupsen/logrus"

	"github.com/miguelangel-nubla/homeassistant-barcode-scanner/pkg/config"
	"github.com/miguelangel-nubla/homeassistant-barcode-scanner/pkg/homeassistant"
	"github.com/miguelangel-nubla/homeassistant-barcode-scanner/pkg/mqtt"
	"github.com/miguelangel-nubla/homeassistant-barcode-scanner/pkg/scanner"
)

// Application represents the main application with separated concerns
type Application struct {
	config   *config.Config
	logger   *logrus.Logger
	version  string
	services *ServiceManager
	handlers *EventHandlers
}

// NewApplication creates a new application instance
func NewApplication(cfg *config.Config, logger *logrus.Logger, version string) *Application {
	app := &Application{
		config:  cfg,
		logger:  logger,
		version: version,
	}

	// Initialize services
	app.services = NewServiceManager(logger)
	app.handlers = NewEventHandlers(logger)

	return app
}

// Initialize sets up all application components
func (app *Application) Initialize() error {
	app.logger.Info("Initializing application components...")

	// Generate bridge availability topic directly using utility function
	bridgeAvailabilityTopic := homeassistant.GenerateBridgeAvailabilityTopic(&app.config.HomeAssistant)

	// Create MQTT client with bridge availability as will topic
	mqttClient, err := mqtt.NewClient(
		&app.config.MQTT,
		bridgeAvailabilityTopic,
		app.logger,
	)
	if err != nil {
		return err
	}

	// Create Home Assistant multi-scanner integration with proper MQTT client
	haManager := homeassistant.NewIntegration(
		mqttClient,
		&app.config.HomeAssistant,
		app.version,
		app.logger,
	)

	// Create scanner manager for multiple scanners
	// Convert map to slice for scanner manager
	var scannerConfigs []config.ScannerConfig
	for _, cfg := range app.config.Scanners {
		scannerConfigs = append(scannerConfigs, cfg)
	}
	scannerManager := scanner.NewScannerManager(scannerConfigs, app.logger)
	scannerManager.SetReconnectDelay(5 * time.Second)

	// Register scanners with Home Assistant integration
	for _, scannerConfig := range app.config.Scanners {
		scannerName := scannerConfig.Name
		if scannerName == "" {
			scannerName = scannerConfig.ID
		}
		haManager.AddScanner(scannerConfig.ID, scannerName, &scannerConfig)
	}

	// Register services
	app.services.Register("mqtt", mqttClient)
	app.services.Register("homeassistant", haManager)
	app.services.Register("scanner", scannerManager)

	// Set up event handlers
	app.handlers.SetupHandlers(app.services, haManager, scannerManager)

	return nil
}

// Start starts all application services
func (app *Application) Start() error {
	return app.services.StartAll()
}

// Stop stops all application services
func (app *Application) Stop() error {
	return app.services.StopAll()
}
