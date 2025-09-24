package scanner

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/karalabe/hid"
	"github.com/sirupsen/logrus"
)

// BarcodeScanner represents a USB HID barcode scanner
type BarcodeScanner struct {
	device              *hid.Device
	logger              *logrus.Logger
	vendorID            uint16
	productID           uint16
	requiredSerial      string
	stopCh              chan struct{}
	reconnectDelay      time.Duration
	isReconnecting      bool
	reconnectMutex      sync.RWMutex
	onConnectionChange  func(bool)
	connectedDeviceInfo *hid.DeviceInfo
	// Components
	hidProcessor  *HIDProcessor
	deviceMonitor *DeviceMonitor
}

// NewBarcodeScanner creates a new barcode scanner instance
func NewBarcodeScanner(vendorID, productID uint16, terminationChar string, logger *logrus.Logger) *BarcodeScanner {
	return NewBarcodeScannerWithSerial(vendorID, productID, "", terminationChar, logger)
}

// NewBarcodeScannerWithSerial creates a new barcode scanner instance with serial number requirement
func NewBarcodeScannerWithSerial(
	vendorID, productID uint16,
	requiredSerial, terminationChar string,
	logger *logrus.Logger,
) *BarcodeScanner {
	s := &BarcodeScanner{
		vendorID:       vendorID,
		productID:      productID,
		requiredSerial: requiredSerial,
		logger:         logger,
		stopCh:         make(chan struct{}),
		reconnectDelay: 1 * time.Second,
	}

	// Initialize HID processor
	s.hidProcessor = NewHIDProcessor(terminationChar, logger)

	// Initialize device monitor with target checker
	s.deviceMonitor = NewDeviceMonitor(s.isTargetDevice, logger)

	return s
}

// SetOnScanCallback sets the callback function to be called when a barcode is scanned
func (s *BarcodeScanner) SetOnScanCallback(callback func(string)) {
	s.hidProcessor.SetOnScanCallback(callback)
}

// SetOnConnectionChangeCallback sets the callback function to be called when connection state changes
func (s *BarcodeScanner) SetOnConnectionChangeCallback(callback func(bool)) {
	s.onConnectionChange = callback
}

// Start begins listening for barcode scans with reactive auto-reconnection
func (s *BarcodeScanner) Start() error {
	// Start device monitoring
	s.deviceMonitor.Start()

	device, err := s.openDevice()
	if err != nil {
		return err
	}
	s.device = device

	// Notify initial connection state
	if s.onConnectionChange != nil {
		s.logger.Debugf("Notifying connection change callback: connected=true")
		s.onConnectionChange(true)
	} else {
		s.logger.Warnf("No connection change callback set when scanner connected")
	}

	go s.readLoopWithReconnect()
	s.logger.Info("Barcode scanner started successfully")
	return nil
}

// openDevice finds and opens a suitable HID device
func (s *BarcodeScanner) openDevice() (*hid.Device, error) {
	devices := hid.Enumerate(0, 0)
	s.logger.Debugf("Found %d total HID devices", len(devices))

	if len(devices) == 0 {
		return nil, fmt.Errorf("no HID devices found")
	}

	// Look for target device by VID/PID and optional serial
	if s.vendorID != 0 && s.productID != 0 {
		s.logger.Debugf("Looking for device with VID:PID %04x:%04x", s.vendorID, s.productID)
		if s.requiredSerial != "" {
			s.logger.Debugf("Also requiring serial: %s", s.requiredSerial)
		}

		for _, deviceInfo := range devices {
			if !s.isTargetDevice(&deviceInfo) {
				continue
			}

			s.logger.Debugf("Found matching device %04x:%04x (%s - %s), attempting to open",
				deviceInfo.VendorID, deviceInfo.ProductID, deviceInfo.Manufacturer, deviceInfo.Product)
			device, err := deviceInfo.Open()
			if err != nil {
				return nil, fmt.Errorf("failed to open device %04x:%04x: %w", s.vendorID, s.productID, err)
			}
			s.logger.Infof("Opened device %04x:%04x (%s)", s.vendorID, s.productID, deviceInfo.Product)
			s.connectedDeviceInfo = s.normalizeDeviceInfo(&deviceInfo)
			return device, nil
		}

		if s.requiredSerial != "" {
			return nil, fmt.Errorf("device %04x:%04x with serial '%s' not found", s.vendorID, s.productID, s.requiredSerial)
		} else {
			return nil, fmt.Errorf("device %04x:%04x not found", s.vendorID, s.productID)
		}
	}

	return nil, fmt.Errorf("device %04x:%04x not found", s.vendorID, s.productID)
}

