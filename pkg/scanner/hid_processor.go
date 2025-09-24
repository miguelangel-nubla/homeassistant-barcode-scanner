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
	buffer          strings.Builder
	onScan          func(string)
	logger          *logrus.Logger
	lastActivity    time.Time
}

// NewHIDProcessor creates a new HID data processor
func NewHIDProcessor(terminationChar string, logger *logrus.Logger) *HIDProcessor {
	return &HIDProcessor{
		terminationChar: terminationChar,
		logger:          logger,
		lastActivity:    time.Now(),
	}
}

// SetOnScanCallback sets the callback for completed scans
func (p *HIDProcessor) SetOnScanCallback(callback func(string)) {
	p.onScan = callback
}

// ProcessData processes raw HID data and extracts characters
func (p *HIDProcessor) ProcessData(data []byte) {
	p.logger.Debugf("Processing HID data: %x (length: %d)", data, len(data))
	if len(data) < 3 {
		p.logger.Debug("HID data too short, ignoring")
		return
	}

	// Process HID keyboard report: [modifier, reserved, key1-key6]
	modifier := data[0]
	p.logger.Debugf("HID modifier: 0x%02x", modifier)

	for i := 2; i < min(len(data), 8); i++ {
		keyCode := data[i]
		if keyCode == 0 {
			continue
		}

		p.logger.Debugf("Processing keyCode: 0x%02x (%d)", keyCode, keyCode)

		// Check for termination key
		if p.isTerminationKey(keyCode) {
			p.logger.Debugf("Termination key detected (0x%02x), finalizing barcode", keyCode)
			p.finalizeInput()
			return
		}

		if char := p.keyCodeToChar(keyCode, modifier); char != 0 {
			p.logger.Debugf("KeyCode 0x%02x -> character '%c' (0x%02x)", keyCode, char, char)
			p.buffer.WriteByte(char)
			p.lastActivity = time.Now()
		} else {
			p.logger.Debugf("KeyCode 0x%02x -> no character mapping", keyCode)
		}
	}
	p.logger.Debugf("Current buffer after processing: '%s'", p.buffer.String())
}

// CheckTimeout checks if input should be finalized due to timeout
func (p *HIDProcessor) CheckTimeout() {
	const timeout = 100 * time.Millisecond
	if p.buffer.Len() > 0 && time.Since(p.lastActivity) > timeout {
		p.logger.Debugf("Timeout: finalizing barcode after %v idle, buffer: '%s'",
			time.Since(p.lastActivity), p.buffer.String())
		p.finalizeInput()
	}
}

// Reset clears the current buffer
func (p *HIDProcessor) Reset() {
	p.buffer.Reset()
}

// finalizeInput processes completed barcode input
func (p *HIDProcessor) finalizeInput() {
	rawBuffer := p.buffer.String()
	barcode := strings.TrimSpace(rawBuffer)
	p.logger.Debugf("Finalizing barcode input: raw='%s', trimmed='%s', length=%d",
		rawBuffer, barcode, len(barcode))
	p.buffer.Reset()

	if barcode != "" && p.onScan != nil {
		p.logger.Infof("Barcode scanned: %s", barcode)
		p.onScan(barcode)
	} else if barcode == "" {
		p.logger.Debug("Empty barcode after trimming, ignoring")
	} else {
		p.logger.Warn("Barcode scanned but no callback set")
	}
}

// isTerminationKey checks if the key code matches the configured termination character
func (p *HIDProcessor) isTerminationKey(keyCode byte) bool {
	termChar := strings.ToLower(p.terminationChar)
	p.logger.Debugf("Checking termination key: keyCode=0x%02x, configured=%s", keyCode, termChar)

	switch termChar {
	case "enter", "return":
		return keyCode == hidKeyEnter
	case "tab":
		return keyCode == hidKeyTab
	case "none", "":
		return false // Rely on timeout
	default:
		// Default to Enter key if unknown termination char specified
		p.logger.Debugf("Unknown termination character '%s', defaulting to Enter", p.terminationChar)
		return keyCode == hidKeyEnter
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
