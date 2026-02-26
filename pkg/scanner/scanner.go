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

type BarcodeScanner struct {
	vendorID          uint16
	productID         uint16
	requiredSerial    string
	requiredInterface *int

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

func NewBarcodeScanner(vendorID, productID uint16, terminationChar, keyboardLayout string, logger *logrus.Logger) *BarcodeScanner {
	return NewBarcodeScannerWithSerial(vendorID, productID, "", terminationChar, keyboardLayout, logger)
}

func NewBarcodeScannerWithSerial(
	vendorID, productID uint16, requiredSerial, terminationChar, keyboardLayout string, logger *logrus.Logger,
) *BarcodeScanner {
	return NewBarcodeScannerWithInterface(vendorID, productID, requiredSerial, nil, terminationChar, keyboardLayout, logger)
}

func NewBarcodeScannerWithInterface(
	vendorID, productID uint16, requiredSerial string, requiredInterface *int, terminationChar, keyboardLayout string, logger *logrus.Logger,
) *BarcodeScanner {
	ctx, cancel := context.WithCancel(context.Background())

	s := &BarcodeScanner{
		vendorID:          vendorID,
		productID:         productID,
		requiredSerial:    requiredSerial,
		requiredInterface: requiredInterface,
		logger:            logger,
		reconnectDelay:    time.Second,
		ctx:               ctx,
		cancel:            cancel,
	}

	s.hidProcessor = NewHIDProcessor(terminationChar, keyboardLayout, logger)
	s.hidProcessor.SetOnScanCallback(func(barcode string) {
		if s.onScan != nil {
			s.onScan(barcode)
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

func (s *BarcodeScanner) TryInitialConnect() error {
	device, _, err := s.findAndOpenDevice()
	if err != nil {
		return err
	}

	if device != nil {
		_ = device.Close()
	}

	return nil
}

func (s *BarcodeScanner) findAndOpenDevice() (*hid.Device, *hid.DeviceInfo, error) {
	devices := hid.Enumerate(s.vendorID, s.productID)

	for _, deviceInfo := range devices {
		if s.isTargetDevice(&deviceInfo) {
			device, err := deviceInfo.Open()
			if err != nil {
				continue // Try next device
			}

			normalizedInfo := s.normalizeDeviceInfo(&deviceInfo)
			return device, normalizedInfo, nil
		}
	}

	errorMsg := fmt.Sprintf("device %04x:%04x", s.vendorID, s.productID)
	if s.requiredSerial != "" {
		errorMsg += fmt.Sprintf(" serial '%s'", s.requiredSerial)
	}
	if s.requiredInterface != nil {
		errorMsg += fmt.Sprintf(" interface %d", *s.requiredInterface)
	}
	return nil, nil, fmt.Errorf("%s not found", errorMsg)
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
			case <-time.After(s.reconnectDelay):
			}
		}
	}
}

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

	interfaceInfo := fmt.Sprintf(" interface %d", deviceInfo.Interface)
	s.logger.Debugf("Connected to device %04x:%04x%s (%s)", s.vendorID, s.productID, interfaceInfo, deviceInfo.Product)
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

func (s *BarcodeScanner) isTargetDevice(deviceInfo *hid.DeviceInfo) bool {
	if deviceInfo.VendorID != s.vendorID || deviceInfo.ProductID != s.productID {
		return false
	}

	if s.requiredSerial != "" && deviceInfo.Serial != s.requiredSerial {
		return false
	}

	if s.requiredInterface != nil && deviceInfo.Interface != *s.requiredInterface {
		return false
	}

	return true
}

func (s *BarcodeScanner) runReadLoop() {
	const bufferSize = 64
	const tickerInterval = 10 * time.Millisecond

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
			if len(data) > 0 && !s.isAllZeros(data) {
				s.hidProcessor.ProcessData(data)
			}

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
				dataChan <- data
			}
		}
	}
}

func (s *BarcodeScanner) isAllZeros(data []byte) bool {
	for _, b := range data {
		if b != 0 {
			return false
		}
	}
	return true
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

func (s *BarcodeScanner) GetRequiredInterface() *int {
	return s.requiredInterface
}

func (s *BarcodeScanner) GetRequiredSerial() string {
	return s.requiredSerial
}

func (s *BarcodeScanner) normalizeDeviceInfo(deviceInfo *hid.DeviceInfo) *hid.DeviceInfo {
	normalized := *deviceInfo // Copy the struct
	normalized.Manufacturer = strings.TrimSpace(normalized.Manufacturer)
	normalized.Product = strings.TrimSpace(normalized.Product)
	normalized.Serial = strings.TrimSpace(normalized.Serial)
	return &normalized
}

func (s *BarcodeScanner) SetReconnectDelay(delay time.Duration) {
	s.reconnectDelay = delay
}

func ListAllDevices() []hid.DeviceInfo {
	return hid.Enumerate(0, 0)
}
