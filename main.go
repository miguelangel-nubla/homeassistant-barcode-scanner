package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v3"

	"github.com/miguelangel-nubla/homeassistant-barcode-scanner/pkg/config"
	"github.com/miguelangel-nubla/homeassistant-barcode-scanner/pkg/homeassistant"
	"github.com/miguelangel-nubla/homeassistant-barcode-scanner/pkg/mqtt"
	"github.com/miguelangel-nubla/homeassistant-barcode-scanner/pkg/scanner"
)

const (
	AppName    = "homeassistant-barcode-scanner"
	AppVersion = "1.0.0"
)

// Application represents the main application
type Application struct {
	config      *config.Config
	logger      *logrus.Logger
	mqttClient  *mqtt.Client
	scanner     *scanner.BarcodeScanner
	haManager   *homeassistant.Integration
	shutdownCh  chan struct{}
}

func main() {
	cmd := &cli.Command{
		Name:    AppName,
		Usage:   "USB Barcode Scanner client for Home Assistant",
		Version: AppVersion,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "config",
				Aliases: []string{"c"},
				Usage:   "Load configuration from `FILE`",
				Value:   "config.yaml",
			},
			&cli.BoolFlag{
				Name:  "list-devices",
				Usage: "List available HID devices that might be barcode scanners",
			},
			&cli.StringFlag{
				Name:  "log-level",
				Usage: "Set log level (debug, info, warn, error)",
				Value: "info",
			},
		},
		Action: runApp,
	}

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runApp(ctx context.Context, c *cli.Command) error {
	logger := setupLogger(c)

	// Handle list-devices flag
	if c.Bool("list-devices") {
		return listDevices(logger)
	}

	// Load and validate configuration
	cfg, err := config.LoadConfig(c.String("config"))
	if err != nil {
		return fmt.Errorf("configuration error: %w", err)
	}

	// Apply config logging settings if not overridden by flags
	if !c.IsSet("log-level") {
		if level, err := logrus.ParseLevel(cfg.Logging.Level); err == nil {
			logger.SetLevel(level)
		}
	}
	if cfg.Logging.Format == "json" {
		logger.SetFormatter(&logrus.JSONFormatter{})
	}

	logger.Infof("Starting %s v%s", AppName, AppVersion)

	// Create and run application
	app := &Application{
		config:     cfg,
		logger:     logger,
		shutdownCh: make(chan struct{}),
	}

	return app.Run()
}

// setupLogger configures the logger based on CLI flags
func setupLogger(c *cli.Command) *logrus.Logger {
	logger := logrus.New()
	logger.SetFormatter(&logrus.TextFormatter{FullTimestamp: true})

	if level, err := logrus.ParseLevel(c.String("log-level")); err == nil {
		logger.SetLevel(level)
	}

	return logger
}

func (app *Application) Run() error {
	// Create MQTT client
	var err error
	app.mqttClient, err = mqtt.NewClient(
		&app.config.MQTT,
		app.config.HomeAssistant.GetAvailabilityTopic(),
		app.logger,
	)
	if err != nil {
		return fmt.Errorf("failed to create MQTT client: %w", err)
	}

	// Create Home Assistant integration
	app.haManager = homeassistant.NewIntegration(
		app.mqttClient,
		&app.config.HomeAssistant,
		AppVersion,
		app.logger,
	)

	// Create barcode scanner
	app.scanner = scanner.NewBarcodeScanner(
		app.config.Scanner.VendorID,
		app.config.Scanner.ProductID,
		app.config.Scanner.DevicePath,
		app.config.Scanner.TerminationChar,
		app.logger,
	)

	// Set up barcode scan callback
	app.scanner.SetOnScanCallback(app.onBarcodeScanned)

	// Set up signal handling
	app.setupSignalHandling()

	// Start components
	if err := app.start(); err != nil {
		return err
	}

	// Wait for shutdown signal
	<-app.shutdownCh

	// Graceful shutdown
	return app.shutdown()
}

func (app *Application) start() error {
	app.logger.Info("Starting application components...")

	// Connect to MQTT broker
	if err := app.mqttClient.Connect(); err != nil {
		return fmt.Errorf("MQTT connection failed: %w", err)
	}

	// Wait for MQTT connection
	if err := app.mqttClient.WaitForConnection(10 * time.Second); err != nil {
		return fmt.Errorf("MQTT connection timeout: %w", err)
	}

	// Start Home Assistant integration
	if err := app.haManager.Start(); err != nil {
		return fmt.Errorf("Home Assistant integration failed: %w", err)
	}

	// Start barcode scanner
	if err := app.scanner.Start(); err != nil {
		return fmt.Errorf("barcode scanner failed: %w", err)
	}

	app.logger.Info("All components started successfully")
	return nil
}

func (app *Application) shutdown() error {
	app.logger.Info("Shutting down application...")

	// Stop components in reverse order
	if app.scanner != nil {
		if err := app.scanner.Stop(); err != nil {
			app.logger.Errorf("Error stopping scanner: %v", err)
		}
	}

	if app.haManager != nil {
		if err := app.haManager.Stop(); err != nil {
			app.logger.Errorf("Error stopping Home Assistant integration: %v", err)
		}
	}

	if app.mqttClient != nil {
		app.mqttClient.Disconnect()
	}

	app.logger.Info("Application shutdown complete")
	return nil
}

func (app *Application) onBarcodeScanned(barcode string) {
	app.logger.Infof("Barcode scanned: %s", barcode)

	// Publish to Home Assistant
	if err := app.haManager.PublishBarcode(barcode); err != nil {
		app.logger.Errorf("Failed to publish barcode to Home Assistant: %v", err)
	}
}

func (app *Application) setupSignalHandling() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		app.logger.Infof("Received signal: %v", sig)
		close(app.shutdownCh)
	}()
}

func listDevices(logger *logrus.Logger) error {
	logger.Info("Scanning for ALL HID devices...")

	// List ALL HID devices first
	allDevices := scanner.ListAllDevices()
	if len(allDevices) == 0 {
		logger.Warn("No HID devices found at all - this might indicate:")
		logger.Warn("  1. No HID devices connected")
		logger.Warn("  2. Permission issues (try running with sudo)")
		logger.Warn("  3. Missing udev rules on Linux")
		return nil
	}

	logger.Infof("Found %d total HID device(s):", len(allDevices))
	for i, device := range allDevices {
		logger.Infof("  %d. %s", i+1, device.Product)
		logger.Infof("     Manufacturer: %s", device.Manufacturer)
		logger.Infof("     Vendor ID: 0x%04x, Product ID: 0x%04x", device.VendorID, device.ProductID)
		logger.Infof("     Path: %s", device.Path)
		logger.Infof("     Usage Page: %d, Usage: %d", device.UsagePage, device.Usage)
		logger.Infof("     Serial: %s", device.Serial)
		if i < len(allDevices)-1 {
			logger.Info("")
		}
	}

	// Now show potential scanners
	logger.Info("\n--- Potential Barcode Scanners ---")
	scannerDevices := scanner.ListDevices()
	if len(scannerDevices) == 0 {
		logger.Info("No devices match barcode scanner criteria")
		logger.Info("Criteria: Usage Page 1 + Usage 6 (keyboard) OR product name contains scanner keywords")
	} else {
		logger.Infof("Found %d potential barcode scanner device(s):", len(scannerDevices))
		for i, device := range scannerDevices {
			logger.Infof("  %d. %s", i+1, device.Product)
			logger.Infof("     Vendor ID: 0x%04x, Product ID: 0x%04x", device.VendorID, device.ProductID)
		}
	}

	return nil
}
