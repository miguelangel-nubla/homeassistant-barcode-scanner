package scanner

import (
	"fmt"
	"strings"
	"time"

	"github.com/karalabe/hid"
	"github.com/sirupsen/logrus"
)

// BarcodeScanner represents a USB HID barcode scanner
type BarcodeScanner struct {
	device          *hid.Device
	logger          *logrus.Logger
	vendorID        uint16
	productID       uint16
	devicePath      string
	terminationChar string
	onScan          func(string)
	stopCh          chan struct{}
	buffer          strings.Builder
}

// NewBarcodeScanner creates a new barcode scanner instance
func NewBarcodeScanner(vendorID, productID uint16, devicePath, terminationChar string, logger *logrus.Logger) *BarcodeScanner {
	return &BarcodeScanner{
		vendorID:        vendorID,
		productID:       productID,
		devicePath:      devicePath,
		terminationChar: terminationChar,
		logger:          logger,
		stopCh:          make(chan struct{}),
	}
}

// SetOnScanCallback sets the callback function to be called when a barcode is scanned
func (s *BarcodeScanner) SetOnScanCallback(callback func(string)) {
	s.onScan = callback
}

// Start begins listening for barcode scans
func (s *BarcodeScanner) Start() error {
	device, err := s.openDevice()
	if err != nil {
		return err
	}
	s.device = device

	go s.readLoop()
	s.logger.Info("Barcode scanner started successfully")
	return nil
}

// openDevice finds and opens a suitable HID device
func (s *BarcodeScanner) openDevice() (*hid.Device, error) {
	devices := hid.Enumerate(0, 0)
	if len(devices) == 0 {
		return nil, fmt.Errorf("no HID devices found")
	}

	// Try specific device path first
	if s.devicePath != "" {
		for _, deviceInfo := range devices {
			if deviceInfo.Path == s.devicePath {
				device, err := deviceInfo.Open()
				if err != nil {
					return nil, fmt.Errorf("failed to open device at path %s: %w", s.devicePath, err)
				}
				s.logger.Infof("Opened device at path: %s", s.devicePath)
				return device, nil
			}
		}
		return nil, fmt.Errorf("device not found at path: %s", s.devicePath)
	}

	// Try specific vendor/product ID
	if s.vendorID != 0 && s.productID != 0 {
		for _, deviceInfo := range devices {
			if deviceInfo.VendorID == s.vendorID && deviceInfo.ProductID == s.productID {
				device, err := deviceInfo.Open()
				if err != nil {
					return nil, fmt.Errorf("failed to open device %04x:%04x: %w", s.vendorID, s.productID, err)
				}
				s.logger.Infof("Opened device %04x:%04x (%s)", s.vendorID, s.productID, deviceInfo.Product)
				return device, nil
			}
		}
		return nil, fmt.Errorf("device %04x:%04x not found", s.vendorID, s.productID)
	}

	// Auto-detect barcode scanner
	for _, deviceInfo := range devices {
		if s.isLikelyBarcodeScanner(deviceInfo) {
			device, err := deviceInfo.Open()
			if err != nil {
				s.logger.Warnf("Failed to open potential scanner %s: %v", deviceInfo.Product, err)
				continue
			}
			s.logger.Infof("Auto-detected and opened scanner: %s", deviceInfo.Product)
			return device, nil
		}
	}

	return nil, fmt.Errorf("no suitable barcode scanner devices found")
}

// Stop stops the barcode scanner
func (s *BarcodeScanner) Stop() error {
	close(s.stopCh)

	if s.device != nil {
		err := s.device.Close()
		s.device = nil
		if err != nil {
			return fmt.Errorf("failed to close HID device: %w", err)
		}
	}

	s.logger.Info("Barcode scanner stopped")
	return nil
}

// isLikelyBarcodeScanner determines if a device is likely a barcode scanner
func (s *BarcodeScanner) isLikelyBarcodeScanner(deviceInfo hid.DeviceInfo) bool {
	// Check usage page and usage for HID keyboard (most barcode scanners emulate keyboards)
	if deviceInfo.UsagePage == 1 && deviceInfo.Usage == 6 {
		return true
	}

	// Check for known barcode scanner manufacturers
	manufacturerLower := strings.ToLower(deviceInfo.Manufacturer)
	manufacturerKeywords := []string{"newtologic", "honeywell", "symbol", "datalogic", "zebra", "unitech", "cipherlab", "opticon"}

	for _, keyword := range manufacturerKeywords {
		if strings.Contains(manufacturerLower, keyword) {
			return true
		}
	}

	// Check product name for scanner keywords
	productLower := strings.ToLower(deviceInfo.Product)
	productKeywords := []string{"scanner", "barcode", "code", "reader", "nt1640", "nt1600", "nt16"}

	for _, keyword := range productKeywords {
		if strings.Contains(productLower, keyword) {
			return true
		}
	}

	// Check for specific known barcode scanner vendor/product combinations
	knownScanners := [][2]uint16{
		{0x060e, 0x16c7}, // Newtologic NT1640S
		{0x05e0, 0x1200}, // Common Symbol/Motorola scanner
		{0x0536, 0x02bc}, // Unitech barcode scanner
	}

	for _, scanner := range knownScanners {
		if deviceInfo.VendorID == scanner[0] && deviceInfo.ProductID == scanner[1] {
			return true
		}
	}

	return false
}

