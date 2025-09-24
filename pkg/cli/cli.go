package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v3"

	"github.com/miguelangel-nubla/homeassistant-barcode-scanner/pkg/app"
	"github.com/miguelangel-nubla/homeassistant-barcode-scanner/pkg/common"
	"github.com/miguelangel-nubla/homeassistant-barcode-scanner/pkg/config"
	"github.com/miguelangel-nubla/homeassistant-barcode-scanner/pkg/scanner"
)

const AppName = "homeassistant-barcode-scanner"

// CLI manages command-line interface concerns
type CLI struct {
	app    *app.Application
	logger *logrus.Logger
}

// NewCLI creates a new CLI instance
func NewCLI() *CLI {
	return &CLI{}
}

// Run runs the CLI application
func (c *CLI) Run(args []string) error {
	cmd := &cli.Command{
		Name:    AppName,
		Usage:   "USB Barcode Scanner client for Home Assistant",
		Version: common.GetVersion(),
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
		Action: c.runApp,
	}

	return cmd.Run(context.Background(), args)
}

// runApp is the main application entry point
func (c *CLI) runApp(ctx context.Context, cmd *cli.Command) error {
	c.logger = c.setupLogger(cmd)

	// Handle list-devices flag
	if cmd.Bool("list-devices") {
		return c.listDevices()
	}

	// Load and validate configuration
	cfg, err := config.LoadConfig(cmd.String("config"))
	if err != nil {
		return fmt.Errorf("configuration error: %w", err)
	}

	// Apply config logging settings if not overridden by flags
	c.applyConfigLogging(cmd, cfg)

	c.logger.Infof("Starting %s v%s", AppName, common.GetVersion())

	// Create and initialize application
	c.app = app.NewApplication(cfg, c.logger, common.GetVersion())
	if err := c.app.Initialize(); err != nil {
		return fmt.Errorf("failed to initialize application: %w", err)
	}

	// Set up signal handling
	shutdownCh := c.setupSignalHandling()

	// Start application
	if err := c.app.Start(); err != nil {
		return err
	}

	// Wait for shutdown signal
	<-shutdownCh

	// Graceful shutdown
	return c.app.Stop()
}

// setupLogger configures the logger based on CLI flags
func (c *CLI) setupLogger(cmd *cli.Command) *logrus.Logger {
	logger := logrus.New()
	logger.SetFormatter(&logrus.TextFormatter{FullTimestamp: true})

	if level, err := logrus.ParseLevel(cmd.String("log-level")); err == nil {
		logger.SetLevel(level)
	}

	return logger
}

// applyConfigLogging applies logging configuration from config file
func (c *CLI) applyConfigLogging(cmd *cli.Command, cfg *config.Config) {
	// Apply config logging settings if not overridden by flags
	if !cmd.IsSet("log-level") {
		if level, err := logrus.ParseLevel(cfg.Logging.Level); err == nil {
			c.logger.SetLevel(level)
		}
	}
	if cfg.Logging.Format == "json" {
		c.logger.SetFormatter(&logrus.JSONFormatter{})
	}
}

// setupSignalHandling sets up OS signal handling for graceful shutdown
func (c *CLI) setupSignalHandling() <-chan struct{} {
	shutdownCh := make(chan struct{})
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		c.logger.Infof("Received signal: %v", sig)
		close(shutdownCh)
	}()

	return shutdownCh
}

// listDevices lists available HID devices for configuration purposes
func (c *CLI) listDevices() error {
	allDevices := scanner.ListAllDevices()
	if len(allDevices) == 0 {
		c.logger.Warn("No HID devices found - check permissions or udev rules")
		return nil
	}

	c.logger.Infof("Found %d HID device(s):", len(allDevices))
	c.logger.Info("\nUse these details to configure your scanners:")
	c.logger.Info("Configuration format:")
	c.logger.Info("scanners:")
	c.logger.Info("  - id: \"scanner1\"")
	c.logger.Info("    identification:")
	c.logger.Info("      usb_device:")
	c.logger.Info("        vendor_id: 0xVVVV")
	c.logger.Info("        product_id: 0xPPPP")
	c.logger.Info("        serial: \"SERIAL\" # optional, for multiple identical devices")
	c.logger.Info("      # OR for direct device path:")
	c.logger.Info("      # device_path: \"/dev/hidraw0\"")
	c.logger.Info("")

	for i, device := range allDevices {
		c.logger.Infof("%d. %s (%s)", i+1, device.Product, device.Manufacturer)
		c.logger.Infof("   VID:PID: %04x:%04x", device.VendorID, device.ProductID)
		if device.Serial != "" {
			c.logger.Infof("   Serial: %s", device.Serial)
		}
		c.logger.Infof("   Device Path: %s", device.Path)
		c.logger.Info("")
	}

	return nil
}
