package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"

	"github.com/karalabe/hid"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v3"

	"github.com/miguelangel-nubla/homeassistant-barcode-scanner/pkg/app"
	"github.com/miguelangel-nubla/homeassistant-barcode-scanner/pkg/common"
	"github.com/miguelangel-nubla/homeassistant-barcode-scanner/pkg/config"
	"github.com/miguelangel-nubla/homeassistant-barcode-scanner/pkg/scanner"
)

const AppName = "homeassistant-barcode-scanner"

type CLI struct {
	app    *app.Application
	logger *logrus.Logger
}

func NewCLI() *CLI {
	return &CLI{}
}

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

func (c *CLI) runApp(ctx context.Context, cmd *cli.Command) error {
	c.logger = c.setupLogger(cmd)

	if cmd.Bool("list-devices") {
		return c.listDevices()
	}

	// If no config file exists at default location and no explicit config provided,
	// show help instead of failing
	configPath := cmd.String("config")
	if !cmd.IsSet("config") && configPath == "config.yaml" {
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			if helpErr := cli.ShowAppHelp(cmd); helpErr != nil {
				return fmt.Errorf("failed to show help: %w", helpErr)
			}
			return fmt.Errorf("no configuration found - create config.yaml or specify with --config")
		}
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("configuration error: %w", err)
	}

	c.applyConfigLogging(cmd, cfg)

	c.logger.Infof("Starting %s %s", AppName, common.GetVersion())

	c.app = app.NewApplication(cfg, c.logger, common.GetVersion())
	if err := c.app.Initialize(); err != nil {
		return fmt.Errorf("failed to initialize application: %w", err)
	}

	shutdownCh := c.setupSignalHandling()

	if err := c.app.Start(); err != nil {
		return err
	}

	<-shutdownCh

	return c.app.Stop()
}

func (c *CLI) setupLogger(cmd *cli.Command) *logrus.Logger {
	logger := logrus.New()
	logger.SetFormatter(&logrus.TextFormatter{FullTimestamp: true})

	if level, err := logrus.ParseLevel(cmd.String("log-level")); err == nil {
		logger.SetLevel(level)
	}

	return logger
}

func (c *CLI) applyConfigLogging(cmd *cli.Command, cfg *config.Config) {
	if !cmd.IsSet("log-level") {
		if level, err := logrus.ParseLevel(cfg.Logging.Level); err == nil {
			c.logger.SetLevel(level)
		}
	}
	if cfg.Logging.Format == "json" {
		c.logger.SetFormatter(&logrus.JSONFormatter{})
	}
}

func (c *CLI) setupSignalHandling() <-chan struct{} {
	shutdownCh := make(chan struct{})
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		c.logger.Warnf("Received signal: %v", sig)
		close(shutdownCh)
	}()

	return shutdownCh
}

// generateScannerID creates a valid YAML key from device info
func generateScannerID(name string, device *hid.DeviceInfo) string {
	// Convert to lowercase and replace spaces/special chars with underscores
	id := strings.ToLower(name)
	// Replace any non-alphanumeric characters with underscores
	reg := regexp.MustCompile(`[^a-z0-9]+`)
	id = reg.ReplaceAllString(id, "_")
	// Remove leading/trailing underscores
	id = strings.Trim(id, "_")

	// If empty or starts with number, prepend "scanner"
	if id == "" || (id != "" && id[0] >= '0' && id[0] <= '9') {
		id = fmt.Sprintf("scanner_%s", id)
	}
	// If still empty, use fallback
	if id == "" || id == "scanner_" {
		id = "scanner"
	}

	// Add interface index for same VID:PID devices (only if > 0)
	if device.Interface > 0 {
		id = fmt.Sprintf("%s_%d", id, device.Interface)
	}

	// Add serial suffix if available (for additional uniqueness)
	if device.Serial != "" {
		serialSuffix := reg.ReplaceAllString(strings.ToLower(device.Serial), "_")
		serialSuffix = strings.Trim(serialSuffix, "_")
		if serialSuffix != "" {
			id = fmt.Sprintf("%s_%s", id, serialSuffix)
		}
	}

	return id
}

func (c *CLI) listDevices() error {
	allDevices := scanner.ListAllDevices()
	if len(allDevices) == 0 {
		fmt.Println("# No HID devices found - check permissions or udev rules")
		return nil
	}

	fmt.Println("scanners:")

	for _, device := range allDevices {
		// Generate a friendly name
		name := device.Product
		if name == "" {
			name = "Unknown Device"
		}
		if device.Manufacturer != "" && device.Manufacturer != name {
			name = fmt.Sprintf("%s %s", device.Manufacturer, name)
		}

		// Generate scanner ID based on device info
		scannerID := generateScannerID(name, &device)

		fmt.Printf("  %s:\n", scannerID)

		// Add comments for additional info not needed in config
		fmt.Printf("    # Device Path: %s\n", device.Path)

		if device.Manufacturer != "" {
			fmt.Printf("    # Manufacturer: %s\n", device.Manufacturer)
		}
		if device.Product != "" {
			fmt.Printf("    # Product: %s\n", device.Product)
		}

		if device.Interface > 0 {
			fmt.Printf("    # Note: Multiple interfaces found for device %04x:%04x (serial: %s).\n",
				device.VendorID, device.ProductID, device.Serial)
			fmt.Printf("    # Test which interface responds to scans.\n")
		}

		fmt.Printf("    name: \"%s\"\n", name)
		fmt.Printf("    identification:\n")
		fmt.Printf("      vendor_id: 0x%04x\n", device.VendorID)
		fmt.Printf("      product_id: 0x%04x\n", device.ProductID)
		if device.Serial != "" {
			fmt.Printf("      serial: \"%s\"\n", device.Serial)
		}
		if device.Interface > 0 {
			fmt.Printf("      interface: %d  # Specify which interface to use\n", device.Interface)
		}
		fmt.Printf("    termination_char: \"tab\"  # Options: enter, tab, none\n")

		fmt.Println()
	}

	return nil
}
