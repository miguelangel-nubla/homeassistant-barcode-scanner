package app

import (
	"time"

	"github.com/sirupsen/logrus"

	"github.com/miguelangel-nubla/homeassistant-barcode-scanner/pkg/config"
	"github.com/miguelangel-nubla/homeassistant-barcode-scanner/pkg/homeassistant"
	"github.com/miguelangel-nubla/homeassistant-barcode-scanner/pkg/mqtt"
	"github.com/miguelangel-nubla/homeassistant-barcode-scanner/pkg/scanner"
)

type Application struct {
	config   *config.Config
	logger   *logrus.Logger
	version  string
	services *ServiceManager
	handlers *EventHandlers
}

func NewApplication(cfg *config.Config, logger *logrus.Logger, version string) *Application {
	app := &Application{
		config:  cfg,
		logger:  logger,
		version: version,
	}

	app.services = NewServiceManager(logger)
	app.handlers = NewEventHandlers(logger)

	return app
}

func (app *Application) Initialize() error {
	app.logger.Info("Initializing application components...")

	bridgeAvailabilityTopic := homeassistant.GenerateBridgeAvailabilityTopic(&app.config.HomeAssistant)

	mqttClient, err := mqtt.NewClient(
		&app.config.MQTT,
		bridgeAvailabilityTopic,
		app.logger,
	)
	if err != nil {
		return err
	}

	haManager := homeassistant.NewIntegration(
		mqttClient,
		&app.config.HomeAssistant,
		app.version,
		app.logger,
	)

	scannerManager := scanner.NewScannerManagerFromMap(app.config.Scanners, app.logger)
	scannerManager.SetReconnectDelay(5 * time.Second)

	for _, scannerConfig := range app.config.Scanners {
		scannerName := scannerConfig.Name
		if scannerName == "" {
			scannerName = scannerConfig.ID
		}
		haManager.AddScanner(scannerConfig.ID, scannerName, &scannerConfig)
	}

	app.services.Register("mqtt", mqttClient)
	app.services.Register("homeassistant", haManager)
	app.services.Register("scanner", scannerManager)

	app.handlers.SetupHandlers(app.services, haManager, scannerManager)

	return nil
}

func (app *Application) Start() error {
	return app.services.StartAll()
}

func (app *Application) Stop() error {
	return app.services.StopAll()
}
