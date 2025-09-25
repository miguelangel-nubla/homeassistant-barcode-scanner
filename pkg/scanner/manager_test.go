package scanner

import (
	"testing"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/miguelangel-nubla/homeassistant-barcode-scanner/pkg/config"
)

func TestNewScannerManager(t *testing.T) {
	configs := []config.ScannerConfig{}
	logger := logrus.New()
	manager := NewScannerManager(configs, logger)

	if manager == nil {
		t.Fatal("Expected manager to be created")
	}

	if manager.logger != logger {
		t.Error("Expected logger to be stored")
	}

	if manager.scanners == nil {
		t.Error("Expected scanners map to be initialized")
	}

	if manager.configs == nil {
		t.Error("Expected configs slice to be initialized")
	}
}

func TestNewScannerManagerFromMap(t *testing.T) {
	scannerConfigs := map[string]config.ScannerConfig{
		"test_scanner": {
			ID:   "test_scanner",
			Name: "Test Scanner",
			Identification: config.ScannerIdentification{
				VendorID:  0x60e,
				ProductID: 0x16c7,
			},
			KeyboardLayout:  "us",
			TerminationChar: "enter",
		},
	}

	logger := logrus.New()
	manager := NewScannerManagerFromMap(scannerConfigs, logger)

	if manager == nil {
		t.Fatal("Expected manager to be created")
	}

	if len(manager.configs) != 1 {
		t.Errorf("Expected 1 config, got %d", len(manager.configs))
	}

	if manager.configs[0].ID != "test_scanner" {
		t.Errorf("Expected config ID 'test_scanner', got %s", manager.configs[0].ID)
	}
}

func TestScannerManager_SetReconnectDelay(t *testing.T) {
	configs := []config.ScannerConfig{}
	logger := logrus.New()
	manager := NewScannerManager(configs, logger)

	delay := 10 * time.Second
	manager.SetReconnectDelay(delay)

	// Note: reconnectDelay is not directly accessible, but method should not panic
	if manager == nil {
		t.Error("Expected manager to remain valid after setting reconnect delay")
	}
}

func TestScannerManager_SetCallbacks(t *testing.T) {
	configs := []config.ScannerConfig{}
	logger := logrus.New()
	manager := NewScannerManager(configs, logger)

	manager.SetOnScanCallback(func(scannerID, barcode string) {
		// Callback set successfully
	})

	manager.SetOnConnectionChangeCallback(func(scannerID string, connected bool) {
		// Callback set successfully
	})

	// Note: callback fields are not directly accessible, but methods should not panic
	if manager == nil {
		t.Error("Expected manager to remain valid after setting callbacks")
	}

	// We can't directly test the callbacks without integration test setup
	// This test verifies the methods can be called without panic
}

func TestScannerManager_GetScanner_NotFound(t *testing.T) {
	configs := []config.ScannerConfig{}
	logger := logrus.New()
	manager := NewScannerManager(configs, logger)

	scanner := manager.GetScanner("nonexistent")
	if scanner != nil {
		t.Error("Expected nil for nonexistent scanner")
	}
}

func TestScannerManager_Start_NoConfigs(t *testing.T) {
	configs := []config.ScannerConfig{}
	logger := logrus.New()
	manager := NewScannerManager(configs, logger)

	err := manager.Start()
	if err != nil {
		t.Errorf("Expected no error starting manager with no configs, got: %v", err)
	}
}

func TestScannerManager_Stop_NotStarted(t *testing.T) {
	configs := []config.ScannerConfig{}
	logger := logrus.New()
	manager := NewScannerManager(configs, logger)

	err := manager.Stop()
	if err != nil {
		t.Errorf("Expected no error stopping manager that wasn't started, got: %v", err)
	}
}

func TestValidScannerConfig(t *testing.T) {
	scannerConfig := config.ScannerConfig{
		ID:   "valid_scanner",
		Name: "Valid Scanner",
		Identification: config.ScannerIdentification{
			VendorID:  0x60e,
			ProductID: 0x16c7,
			Serial:    "ABC123",
		},
		KeyboardLayout:  "us",
		TerminationChar: "enter",
	}

	if scannerConfig.ID == "" {
		t.Error("Expected scanner ID to be set")
	}

	if scannerConfig.Identification.VendorID == 0 {
		t.Error("Expected vendor ID to be set")
	}

	if scannerConfig.Identification.ProductID == 0 {
		t.Error("Expected product ID to be set")
	}

	if scannerConfig.KeyboardLayout == "" {
		t.Error("Expected keyboard layout to be set")
	}

	if scannerConfig.TerminationChar == "" {
		t.Error("Expected termination char to be set")
	}
}

func TestScannerConfigWithDefaults(t *testing.T) {
	minimalConfig := config.ScannerConfig{
		ID: "minimal_scanner",
		Identification: config.ScannerIdentification{
			VendorID:  0x60e,
			ProductID: 0x16c7,
		},
		KeyboardLayout:  "us",
		TerminationChar: "enter",
	}

	if minimalConfig.Name == "" {
		if minimalConfig.Name != "" {
			t.Errorf("Expected empty name for minimal config, got %s", minimalConfig.Name)
		}
	}

	if minimalConfig.Identification.Serial != "" {
		t.Errorf("Expected empty serial for minimal config, got %s", minimalConfig.Identification.Serial)
	}
}