// Stop stops the barcode scanner
func (s *BarcodeScanner) Stop() error {
	close(s.stopCh)
	s.deviceMonitor.Stop()
	return s.closeDevice()
}

// closeDevice safely closes the current device
func (s *BarcodeScanner) closeDevice() error {
	s.reconnectMutex.Lock()
	defer s.reconnectMutex.Unlock()

	if s.device != nil {
		if err := s.device.Close(); err != nil {
			s.logger.Errorf("Error closing HID device: %v", err)
			s.device = nil
			s.connectedDeviceInfo = nil
			return fmt.Errorf("failed to close HID device: %w", err)
		}
		s.device = nil
		s.connectedDeviceInfo = nil
		s.logger.Debug("HID device closed successfully")
	}

	s.logger.Info("Barcode scanner stopped")
	return nil
}

// isTargetDevice checks if a device matches our target criteria
func (s *BarcodeScanner) isTargetDevice(deviceInfo *hid.DeviceInfo) bool {
	// Check vendor/product ID
	if deviceInfo.VendorID != s.vendorID || deviceInfo.ProductID != s.productID {
		return false
	}

	// If serial is required, check it too
	if s.requiredSerial != "" {
		return deviceInfo.Serial == s.requiredSerial
	}

	return true
}

// attemptReconnection tries to reconnect to the scanner device
func (s *BarcodeScanner) attemptReconnection() error {
	s.reconnectMutex.Lock()
	defer s.reconnectMutex.Unlock()

	if s.isReconnecting {
		s.logger.Debug("Reconnection already in progress")
		return fmt.Errorf("reconnection already in progress")
	}

	s.isReconnecting = true
	defer func() {
		s.isReconnecting = false
	}()

	s.logger.Info("Waiting for device to become available...")

	for {
		select {
		case <-s.stopCh:
			s.logger.Debug("Stop signal received during reconnection")
			return fmt.Errorf("scanner stopped during reconnection")
		case available := <-s.deviceMonitor.Changes():
			if available {
				s.logger.Info("Device detected, attempting to reconnect...")
				device, err := s.openDevice()
				if err != nil {
					s.logger.Debugf("Failed to open detected device: %v", err)
					continue
				}

				s.device = device
				s.logger.Info("Scanner reconnected successfully")
				if s.onConnectionChange != nil {
					s.onConnectionChange(true)
				}
				return nil
			}
		}
	}
}

// IsConnected returns true if the scanner device is connected
func (s *BarcodeScanner) IsConnected() bool {
	s.reconnectMutex.RLock()
	defer s.reconnectMutex.RUnlock()
	return s.device != nil
}

// GetConnectedDeviceInfo returns the connected device info if available
func (s *BarcodeScanner) GetConnectedDeviceInfo() *hid.DeviceInfo {
	s.reconnectMutex.RLock()
	defer s.reconnectMutex.RUnlock()
	return s.connectedDeviceInfo
}

// normalizeDeviceInfo cleans up device information from HID library
func (s *BarcodeScanner) normalizeDeviceInfo(deviceInfo *hid.DeviceInfo) *hid.DeviceInfo {
	normalized := *deviceInfo // Copy the struct
	normalized.Manufacturer = strings.TrimSpace(normalized.Manufacturer)
	normalized.Product = strings.TrimSpace(normalized.Product)
	normalized.Serial = strings.TrimSpace(normalized.Serial)
	return &normalized
}

