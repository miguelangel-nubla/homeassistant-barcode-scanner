package scanner

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/karalabe/hid"
	"github.com/sirupsen/logrus"
)

// BarcodeScanner represents a USB HID barcode scanner
type BarcodeScanner struct {
	// Device identification
	vendorID       uint16
	productID      uint16
	requiredSerial string

	// Connection state
	device     *hid.Device
	deviceInfo *hid.DeviceInfo
	connected  int32 // atomic

	// Configuration
	reconnectDelay time.Duration
	logger         *logrus.Logger

	// Callbacks
	onScan             func(string)
	onConnectionChange func(bool)

	// Control
	ctx    context.Context
	cancel context.CancelFunc
	mutex  sync.RWMutex

	// Processing
	hidProcessor *HIDProcessor
}

// NewBarcodeScanner creates a new barcode scanner instance
func NewBarcodeScanner(vendorID, productID uint16, terminationChar string, logger *logrus.Logger) *BarcodeScanner {
	return NewBarcodeScannerWithSerial(vendorID, productID, "", terminationChar, logger)
}

// NewBarcodeScannerWithSerial creates a new barcode scanner instance with serial number requirement
func NewBarcodeScannerWithSerial(
	vendorID, productID uint16, requiredSerial, terminationChar string, logger *logrus.Logger,
) *BarcodeScanner {
	ctx, cancel := context.WithCancel(context.Background())

	s := &BarcodeScanner{
		vendorID:       vendorID,
		productID:      productID,
		requiredSerial: requiredSerial,
		logger:         logger,
		reconnectDelay: time.Second,
		ctx:            ctx,
		cancel:         cancel,
	}

	s.hidProcessor = NewHIDProcessor(terminationChar, logger)
	s.hidProcessor.SetOnScanCallback(func(barcode string) {
		if s.onScan != nil {
			s.onScan(barcode)
		}
	})

	return s
}

// SetOnScanCallback sets the callback function to be called when a barcode is scanned
func (s *BarcodeScanner) SetOnScanCallback(callback func(string)) {
	s.mutex.Lock()
	s.onScan = callback
	s.mutex.Unlock()
}

// SetOnConnectionChangeCallback sets the callback function to be called when connection state changes
func (s *BarcodeScanner) SetOnConnectionChangeCallback(callback func(bool)) {
	s.mutex.Lock()
	s.onConnectionChange = callback
	s.mutex.Unlock()
}

// Start begins listening for barcode scans with automatic reconnection
func (s *BarcodeScanner) Start() error {
	go s.connectionManager()
	s.logger.Info("Barcode scanner started successfully")
	return nil
}

// findAndOpenDevice finds and opens a suitable HID device
func (s *BarcodeScanner) findAndOpenDevice() (*hid.Device, *hid.DeviceInfo, error) {
	devices := hid.Enumerate(s.vendorID, s.productID)

	for _, deviceInfo := range devices {
		if !s.isTargetDevice(&deviceInfo) {
			continue
		}

		device, err := deviceInfo.Open()
		if err != nil {
			continue // Try next device
		}

		normalizedInfo := s.normalizeDeviceInfo(&deviceInfo)
		return device, normalizedInfo, nil
	}

	if s.requiredSerial != "" {
		return nil, nil, fmt.Errorf("device %04x:%04x with serial '%s' not found", s.vendorID, s.productID, s.requiredSerial)
	}
	return nil, nil, fmt.Errorf("device %04x:%04x not found", s.vendorID, s.productID)
}

// Stop stops the barcode scanner
func (s *BarcodeScanner) Stop() error {
	s.cancel()

	// Close device safely
	s.mutex.Lock()
	device := s.device
	s.device = nil
	s.deviceInfo = nil
	atomic.StoreInt32(&s.connected, 0)
	s.mutex.Unlock()

	if device != nil {
		if err := device.Close(); err != nil {
			s.logger.Warnf("Error closing device: %v", err)
		}
	}

	s.logger.Info("Barcode scanner stopped")
	return nil
}

// connectionManager handles device connection and reconnection
func (s *BarcodeScanner) connectionManager() {
	for {
		select {
		case <-s.ctx.Done():
			return
		default:
			if s.tryConnect() {
				s.runReadLoop()
			}
			// Wait before retry
			select {
			case <-s.ctx.Done():
				return
			case <-time.After(s.reconnectDelay):
			}
		}
	}
}

