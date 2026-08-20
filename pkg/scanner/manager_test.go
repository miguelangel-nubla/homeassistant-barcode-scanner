package scanner

import (
	"testing"
	"time"

	"github.com/miguelangel-nubla/homeassistant-barcode-scanner/pkg/config"
)

func testScannerConfig(id string) config.ScannerConfig {
	return config.ScannerConfig{
		ID:              id,
		Name:            "Test Scanner",
		Identification:  testIdent(),
		KeyboardLayout:  "us",
		TerminationChar: "enter",
	}
}

func TestNewScannerManagerFromMap(t *testing.T) {
	manager := NewScannerManagerFromMap(map[string]config.ScannerConfig{
		"test_scanner": testScannerConfig("test_scanner"),
	}, testLogger())

	if len(manager.configs) != 1 {
		t.Fatalf("expected 1 config, got %d", len(manager.configs))
	}
	if manager.configs[0].ID != "test_scanner" {
		t.Errorf("expected config ID 'test_scanner', got %s", manager.configs[0].ID)
	}
}

func TestScannerManager_ReconnectDelayAppliedToNewScanners(t *testing.T) {
	manager := NewScannerManager(nil, testLogger())
	manager.SetReconnectDelay(9 * time.Second)

	cfg := testScannerConfig("delayed")
	if err := manager.startScanner(&cfg); err != nil {
		t.Fatalf("startScanner() error: %v", err)
	}
	defer func() { _ = manager.Stop() }()

	scanner := manager.GetScanner("delayed")
	if scanner == nil {
		t.Fatal("expected scanner to be registered")
	}
	if got := scanner.getReconnectDelay(); got != 9*time.Second {
		t.Errorf("expected reconnect delay 9s applied to new scanner, got %v", got)
	}
}

func TestScannerManager_ReconnectDelayAppliedToExistingScanners(t *testing.T) {
	manager := NewScannerManager(nil, testLogger())

	cfg := testScannerConfig("existing")
	if err := manager.startScanner(&cfg); err != nil {
		t.Fatalf("startScanner() error: %v", err)
	}
	defer func() { _ = manager.Stop() }()

	manager.SetReconnectDelay(4 * time.Second)

	scanner := manager.GetScanner("existing")
	if got := scanner.getReconnectDelay(); got != 4*time.Second {
		t.Errorf("expected reconnect delay 4s applied to existing scanner, got %v", got)
	}
}

func TestScannerManager_ScanCallbackReceivesScannerID(t *testing.T) {
	manager := NewScannerManager(nil, testLogger())

	type scanEvent struct {
		scannerID string
		barcode   string
	}
	var events []scanEvent
	manager.SetOnScanCallback(func(scannerID, barcode string) {
		events = append(events, scanEvent{scannerID, barcode})
	})

	cfg := testScannerConfig("front_desk")
	if err := manager.startScanner(&cfg); err != nil {
		t.Fatalf("startScanner() error: %v", err)
	}
	defer func() { _ = manager.Stop() }()

	scanner := manager.GetScanner("front_desk")
	typeKeys(scanner.hidProcessor, 0, 0x04, 0x05, 0x06)
	scanner.hidProcessor.ProcessData(report(0, hidKeyEnter))

	if len(events) != 1 {
		t.Fatalf("expected 1 scan event, got %d", len(events))
	}
	if events[0].scannerID != "front_desk" || events[0].barcode != "abc" {
		t.Errorf("expected event {front_desk abc}, got %+v", events[0])
	}
}

func TestScannerManager_GetScanner_NotFound(t *testing.T) {
	manager := NewScannerManager(nil, testLogger())

	if manager.GetScanner("nonexistent") != nil {
		t.Error("expected nil for nonexistent scanner")
	}
}

func TestScannerManager_StartAndStop_NoConfigs(t *testing.T) {
	manager := NewScannerManager(nil, testLogger())

	if err := manager.Start(); err != nil {
		t.Errorf("Start() with no configs error: %v", err)
	}
	if err := manager.Stop(); err != nil {
		t.Errorf("Stop() error: %v", err)
	}
}

func TestScannerManager_StopIsIdempotent(t *testing.T) {
	manager := NewScannerManager(nil, testLogger())

	cfg := testScannerConfig("stopper")
	if err := manager.startScanner(&cfg); err != nil {
		t.Fatalf("startScanner() error: %v", err)
	}

	if err := manager.Stop(); err != nil {
		t.Errorf("first Stop() error: %v", err)
	}
	if err := manager.Stop(); err != nil {
		t.Errorf("second Stop() error: %v", err)
	}

	if manager.GetScanner("stopper") != nil {
		t.Error("expected scanners map to be cleared after Stop")
	}
}

func TestScannerManager_StartWithUnpluggedScannerSucceeds(t *testing.T) {
	// A configured scanner that is not plugged in must not prevent startup;
	// the reconnect loop picks it up later.
	manager := NewScannerManagerFromMap(map[string]config.ScannerConfig{
		"unplugged": testScannerConfig("unplugged"),
	}, testLogger())

	if err := manager.Start(); err != nil {
		t.Fatalf("expected Start() to succeed with unplugged scanner, got: %v", err)
	}
	defer func() { _ = manager.Stop() }()

	if manager.GetScanner("unplugged") == nil {
		t.Error("expected scanner to be registered and waiting for the device")
	}
}
