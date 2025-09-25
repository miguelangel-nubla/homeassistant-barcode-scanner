package scanner

import (
	"fmt"
	"sync"
	"time"

	"github.com/karalabe/hid"
	"github.com/sirupsen/logrus"

	"github.com/miguelangel-nubla/homeassistant-barcode-scanner/pkg/config"
)

type ScannerManager struct {
	scanners             map[string]*BarcodeScanner
	configs              []config.ScannerConfig
	logger               *logrus.Logger
	onScanCallback       func(scannerID, barcode string)
	onConnectionCallback func(scannerID string, connected bool)
	mutex                sync.RWMutex
	stopCh               chan struct{}
}

func NewScannerManager(configs []config.ScannerConfig, logger *logrus.Logger) *ScannerManager {
	return &ScannerManager{
		scanners: make(map[string]*BarcodeScanner),
		configs:  configs,
		logger:   logger,
		stopCh:   make(chan struct{}),
	}
}

func NewScannerManagerFromMap(configMap map[string]config.ScannerConfig, logger *logrus.Logger) *ScannerManager {
	configs := make([]config.ScannerConfig, 0, len(configMap))
	for _, cfg := range configMap {
		configs = append(configs, cfg)
	}
	return NewScannerManager(configs, logger)
}

func (sm *ScannerManager) SetOnScanCallback(callback func(scannerID, barcode string)) {
	sm.onScanCallback = callback
}

func (sm *ScannerManager) SetOnConnectionChangeCallback(callback func(scannerID string, connected bool)) {
	sm.onConnectionCallback = callback
}

func (sm *ScannerManager) Start() error {
	sm.logger.Info("Starting scanner manager...")

	if err := sm.checkInitialConnections(); err != nil {
		return err
	}

	for _, cfg := range sm.configs {
		if err := sm.startScanner(&cfg); err != nil {
			sm.logger.Errorf("Failed to start scanner %s: %v", cfg.ID, err)
		}
	}

	sm.logger.Infof("Scanner manager started with %d active scanners", len(sm.scanners))
	return nil
}

func (sm *ScannerManager) Stop() error {
	close(sm.stopCh)

	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	for id, scanner := range sm.scanners {
		sm.logger.Debugf("Stopping scanner: %s", id)
		if err := scanner.Stop(); err != nil {
			sm.logger.Errorf("Error stopping scanner %s: %v", id, err)
		}
	}

	sm.scanners = make(map[string]*BarcodeScanner)
	sm.logger.Info("All scanners stopped")
	return nil
}

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

func (sm *ScannerManager) GetScanner(id string) *BarcodeScanner {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()
	return sm.scanners[id]
}

func (sm *ScannerManager) startScanner(cfg *config.ScannerConfig) error {
	sm.logger.Debugf("Starting scanner: %s", cfg.ID)

	keyboardLayout := cfg.KeyboardLayout

	scanner := NewBarcodeScannerWithSerial(
		cfg.Identification.VendorID,
		cfg.Identification.ProductID,
		cfg.Identification.Serial,
		cfg.TerminationChar,
		keyboardLayout,
		sm.logger,
	)

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

	sm.mutex.Lock()
	sm.scanners[cfg.ID] = scanner
	sm.mutex.Unlock()
	sm.logger.Debugf("Stored scanner %s in manager before starting", cfg.ID)

	if err := scanner.Start(); err != nil {
		sm.mutex.Lock()
		delete(sm.scanners, cfg.ID)
		sm.mutex.Unlock()
		return fmt.Errorf("failed to start scanner: %w", err)
	}

	sm.logger.Debugf("Scanner %s started successfully", cfg.ID)
	return nil
}

func (sm *ScannerManager) SetReconnectDelay(delay time.Duration) {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	for _, scanner := range sm.scanners {
		scanner.SetReconnectDelay(delay)
	}
}

func (sm *ScannerManager) checkInitialConnections() error {
	sm.logger.Info("Checking initial scanner connections...")

	connected := 0
	disconnected := 0

	for _, cfg := range sm.configs {
		scanner := NewBarcodeScannerWithSerial(
			cfg.Identification.VendorID,
			cfg.Identification.ProductID,
			cfg.Identification.Serial,
			cfg.TerminationChar,
			cfg.KeyboardLayout,
			sm.logger,
		)

		if err := scanner.TryInitialConnect(); err != nil {
			sm.logger.Warnf("Scanner '%s' (%s) not connected at startup: %v", cfg.ID, cfg.Name, err)
			disconnected++
		} else {
			sm.logger.Infof("Scanner '%s' (%s) available at startup", cfg.ID, cfg.Name)
			connected++
		}
	}

	if connected == 0 && len(sm.configs) > 0 {
		return fmt.Errorf("FATAL: None of the %d configured scanners could connect - "+
			"this usually indicates insufficient privileges (privileged mode required for HID device access)",
			len(sm.configs))
	}

	if disconnected > 0 {
		sm.logger.Warnf("Startup check: %d scanner(s) connected, %d scanner(s) not available", connected, disconnected)
		sm.logger.Info("Disconnected scanners will automatically connect when plugged in")
	} else {
		sm.logger.Infof("Startup check: All %d configured scanner(s) are available", connected)
	}

	return nil
}
