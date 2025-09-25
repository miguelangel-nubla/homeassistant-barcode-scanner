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

	c.logger.Infof("Starting %s v%s", AppName, common.GetVersion())

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
		c.logger.Infof("Received signal: %v", sig)
		close(shutdownCh)
	}()

	return shutdownCh
}

func (c *CLI) listDevices() error {
	allDevices := scanner.ListAllDevices()
	if len(allDevices) == 0 {
		fmt.Println("No HID devices found - check permissions or udev rules")
		return nil
	}

	fmt.Printf("Found %d HID device(s):\n\n", len(allDevices))
	fmt.Println("Use these details to configure your scanners:")
	fmt.Println("Configuration format:")
	fmt.Println("scanners:")
	fmt.Println("  - id: \"scanner1\"")
	fmt.Println("    identification:")
	fmt.Println("      usb_device:")
	fmt.Println("        vendor_id: 0xVVVV")
	fmt.Println("        product_id: 0xPPPP")
	fmt.Println("        serial: \"SERIAL\" # optional, for multiple identical devices")
	fmt.Println("      # OR for direct device path:")
	fmt.Println("      # device_path: \"/dev/hidraw0\"")
	fmt.Println("")

	for i, device := range allDevices {
		fmt.Printf("%d. %s (%s)\n", i+1, device.Product, device.Manufacturer)
		fmt.Printf("   VID:PID: %04x:%04x\n", device.VendorID, device.ProductID)
		if device.Serial != "" {
			fmt.Printf("   Serial: %s\n", device.Serial)
		}
		fmt.Printf("   Device Path: %s\n", device.Path)
		fmt.Println("")
	}

	return nil
}
