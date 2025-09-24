package scanner

import (
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

const (
	// HID keyboard key codes
	hidKeyEnter = 0x28
	hidKeyTab   = 0x2B

	// HID data parsing constants
	hidModifierShift = 0x22 // Left or right shift
	hidKeyMin        = 0x04 // 'a' key
	hidKeyMax        = 0x1D // 'z' key
	hidNumMin        = 0x1E // '1' key
	hidNumMax        = 0x27 // '0' key
)

// HIDProcessor handles HID keyboard data processing
type HIDProcessor struct {
	terminationChar string
	buffer          []byte
	bufferLen       int
	onScan          func(string)
	logger          *logrus.Logger
	lastActivity    time.Time
}

// NewHIDProcessor creates a new HID data processor
func NewHIDProcessor(terminationChar string, logger *logrus.Logger) *HIDProcessor {
	return &HIDProcessor{
		terminationChar: terminationChar,
		logger:          logger,
		buffer:          make([]byte, 256), // Pre-allocate buffer
		lastActivity:    time.Now(),
	}
}

// SetOnScanCallback sets the callback for completed scans
func (p *HIDProcessor) SetOnScanCallback(callback func(string)) {
	p.onScan = callback
}

// ProcessData processes raw HID data and extracts characters
func (p *HIDProcessor) ProcessData(data []byte) {
	if len(data) < 3 {
		return
	}

	// Process HID keyboard report: [modifier, reserved, key1-key6]
	modifier := data[0]

	for i := 2; i < min(len(data), 8); i++ {
		keyCode := data[i]
		if keyCode == 0 {
			continue
		}

		// Check for termination key
		if p.isTerminationKey(keyCode) {
			p.finalizeInput()
			return
		}

		if char := p.keyCodeToChar(keyCode, modifier); char != 0 && p.bufferLen < len(p.buffer)-1 {
			p.buffer[p.bufferLen] = char
			p.bufferLen++
			p.lastActivity = time.Now()
		}
	}
}

// CheckTimeout checks if input should be finalized due to timeout
func (p *HIDProcessor) CheckTimeout() {
	const timeout = 100 * time.Millisecond
	if p.bufferLen > 0 && time.Since(p.lastActivity) > timeout {
		p.finalizeInput()
	}
}

// Reset clears the current buffer
func (p *HIDProcessor) Reset() {
	p.bufferLen = 0
}

// finalizeInput processes completed barcode input
func (p *HIDProcessor) finalizeInput() {
	if p.bufferLen == 0 {
		return
	}

	barcode := strings.TrimSpace(string(p.buffer[:p.bufferLen]))
	p.bufferLen = 0

	if barcode != "" && p.onScan != nil {
		p.logger.Infof("Barcode scanned: %s", barcode)
		p.onScan(barcode)
	}
}

// isTerminationKey checks if the key code matches the configured termination character
func (p *HIDProcessor) isTerminationKey(keyCode byte) bool {
	termChar := strings.ToLower(p.terminationChar)

	switch termChar {
	case "enter", "return":
		return keyCode == hidKeyEnter
	case "tab":
		return keyCode == hidKeyTab
	case "none", "":
		return false // Rely on timeout
	default:
		return keyCode == hidKeyEnter // Default to Enter
	}
}

// keyCodeToChar converts USB HID key codes to ASCII characters
func (p *HIDProcessor) keyCodeToChar(keyCode, modifier byte) byte {
	shifted := (modifier & hidModifierShift) != 0

	// Letters (a-z)
	if keyCode >= hidKeyMin && keyCode <= hidKeyMax {
		if shifted {
			return 'A' + keyCode - hidKeyMin
		}
		return 'a' + keyCode - hidKeyMin
	}

	// Numbers with symbols when shifted
	if keyCode >= hidNumMin && keyCode <= hidNumMax {
		if shifted {
			return "!@#$%^&*()"[keyCode-hidNumMin]
		}
		if keyCode == hidNumMax { // '0' key
			return '0'
		}
		return '1' + keyCode - hidNumMin
	}

	// Symbol mappings
	symbols := map[byte][2]byte{
		0x2C: {' ', ' '}, 0x2D: {'-', '_'}, 0x2E: {'=', '+'}, 0x2F: {'[', '{'},
		0x30: {']', '}'}, 0x31: {'\\', '|'}, 0x33: {';', ':'}, 0x34: {'\'', '"'},
		0x35: {'`', '~'}, 0x36: {',', '<'}, 0x37: {'.', '>'}, 0x38: {'/', '?'},
	}

	if chars, exists := symbols[keyCode]; exists {
		if shifted {
			return chars[1]
		}
		return chars[0]
	}

	return 0
}
