package scanner

import (
	"slices"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

const (
	hidKeyEnter      = 0x28
	hidKeyTab        = 0x2B
	hidModifierShift = 0x22
)

type KeyboardLayout struct {
	Letters map[byte][2]byte
	Numbers map[byte][2]byte
	Symbols map[byte][2]byte
}

type HIDProcessor struct {
	terminationChar string
	keyboardLayout  string
	buffer          []byte
	bufferLen       int
	onScan          func(string)
	logger          *logrus.Logger
	lastActivity    time.Time
}

func NewHIDProcessor(terminationChar, keyboardLayout string, logger *logrus.Logger) *HIDProcessor {
	return &HIDProcessor{
		terminationChar: terminationChar,
		keyboardLayout:  keyboardLayout,
		logger:          logger,
		buffer:          make([]byte, 256),
		lastActivity:    time.Now(),
	}
}

func (p *HIDProcessor) SetOnScanCallback(callback func(string)) {
	p.onScan = callback
}

func (p *HIDProcessor) ProcessData(data []byte) {
	if len(data) < 3 {
		return
	}

	modifier := data[0]

	for i := 2; i < min(len(data), 8); i++ {
		keyCode := data[i]
		if keyCode == 0 {
			continue
		}

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

func (p *HIDProcessor) CheckTimeout() {
	const timeout = 100 * time.Millisecond
	if p.bufferLen > 0 && time.Since(p.lastActivity) > timeout {
		p.finalizeInput()
	}
}

func (p *HIDProcessor) Reset() {
	p.bufferLen = 0
}

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

func (p *HIDProcessor) isTerminationKey(keyCode byte) bool {
	termChar := strings.ToLower(p.terminationChar)

	switch termChar {
	case "enter", "return":
		return keyCode == hidKeyEnter
	case "tab":
		return keyCode == hidKeyTab
	case "none", "":
		return false
	default:
		return keyCode == hidKeyEnter
	}
}

func (p *HIDProcessor) keyCodeToChar(keyCode, modifier byte) byte {
	layout, err := GetKeyboardLayout(p.keyboardLayout)
	if err != nil {
		p.logger.WithError(err).Warnf("Failed to load keyboard layout '%s', using US fallback", p.keyboardLayout)
		layout, _ = GetKeyboardLayout("us")
	}

	shifted := (modifier & hidModifierShift) != 0

	if slices.Contains(layout.Ignored, keyCode) {
		return 0
	}

	if chars, exists := layout.Letters[keyCode]; exists {
		if shifted {
			return chars[1]
		}
		return chars[0]
	}

	if chars, exists := layout.Numbers[keyCode]; exists {
		if shifted {
			return chars[1]
		}
		return chars[0]
	}

	if chars, exists := layout.Symbols[keyCode]; exists {
		if chars[0] == 0 {
			return 0
		}
		if shifted {
			return chars[1]
		}
		return chars[0]
	}

	return 0
}
