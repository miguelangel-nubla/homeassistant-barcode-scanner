package scanner

import (
	"strings"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/miguelangel-nubla/homeassistant-barcode-scanner/pkg/layouts"
)

const (
	hidKeyEnter = 0x28
	hidKeyTab   = 0x2B

	// Left shift (bit 1) and right shift (bit 5) of the HID modifier byte.
	hidShiftMask = 0x02 | 0x20

	// HID boot-protocol keyboard reports carry up to 6 key codes in bytes 2-7.
	hidMaxKeys = 6

	// maxBarcodeLength caps the scan buffer; longer input is truncated.
	maxBarcodeLength = 256

	// scanTimeout finalizes a scan when no termination key arrives.
	scanTimeout = 100 * time.Millisecond
)

type HIDProcessor struct {
	terminationChar string
	layoutName      string
	layout          layouts.Layout
	buffer          []rune
	onScan          func(string)
	logger          *logrus.Logger
	lastActivity    time.Time

	// prevKeys holds the key codes of the previous report so that keys held
	// across consecutive reports are not emitted twice.
	prevKeys  [hidMaxKeys]byte
	truncated bool
}

func NewHIDProcessor(terminationChar, keyboardLayout string, logger *logrus.Logger) *HIDProcessor {
	layoutName := strings.ToLower(keyboardLayout)
	if layoutName == "" {
		layoutName = layouts.Fallback
	}

	layout, err := layouts.Get(layoutName)
	if err != nil {
		logger.WithError(err).Warnf("Failed to load keyboard layout %q, using %q fallback", keyboardLayout, layouts.Fallback)
		layout, err = layouts.Get(layouts.Fallback)
		if err != nil {
			// The fallback layout is embedded in the binary; failing to load
			// it means the build itself is broken.
			logger.WithError(err).Panic("Embedded fallback keyboard layout unavailable")
		}
		layoutName = layouts.Fallback
	}

	return &HIDProcessor{
		terminationChar: strings.ToLower(terminationChar),
		layoutName:      layoutName,
		layout:          layout,
		logger:          logger,
		buffer:          make([]rune, 0, maxBarcodeLength),
		lastActivity:    time.Now(),
	}
}

func (p *HIDProcessor) SetOnScanCallback(callback func(string)) {
	p.onScan = callback
}

// ProcessData handles a single HID boot-protocol keyboard report. Reports
// with no pressed keys (key release) reset the held-key tracking and must be
// passed in as well.
func (p *HIDProcessor) ProcessData(data []byte) {
	if len(data) < 3 {
		return
	}

	modifier := data[0]
	shifted := (modifier & hidShiftMask) != 0

	var currentKeys [hidMaxKeys]byte
	copy(currentKeys[:], data[2:min(len(data), 2+hidMaxKeys)])

	for _, keyCode := range currentKeys {
		if keyCode == 0 {
			continue
		}
		if p.wasKeyHeld(keyCode) {
			continue
		}

		if p.isTerminationKey(keyCode) {
			p.prevKeys = currentKeys
			p.finalizeInput()
			return
		}

		p.appendKey(keyCode, shifted)
	}

	p.prevKeys = currentKeys
}

func (p *HIDProcessor) wasKeyHeld(keyCode byte) bool {
	for _, prev := range p.prevKeys {
		if prev == keyCode {
			return true
		}
	}
	return false
}

func (p *HIDProcessor) appendKey(keyCode byte, shifted bool) {
	char, ok := p.layout.Rune(keyCode, shifted)
	if !ok {
		return
	}

	p.lastActivity = time.Now()
	if len(p.buffer) >= maxBarcodeLength {
		if !p.truncated {
			p.truncated = true
			p.logger.Warnf("Barcode exceeds %d characters, truncating", maxBarcodeLength)
		}
		return
	}
	p.buffer = append(p.buffer, char)
}

// CheckTimeout finalizes a pending scan when input has been idle, which
// covers scanners configured without a termination character.
func (p *HIDProcessor) CheckTimeout() {
	if len(p.buffer) > 0 && time.Since(p.lastActivity) > scanTimeout {
		p.finalizeInput()
	}
}

// Reset discards any partial scan and held-key state, e.g. after the device
// reconnects.
func (p *HIDProcessor) Reset() {
	p.buffer = p.buffer[:0]
	p.truncated = false
	p.prevKeys = [hidMaxKeys]byte{}
}

func (p *HIDProcessor) finalizeInput() {
	if len(p.buffer) == 0 {
		return
	}

	barcode := strings.TrimSpace(string(p.buffer))
	p.buffer = p.buffer[:0]
	p.truncated = false

	if barcode != "" && p.onScan != nil {
		p.onScan(barcode)
	}
}

func (p *HIDProcessor) isTerminationKey(keyCode byte) bool {
	switch p.terminationChar {
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
