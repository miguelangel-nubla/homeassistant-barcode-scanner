package scanner

import (
	"testing"

	"github.com/sirupsen/logrus"
)

func TestNewHIDProcessor(t *testing.T) {
	logger := logrus.New()

	processor := NewHIDProcessor("enter", "us", logger)

	if processor == nil {
		t.Fatal("Expected processor to be created")
	}

	if processor.terminationChar != "enter" {
		t.Errorf("Expected termination char 'enter', got %s", processor.terminationChar)
	}

	if processor.keyboardLayout != "us" {
		t.Errorf("Expected keyboard layout 'us', got %s", processor.keyboardLayout)
	}

	if processor.logger != logger {
		t.Error("Expected logger to be stored")
	}
}

func TestHIDProcessor_EmptyKeyboardLayout(t *testing.T) {
	logger := logrus.New()

	// Test with empty keyboard layout - should store as empty (defaulting happens in config)
	processor := NewHIDProcessor("tab", "", logger)

	if processor.keyboardLayout != "" {
		t.Errorf("Expected empty keyboard layout to be stored as empty, got %s", processor.keyboardLayout)
	}
}

func TestHIDProcessor_TerminationCharacters(t *testing.T) {
	logger := logrus.New()

	tests := []struct {
		name     string
		termChar string
		expected string
	}{
		{"Enter key", "enter", "enter"},
		{"Tab key", "tab", "tab"},
		{"None", "none", "none"},
		{"Empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			processor := NewHIDProcessor(tt.termChar, "us", logger)

			if processor.terminationChar != tt.expected {
				t.Errorf("Expected termination char '%s', got '%s'", tt.expected, processor.terminationChar)
			}
		})
	}
}

func TestHIDProcessor_BufferInitialization(t *testing.T) {
	logger := logrus.New()
	processor := NewHIDProcessor("enter", "us", logger)

	if processor.buffer == nil {
		t.Error("Expected buffer to be initialized")
	}

	if len(processor.buffer) != 256 {
		t.Errorf("Expected buffer size 256, got %d", len(processor.buffer))
	}

	if processor.bufferLen != 0 {
		t.Errorf("Expected buffer length to be 0, got %d", processor.bufferLen)
	}
}

func TestHIDProcessor_KeyboardLayoutOptions(t *testing.T) {
	logger := logrus.New()

	layouts := []string{"us", "es", "fr", "de"}

	for _, layout := range layouts {
		processor := NewHIDProcessor("enter", layout, logger)

		if processor.keyboardLayout != layout {
			t.Errorf("Expected keyboard layout '%s', got '%s'", layout, processor.keyboardLayout)
		}
	}
}
