package scanner

import (
	"fmt"
	"sync"
	"time"

	"github.com/karalabe/hid"
	"github.com/sirupsen/logrus"

	"github.com/miguelangel-nubla/homeassistant-barcode-scanner/pkg/config"
)

// ScannerManager manages multiple barcode scanner instances
type ScannerManager struct {
	scanners             map[string]*BarcodeScanner
	configs              []config.ScannerConfig
	logger               *logrus.Logger
	onScanCallback       func(scannerID, barcode string)
	onConnectionCallback func(scannerID string, connected bool)
	mutex                sync.RWMutex
	stopCh               chan struct{}
}

// NewScannerManager creates a new scanner manager
func NewScannerManager(configs []config.ScannerConfig, logger *logrus.Logger) *ScannerManager {
	return &ScannerManager{
		scanners: make(map[string]*BarcodeScanner),
		configs:  configs,
		logger:   logger,
		stopCh:   make(chan struct{}),
	}
}

// SetOnScanCallback sets the callback for barcode scan events
func (sm *ScannerManager) SetOnScanCallback(callback func(scannerID, barcode string)) {
	sm.onScanCallback = callback
}

// SetOnConnectionChangeCallback sets the callback for connection state changes
func (sm *ScannerManager) SetOnConnectionChangeCallback(callback func(scannerID string, connected bool)) {
	sm.onConnectionCallback = callback
}

// Start starts all configured scanners
func (sm *ScannerManager) Start() error {
	sm.logger.Info("Starting scanner manager...")

	for _, cfg := range sm.configs {
		if err := sm.startScanner(cfg); err != nil {
			sm.logger.Errorf("Failed to start scanner %s: %v", cfg.ID, err)
			// Continue with other scanners - don't fail completely
		}
	}

	sm.logger.Infof("Scanner manager started with %d active scanners", len(sm.scanners))
	return nil
}

// Stop stops all scanners
func (sm *ScannerManager) Stop() error {
	close(sm.stopCh)

	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	for id, scanner := range sm.scanners {
		sm.logger.Infof("Stopping scanner: %s", id)
		if err := scanner.Stop(); err != nil {
			sm.logger.Errorf("Error stopping scanner %s: %v", id, err)
		}
	}

	sm.scanners = make(map[string]*BarcodeScanner)
	sm.logger.Info("All scanners stopped")
	return nil
}

// GetConnectedScanners returns information about all connected scanners
func (sm *ScannerManager) GetConnectedScanners() map[string]*hid.DeviceInfo {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	connected := make(map[string]*hid.DeviceInfo)
	for id, scanner := range sm.scanners {
		if scanner.IsConnected() {
			if deviceInfo := scanner.GetConnectedDeviceInfo(); deviceInfo != nil {
				connected[id] = deviceInfo
			}
		}
	}
	return connected
}

// GetScanner returns a specific scanner by ID
func (sm *ScannerManager) GetScanner(id string) *BarcodeScanner {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()
	return sm.scanners[id]
}

// startScanner initializes and starts a single scanner
func (sm *ScannerManager) startScanner(cfg config.ScannerConfig) error {
	sm.logger.Infof("Starting scanner: %s", cfg.ID)

	// Create scanner using VID/PID identification
	scanner := NewBarcodeScannerWithSerial(
		cfg.Identification.VendorID,
		cfg.Identification.ProductID,
		cfg.Identification.Serial,
		cfg.TerminationChar,
		sm.logger,
	)

	// Set callbacks that include scanner ID
	scanner.SetOnScanCallback(func(barcode string) {
		if sm.onScanCallback != nil {
			sm.onScanCallback(cfg.ID, barcode)
		}
	})

	scanner.SetOnConnectionChangeCallback(func(connected bool) {
		if sm.onConnectionCallback != nil {
			sm.onConnectionCallback(cfg.ID, connected)
		}
	})

	// Store scanner in map BEFORE starting it (to avoid race with callback)
	sm.mutex.Lock()
	sm.scanners[cfg.ID] = scanner
	sm.mutex.Unlock()
	sm.logger.Debugf("Stored scanner %s in manager before starting", cfg.ID)

	// Start the scanner (this will trigger connection callback)
	if err := scanner.Start(); err != nil {
		// Remove from map if start fails
		sm.mutex.Lock()
		delete(sm.scanners, cfg.ID)
		sm.mutex.Unlock()
		return fmt.Errorf("failed to start scanner: %w", err)
	}

	sm.logger.Infof("Scanner %s started successfully", cfg.ID)
	return nil
}

// SetReconnectDelay sets the reconnection delay for all scanners
func (sm *ScannerManager) SetReconnectDelay(delay time.Duration) {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	for _, scanner := range sm.scanners {
		scanner.SetReconnectDelay(delay)
	}
}
