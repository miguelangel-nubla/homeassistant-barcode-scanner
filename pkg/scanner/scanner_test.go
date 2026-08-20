package scanner

import (
	"testing"
	"time"

	"github.com/miguelangel-nubla/homeassistant-barcode-scanner/pkg/config"
)

// testIdent uses a VID:PID that no real device should have, so connection
// attempts in tests never succeed.
func testIdent() config.ScannerIdentification {
	return config.ScannerIdentification{VendorID: 0xFFFF, ProductID: 0xFFFF}
}

func TestNewBarcodeScanner(t *testing.T) {
	s := NewBarcodeScanner(testIdent(), "enter", "us", testLogger())

	if s.IsConnected() {
		t.Error("expected scanner to start disconnected")
	}
	if s.GetConnectedDeviceInfo() != nil {
		t.Error("expected no device info before connection")
	}
	if s.getReconnectDelay() != time.Second {
		t.Errorf("expected default reconnect delay 1s, got %v", s.getReconnectDelay())
	}
}

func TestBarcodeScanner_SetReconnectDelay(t *testing.T) {
	s := NewBarcodeScanner(testIdent(), "enter", "us", testLogger())

	s.SetReconnectDelay(7 * time.Second)
	if s.getReconnectDelay() != 7*time.Second {
		t.Errorf("expected reconnect delay 7s, got %v", s.getReconnectDelay())
	}
}

func TestBarcodeScanner_ScanCallbackWiring(t *testing.T) {
	s := NewBarcodeScanner(testIdent(), "enter", "us", testLogger())

	var scanned []string
	s.SetOnScanCallback(func(barcode string) {
		scanned = append(scanned, barcode)
	})

	// Feed reports directly into the processor to verify the wiring from
	// processor to scanner callback without hardware.
	typeKeys(s.hidProcessor, 0, 0x04, 0x05)
	s.hidProcessor.ProcessData(report(0, hidKeyEnter))

	if len(scanned) != 1 || scanned[0] != barcodeAB {
		t.Fatalf("expected scan 'ab' through scanner callback, got %v", scanned)
	}
}

func TestBarcodeScanner_StopIsIdempotent(t *testing.T) {
	s := NewBarcodeScanner(testIdent(), "enter", "us", testLogger())

	if err := s.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	if err := s.Stop(); err != nil {
		t.Errorf("first Stop() error: %v", err)
	}
	if err := s.Stop(); err != nil {
		t.Errorf("second Stop() error: %v", err)
	}

	if s.IsConnected() {
		t.Error("expected scanner to be disconnected after Stop")
	}
}

func TestProbeDevice_NotFound(t *testing.T) {
	err := ProbeDevice(testIdent())
	if err == nil {
		t.Skip("a device with VID:PID ffff:ffff is actually present")
	}
}