// tryConnect attempts to connect to the target device
func (s *BarcodeScanner) tryConnect() bool {
	device, deviceInfo, err := s.findAndOpenDevice()
	if err != nil {
		return false
	}

	s.mutex.Lock()
	s.device = device
	s.deviceInfo = deviceInfo
	s.mutex.Unlock()

	atomic.StoreInt32(&s.connected, 1)

	s.mutex.RLock()
	callback := s.onConnectionChange
	s.mutex.RUnlock()

	if callback != nil {
		callback(true)
	}

	s.logger.Infof("Connected to device %04x:%04x (%s)", s.vendorID, s.productID, deviceInfo.Product)
	return true
}

// disconnect safely disconnects the current device
func (s *BarcodeScanner) disconnect() {
	atomic.StoreInt32(&s.connected, 0)

	s.mutex.Lock()
	device := s.device
	s.device = nil
	s.deviceInfo = nil
	s.mutex.Unlock()

	if device != nil {
		if err := device.Close(); err != nil {
			s.logger.Debugf("Error closing device: %v", err)
		}
	}

	s.mutex.RLock()
	callback := s.onConnectionChange
	s.mutex.RUnlock()

	if callback != nil {
		callback(false)
	}

	s.logger.Info("Device disconnected")
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

// runReadLoop runs the main read loop for the connected device
func (s *BarcodeScanner) runReadLoop() {
	const bufferSize = 64
	const tickerInterval = 10 * time.Millisecond

	timeoutTicker := time.NewTicker(tickerInterval)
	defer timeoutTicker.Stop()

	// Channel to receive HID data from read goroutine
	dataChan := make(chan []byte, 10)
	errorChan := make(chan error, 1)

	// Start the HID read goroutine
	go s.hidReadGoroutine(dataChan, errorChan, bufferSize)

	for {
		select {
		case <-s.ctx.Done():
			return

		case <-timeoutTicker.C:
			// Check timeout on every ticker
			s.hidProcessor.CheckTimeout()

		case data := <-dataChan:
			// Process received HID data
			if len(data) > 0 && !s.isAllZeros(data) {
				s.hidProcessor.ProcessData(data)
			}

		case err := <-errorChan:
			// Handle read errors
			s.logger.Debugf("HID read error: %v", err)
			s.disconnect()
			return
		}
	}
}

// hidReadGoroutine runs in a separate goroutine to read HID data
func (s *BarcodeScanner) hidReadGoroutine(dataChan chan<- []byte, errorChan chan<- error, bufferSize int) {
	buffer := make([]byte, bufferSize)

	for {
		select {
		case <-s.ctx.Done():
			return
		default:
			s.mutex.RLock()
			device := s.device
			s.mutex.RUnlock()

			if device == nil {
				errorChan <- fmt.Errorf("device is nil")
				return
			}

			n, err := device.Read(buffer)
			if err != nil {
				// Check if it's a timeout error, which is normal
				if err.Error() == "hid: read timeout" || err.Error() == "hid: timeout" {
					// Timeout is normal, continue reading
					continue
				}
				// Real error occurred
				errorChan <- err
				return
			}

			if n > 0 {
				// Copy the data to send on channel
				data := make([]byte, n)
				copy(data, buffer[:n])
				dataChan <- data
			}
		}
	}
}

// isAllZeros checks if buffer contains only zero bytes
func (s *BarcodeScanner) isAllZeros(data []byte) bool {
	for _, b := range data {
		if b != 0 {
			return false
		}
	}
	return true
}

// IsConnected returns true if the scanner device is connected
func (s *BarcodeScanner) IsConnected() bool {
	return atomic.LoadInt32(&s.connected) == 1
}

// GetConnectedDeviceInfo returns the connected device info if available
func (s *BarcodeScanner) GetConnectedDeviceInfo() *hid.DeviceInfo {
	s.mutex.RLock()
	info := s.deviceInfo
	s.mutex.RUnlock()
	return info
}

// normalizeDeviceInfo cleans up device information from HID library
func (s *BarcodeScanner) normalizeDeviceInfo(deviceInfo *hid.DeviceInfo) *hid.DeviceInfo {
	normalized := *deviceInfo // Copy the struct
	normalized.Manufacturer = strings.TrimSpace(normalized.Manufacturer)
	normalized.Product = strings.TrimSpace(normalized.Product)
	normalized.Serial = strings.TrimSpace(normalized.Serial)
	return &normalized
}

// SetReconnectDelay sets the delay between reconnection attempts
func (s *BarcodeScanner) SetReconnectDelay(delay time.Duration) {
	s.reconnectDelay = delay
}

// ListAllDevices returns a list of all available HID devices
func ListAllDevices() []hid.DeviceInfo {
	return hid.Enumerate(0, 0)
}
