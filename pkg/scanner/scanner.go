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

	"github.com/miguelangel-nubla/homeassistant-barcode-scanner/pkg/config"
)

type BarcodeScanner struct {
	ident config.ScannerIdentification

	device     *hid.Device
	deviceInfo *hid.DeviceInfo
	connected  int32

	reconnectDelay time.Duration
	logger         *logrus.Logger

	onScan             func(string)
	onConnectionChange func(bool)

	ctx    context.Context
	cancel context.CancelFunc
	mutex  sync.RWMutex

	hidProcessor *HIDProcessor
}

func NewBarcodeScanner(
	ident config.ScannerIdentification, terminationChar, keyboardLayout string, logger *logrus.Logger,
) *BarcodeScanner {
	ctx, cancel := context.WithCancel(context.Background())

	s := &BarcodeScanner{
		ident:          ident,
		logger:         logger,
		reconnectDelay: time.Second,
		ctx:            ctx,
		cancel:         cancel,
	}

	s.hidProcessor = NewHIDProcessor(terminationChar, keyboardLayout, logger)
	s.hidProcessor.SetOnScanCallback(func(barcode string) {
		s.mutex.RLock()
		callback := s.onScan
		s.mutex.RUnlock()

		if callback != nil {
			callback(barcode)
		}
	})

	return s
}

func (s *BarcodeScanner) SetOnScanCallback(callback func(string)) {
	s.mutex.Lock()
	s.onScan = callback
	s.mutex.Unlock()
}

func (s *BarcodeScanner) SetOnConnectionChangeCallback(callback func(bool)) {
	s.mutex.Lock()
	s.onConnectionChange = callback
	s.mutex.Unlock()
}

func (s *BarcodeScanner) Start() error {
	go s.connectionManager()
	s.logger.Debug("Barcode scanner started successfully")
	return nil
}

// findAndOpenDevice locates and opens the HID device matching the given
// identification. A device that matches but cannot be opened (typically a
// permission problem) is reported with ErrDeviceOpenFailed.
func findAndOpenDevice(ident config.ScannerIdentification) (*hid.Device, *hid.DeviceInfo, error) {
	devices := hid.Enumerate(ident.VendorID, ident.ProductID)

	var openErr error
	for _, deviceInfo := range devices {
		if !isTargetDevice(ident, &deviceInfo) {
			continue
		}

		device, err := deviceInfo.Open()
		if err != nil {
			openErr = err
			continue // Try next matching device
		}

		return device, normalizeDeviceInfo(&deviceInfo), nil
	}

	desc := fmt.Sprintf("device %04x:%04x", ident.VendorID, ident.ProductID)
	if ident.Serial != "" {
		desc += fmt.Sprintf(" serial '%s'", ident.Serial)
	}
	if ident.Interface != nil {
		desc += fmt.Sprintf(" interface %d", *ident.Interface)
	}

	if openErr != nil {
		return nil, nil, fmt.Errorf("%w: %s: %v", ErrDeviceOpenFailed, desc, openErr)
	}
	return nil, nil, fmt.Errorf("%s not found", desc)
}

func isTargetDevice(ident config.ScannerIdentification, deviceInfo *hid.DeviceInfo) bool {
	if deviceInfo.VendorID != ident.VendorID || deviceInfo.ProductID != ident.ProductID {
		return false
	}

	if ident.Serial != "" && deviceInfo.Serial != ident.Serial {
		return false
	}

	if ident.Interface != nil && deviceInfo.Interface != *ident.Interface {
		return false
	}

	return true
}

// ProbeDevice checks whether the device is currently present and openable
// without keeping it open.
func ProbeDevice(ident config.ScannerIdentification) error {
	device, _, err := findAndOpenDevice(ident)
	if err != nil {
		return err
	}
	_ = device.Close()
	return nil
}

func (s *BarcodeScanner) Stop() error {
	s.cancel()

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

	s.logger.Debug("Barcode scanner stopped")
	return nil
}

func (s *BarcodeScanner) connectionManager() {
	for {
		select {
		case <-s.ctx.Done():
			return
		default:
			if s.tryConnect() {
				s.runReadLoop()
			}
			select {
			case <-s.ctx.Done():
				return
			case <-time.After(s.getReconnectDelay()):
			}
		}
	}
}

func (s *BarcodeScanner) tryConnect() bool {
	device, deviceInfo, err := findAndOpenDevice(s.ident)
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

	s.logger.Debugf("Connected to device %04x:%04x interface %d (%s)",
		s.ident.VendorID, s.ident.ProductID, deviceInfo.Interface, deviceInfo.Product)
	return true
}

func (s *BarcodeScanner) disconnect() {
	atomic.StoreInt32(&s.connected, 0)

	s.mutex.Lock()
	device := s.device
	s.device = nil
	s.deviceInfo = nil
	s.mutex.Unlock()

	if device != nil {
		if err := device.Close(); err != nil {
			s.logger.Warnf("Error closing device: %v", err)
		}
	}

	s.mutex.RLock()
	callback := s.onConnectionChange
	s.mutex.RUnlock()

	if callback != nil {
		callback(false)
	}
}

func (s *BarcodeScanner) runReadLoop() {
	const bufferSize = 64
	const tickerInterval = 10 * time.Millisecond

	// Discard any partial scan left over from a previous connection.
	s.hidProcessor.Reset()

	timeoutTicker := time.NewTicker(tickerInterval)
	defer timeoutTicker.Stop()

	dataChan := make(chan []byte, 10)
	errorChan := make(chan error, 1)

	go s.hidReadGoroutine(dataChan, errorChan, bufferSize)

	for {
		select {
		case <-s.ctx.Done():
			return

		case <-timeoutTicker.C:
			s.hidProcessor.CheckTimeout()

		case data := <-dataChan:
			// Key-release reports (all zeros) are processed too: they reset
			// the held-key tracking that deduplicates repeated reports.
			s.hidProcessor.ProcessData(data)

		case err := <-errorChan:
			s.logger.Warnf("HID read error: %v", err)
			s.disconnect()
			return
		}
	}
}

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
				if err.Error() == "hid: read timeout" || err.Error() == "hid: timeout" {
					continue
				}
				errorChan <- err
				return
			}

			if n > 0 {
				data := make([]byte, n)
				copy(data, buffer[:n])
				select {
				case dataChan <- data:
				case <-s.ctx.Done():
					return
				}
			}
		}
	}
}

func (s *BarcodeScanner) IsConnected() bool {
	return atomic.LoadInt32(&s.connected) == 1
}

func (s *BarcodeScanner) GetConnectedDeviceInfo() *hid.DeviceInfo {
	s.mutex.RLock()
	info := s.deviceInfo
	s.mutex.RUnlock()
	return info
}

func normalizeDeviceInfo(deviceInfo *hid.DeviceInfo) *hid.DeviceInfo {
	normalized := *deviceInfo // Copy the struct
	normalized.Manufacturer = strings.TrimSpace(normalized.Manufacturer)
	normalized.Product = strings.TrimSpace(normalized.Product)
	normalized.Serial = strings.TrimSpace(normalized.Serial)
	return &normalized
}

func (s *BarcodeScanner) SetReconnectDelay(delay time.Duration) {
	s.mutex.Lock()
	s.reconnectDelay = delay
	s.mutex.Unlock()
}

func (s *BarcodeScanner) getReconnectDelay() time.Duration {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.reconnectDelay
}

func ListAllDevices() []hid.DeviceInfo {
	return hid.Enumerate(0, 0)
}
