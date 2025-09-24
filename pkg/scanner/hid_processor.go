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

	// Comprehensive symbol and special key mappings
	symbols := map[byte][2]byte{
		// Special keys
		0x28: {'\n', '\n'}, // Enter key
		0x29: {0x1B, 0x1B}, // Escape key (ESC character)
		0x2A: {0x08, 0x08}, // Backspace
		0x2B: {'\t', '\t'}, // Tab key
		0x2C: {' ', ' '},   // Space key

		// Symbol keys
		0x2D: {'-', '_'},  // Minus/Underscore
		0x2E: {'=', '+'},  // Equal/Plus
		0x2F: {'[', '{'},  // Left bracket
		0x30: {']', '}'},  // Right bracket
		0x31: {'\\', '|'}, // Backslash/Pipe
		0x32: {'#', '~'},  // Non-US Hash/Tilde (varies by layout)
		0x33: {';', ':'},  // Semicolon/Colon
		0x34: {'\'', '"'}, // Apostrophe/Quote
		0x35: {'`', '~'},  // Grave accent/Tilde
		0x36: {',', '<'},  // Comma/Less than
		0x37: {'.', '>'},  // Period/Greater than
		0x38: {'/', '?'},  // Slash/Question mark

		// Function keys (as control characters or ignore)
		0x3A: {0, 0}, // F1
		0x3B: {0, 0}, // F2
		0x3C: {0, 0}, // F3
		0x3D: {0, 0}, // F4
		0x3E: {0, 0}, // F5
		0x3F: {0, 0}, // F6
		0x40: {0, 0}, // F7
		0x41: {0, 0}, // F8
		0x42: {0, 0}, // F9
		0x43: {0, 0}, // F10
		0x44: {0, 0}, // F11
		0x45: {0, 0}, // F12

		// Arrow keys and other navigation (typically not in barcodes but should be handled)
		0x4F: {0, 0}, // Right arrow
		0x50: {0, 0}, // Left arrow
		0x51: {0, 0}, // Down arrow
		0x52: {0, 0}, // Up arrow

		// Keypad numbers (when NumLock is on)
		0x53: {0, 0},       // Num Lock
		0x54: {'/', '/'},   // Keypad /
		0x55: {'*', '*'},   // Keypad *
		0x56: {'-', '-'},   // Keypad -
		0x57: {'+', '+'},   // Keypad +
		0x58: {'\n', '\n'}, // Keypad Enter
		0x59: {'1', '1'},   // Keypad 1
		0x5A: {'2', '2'},   // Keypad 2
		0x5B: {'3', '3'},   // Keypad 3
		0x5C: {'4', '4'},   // Keypad 4
		0x5D: {'5', '5'},   // Keypad 5
		0x5E: {'6', '6'},   // Keypad 6
		0x5F: {'7', '7'},   // Keypad 7
		0x60: {'8', '8'},   // Keypad 8
		0x61: {'9', '9'},   // Keypad 9
		0x62: {'0', '0'},   // Keypad 0
		0x63: {'.', '.'},   // Keypad .
	}

	if chars, exists := symbols[keyCode]; exists {
		if chars[0] == 0 {
			return 0 // Ignore function keys and navigation keys
		}
		if shifted {
			return chars[1]
		}
		return chars[0]
	}

	return 0
}
