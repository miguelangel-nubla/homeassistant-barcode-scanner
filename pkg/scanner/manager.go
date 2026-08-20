package scanner

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/miguelangel-nubla/homeassistant-barcode-scanner/pkg/config"
)

const defaultReconnectDelay = time.Second

type ScannerManager struct {
	scanners             map[string]*BarcodeScanner
	configs              []config.ScannerConfig
	logger               *logrus.Logger
	onScanCallback       func(scannerID, barcode string)
	onConnectionCallback func(scannerID string, connected bool)
	reconnectDelay       time.Duration
	mutex                sync.RWMutex
}

func NewScannerManager(configs []config.ScannerConfig, logger *logrus.Logger) *ScannerManager {
	return &ScannerManager{
		scanners:       make(map[string]*BarcodeScanner),
		configs:        configs,
		logger:         logger,
		reconnectDelay: defaultReconnectDelay,
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
	sm.mutex.Lock()
	sm.onScanCallback = callback
	sm.mutex.Unlock()
}

func (sm *ScannerManager) SetOnConnectionChangeCallback(callback func(scannerID string, connected bool)) {
	sm.mutex.Lock()
	sm.onConnectionCallback = callback
	sm.mutex.Unlock()
}

// SetReconnectDelay sets the reconnect delay for all current and future
// scanners.
func (sm *ScannerManager) SetReconnectDelay(delay time.Duration) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	sm.reconnectDelay = delay
	for _, scanner := range sm.scanners {
		scanner.SetReconnectDelay(delay)
	}
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

func (sm *ScannerManager) GetScanner(id string) *BarcodeScanner {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()
	return sm.scanners[id]
}

func (sm *ScannerManager) startScanner(cfg *config.ScannerConfig) error {
	sm.logger.Debugf("Starting scanner: %s", cfg.ID)

	scanner := NewBarcodeScanner(
		cfg.Identification,
		cfg.TerminationChar,
		cfg.KeyboardLayout,
		sm.logger,
	)

	scannerID := cfg.ID
	scanner.SetOnScanCallback(func(barcode string) {
		sm.mutex.RLock()
		callback := sm.onScanCallback
		sm.mutex.RUnlock()

		if callback != nil {
			callback(scannerID, barcode)
		}
	})

	scanner.SetOnConnectionChangeCallback(func(connected bool) {
		sm.mutex.RLock()
		callback := sm.onConnectionCallback
		sm.mutex.RUnlock()

		if callback != nil {
			callback(scannerID, connected)
		}
	})

	sm.mutex.Lock()
	scanner.SetReconnectDelay(sm.reconnectDelay)
	sm.scanners[cfg.ID] = scanner
	sm.mutex.Unlock()

	if err := scanner.Start(); err != nil {
		sm.mutex.Lock()
		delete(sm.scanners, cfg.ID)
		sm.mutex.Unlock()
		return fmt.Errorf("failed to start scanner: %w", err)
	}

	sm.logger.Debugf("Scanner %s started successfully", cfg.ID)
	return nil
}

// checkInitialConnections probes all configured devices once at startup. It
// only fails when a device is present but cannot be opened, which indicates
// a permission problem that reconnecting will never solve. Devices that are
// simply unplugged are picked up later by the reconnect loop.
func (sm *ScannerManager) checkInitialConnections() error {
	sm.logger.Info("Checking initial scanner connections...")

	connected := 0
	openFailures := 0

	for _, cfg := range sm.configs {
		err := ProbeDevice(cfg.Identification)
		switch {
		case err == nil:
			sm.logger.Infof("Scanner '%s' (%s) available at startup", cfg.ID, cfg.Name)
			connected++
		case errors.Is(err, ErrDeviceOpenFailed):
			sm.logger.Errorf("Scanner '%s' (%s) present but cannot be opened: %v", cfg.ID, cfg.Name, err)
			openFailures++
		default:
			sm.logger.Warnf("Scanner '%s' (%s) not connected at startup: %v", cfg.ID, cfg.Name, err)
		}
	}

	if openFailures > 0 && connected == 0 {
		return fmt.Errorf(
			"FATAL: %d configured scanner(s) are present but cannot be opened - "+
				"this usually indicates insufficient privileges (udev rules on Linux, privileged mode in containers)",
			openFailures)
	}

	if connected < len(sm.configs) {
		sm.logger.Warnf("Startup check: %d of %d scanner(s) available", connected, len(sm.configs))
		sm.logger.Info("Missing scanners will automatically connect when plugged in")
	} else if len(sm.configs) > 0 {
		sm.logger.Infof("Startup check: All %d configured scanner(s) are available", connected)
	}

	return nil
}