// readLoopWithReconnect continuously reads from the HID device with auto-reconnection
func (s *BarcodeScanner) readLoopWithReconnect() {
	defer s.logger.Info("Scanner read loop with auto-reconnection stopped")

	for {
		select {
		case <-s.stopCh:
			return
		default:
			if s.device == nil {
				s.logger.Debug("No device available, attempting reconnection...")
				if err := s.attemptReconnection(); err != nil {
					time.Sleep(s.reconnectDelay)
					continue
				}
			}

			if !s.readLoop() {
				// Device disconnected, close and prepare for reconnection
				if err := s.closeDevice(); err != nil {
					s.logger.Errorf("Error closing device during reconnection: %v", err)
				}
				// Notify disconnection
				if s.onConnectionChange != nil {
					s.onConnectionChange(false)
				}
				s.logger.Info("Scanner disconnected, will attempt reconnection...")
			}
		}
	}
}

// readLoop continuously reads from the HID device
// Returns false if device is disconnected and needs reconnection
func (s *BarcodeScanner) readLoop() bool {
	s.logger.Debug("Starting scanner read loop")
	buffer := make([]byte, 64)
	stats := &readStats{}

	// Start timeout checker in separate goroutine
	go s.timeoutChecker()

	for {
		select {
		case <-s.stopCh:
			s.logger.Debug("Stop signal received in read loop")
			return true // Clean shutdown
		default:
			if s.device == nil {
				s.logger.Debug("Device is nil in read loop")
				return false // Need reconnection
			}

			if !s.processDeviceRead(buffer, stats) {
				return false // Need reconnection
			}

			// Short sleep to prevent tight loop
			time.Sleep(1 * time.Millisecond)
		}
	}
}

type readStats struct {
	readCount         int
	errorCount        int
	consecutiveErrors int
}

func (s *BarcodeScanner) processDeviceRead(buffer []byte, stats *readStats) bool {
	n, err := s.device.Read(buffer)
	stats.readCount++

	if err != nil {
		return s.handleReadError(err, stats)
	}

	stats.consecutiveErrors = 0
	return s.handleReadData(buffer[:n], stats)
}

func (s *BarcodeScanner) handleReadError(err error, stats *readStats) bool {
	stats.errorCount++
	stats.consecutiveErrors++
	s.logger.Debugf("Error reading from HID device (read #%d, error #%d): %v",
		stats.readCount, stats.errorCount, err)

	const maxConsecutiveErrors = 10
	if stats.consecutiveErrors >= maxConsecutiveErrors {
		s.logger.Warn("Too many consecutive read errors, assuming device disconnected")
		return false
	}

	time.Sleep(100 * time.Millisecond)
	return true
}

func (s *BarcodeScanner) handleReadData(data []byte, stats *readStats) bool {
	if len(data) == 0 {
		const logInterval = 1000
		if stats.readCount%logInterval == 0 {
			s.logger.Debugf("No data received in read #%d", stats.readCount)
		}
		return true
	}

	s.logger.Debugf("Read %d bytes from HID device (read #%d): %x", len(data), stats.readCount, data)

	if s.hasMeaningfulData(data) {
		s.hidProcessor.ProcessData(data)
	} else {
		s.logger.Debugf("Ignoring empty HID report (all zeros)")
	}
	return true
}

func (s *BarcodeScanner) hasMeaningfulData(data []byte) bool {
	for _, b := range data {
		if b != 0 {
			return true
		}
	}
	return false
}

// timeoutChecker runs in a separate goroutine to check for barcode completion timeouts
func (s *BarcodeScanner) timeoutChecker() {
	defer s.logger.Debug("Timeout checker stopped")

	const timeoutCheckInterval = 10 * time.Millisecond
	ticker := time.NewTicker(timeoutCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.hidProcessor.CheckTimeout()
		}
	}
}

// SetReconnectDelay sets the delay between reconnection attempts
func (s *BarcodeScanner) SetReconnectDelay(delay time.Duration) {
	s.reconnectDelay = delay
	s.logger.Debugf("Scanner reconnect delay set to %v", delay)
}

// ListAllDevices returns a list of all available HID devices
func ListAllDevices() []hid.DeviceInfo {
	return hid.Enumerate(0, 0)
}