// readLoop continuously reads from the HID device
func (s *BarcodeScanner) readLoop() {
	defer s.logger.Info("Scanner read loop stopped")

	buffer := make([]byte, 64)
	lastActivity := time.Now()

	for {
		select {
		case <-s.stopCh:
			return
		default:
			n, err := s.device.Read(buffer)
			if err != nil {
				s.logger.Errorf("Error reading from HID device: %v", err)
				time.Sleep(100 * time.Millisecond)
				continue
			}

			if n > 0 {
				lastActivity = time.Now()
				s.processHIDData(buffer[:n])
			} else {
				// Check for completed barcode after idle period
				if time.Since(lastActivity) > 100*time.Millisecond && s.buffer.Len() > 0 {
					s.finalizeBarcodeInput()
				}
				time.Sleep(10 * time.Millisecond)
			}
		}
	}
}

// finalizeBarcodeInput processes completed barcode input
func (s *BarcodeScanner) finalizeBarcodeInput() {
	barcode := strings.TrimSpace(s.buffer.String())
	s.buffer.Reset()

	if barcode != "" && s.onScan != nil {
		s.logger.Infof("Barcode scanned: %s", barcode)
		s.onScan(barcode)
	}
}

// processHIDData processes raw HID data and extracts characters
func (s *BarcodeScanner) processHIDData(data []byte) {
	if len(data) < 3 {
		return
	}

	// Process HID keyboard report: [modifier, reserved, key1-key6]
	modifier := data[0]
	for i := 2; i < len(data) && i < 8; i++ {
		keyCode := data[i]
		if keyCode == 0 {
			continue
		}

		// Check for configured termination character
		if s.isTerminationKey(keyCode) {
			s.finalizeBarcodeInput()
			return
		}

		if char := s.hidKeyCodeToChar(keyCode, modifier); char != 0 {
			s.buffer.WriteByte(char)
		}
	}
}

// isTerminationKey checks if the key code matches the configured termination character
func (s *BarcodeScanner) isTerminationKey(keyCode byte) bool {
	switch strings.ToLower(s.terminationChar) {
	case "enter", "return":
		return keyCode == 0x28 // Enter key
	case "tab":
		return keyCode == 0x2B // Tab key
	case "none", "":
		return false // No termination character, rely on timeout
	default:
		// Default to Enter key if unknown termination char specified
		return keyCode == 0x28
	}
}

// hidKeyCodeToChar converts USB HID key codes to ASCII characters
// Based on Linux input-event-codes.h: https://github.com/torvalds/linux/blob/master/include/uapi/linux/input-event-codes.h
func (s *BarcodeScanner) hidKeyCodeToChar(keyCode, modifier byte) byte {
	shifted := (modifier & 0x22) != 0 // Left or right shift pressed

	// Letters (a-z) - KEY_A to KEY_Z (0x04-0x1D)
	if keyCode >= 0x04 && keyCode <= 0x1D {
		char := 'a' + keyCode - 0x04
		if shifted {
			char = 'A' + keyCode - 0x04
		}
		return byte(char)
	}

	// Numbers (1-9, 0) - KEY_1 to KEY_0 (0x1E-0x27)
	if keyCode >= 0x1E && keyCode <= 0x27 {
		if shifted {
			return "!@#$%^&*()"[keyCode-0x1E]
		}
		if keyCode == 0x27 { // KEY_0
			return '0'
		}
		return byte('1' + keyCode - 0x1E)
	}

	// Common symbols - based on USB HID usage table
	symbolMap := map[byte][2]byte{
		0x2C: {' ', ' '},   // KEY_SPACE
		0x2D: {'-', '_'},   // KEY_MINUS
		0x2E: {'=', '+'},   // KEY_EQUAL
		0x2F: {'[', '{'},   // KEY_LEFTBRACE
		0x30: {']', '}'},   // KEY_RIGHTBRACE
		0x31: {'\\', '|'},  // KEY_BACKSLASH
		0x33: {';', ':'},   // KEY_SEMICOLON
		0x34: {'\'', '"'},  // KEY_APOSTROPHE
		0x35: {'`', '~'},   // KEY_GRAVE
		0x36: {',', '<'},   // KEY_COMMA
		0x37: {'.', '>'},   // KEY_DOT
		0x38: {'/', '?'},   // KEY_SLASH
	}

	if chars, exists := symbolMap[keyCode]; exists {
		if shifted {
			return chars[1]
		}
		return chars[0]
	}

	return 0 // Unknown key code
}

// ListAllDevices returns a list of all available HID devices
func ListAllDevices() []hid.DeviceInfo {
	return hid.Enumerate(0, 0)
}

// ListDevices returns a list of available HID devices that might be barcode scanners
func ListDevices() []hid.DeviceInfo {
	devices := hid.Enumerate(0, 0)
	var scanners []hid.DeviceInfo

	scanner := &BarcodeScanner{}
	for _, device := range devices {
		if scanner.isLikelyBarcodeScanner(device) {
			scanners = append(scanners, device)
		}
	}

	return scanners
}