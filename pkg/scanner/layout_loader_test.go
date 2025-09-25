package scanner

import (
	"testing"
)

func TestLoadKeyboardLayouts(t *testing.T) {
	err := LoadKeyboardLayouts()
	if err != nil {
		t.Fatalf("Expected no error loading keyboard layouts, got: %v", err)
	}

	if loadedLayouts == nil {
		t.Fatal("Expected loaded layouts to be initialized")
	}

	if len(loadedLayouts) == 0 {
		t.Fatal("Expected at least one keyboard layout to be loaded")
	}

	if _, exists := loadedLayouts["us"]; !exists {
		t.Error("Expected US keyboard layout to exist")
	}
}

func TestGetKeyboardLayout_USLayout(t *testing.T) {
	layout, err := GetKeyboardLayout("us")
	if err != nil {
		t.Fatalf("Expected no error getting US layout, got: %v", err)
	}

	if layout.Name == "" {
		t.Error("Expected layout name to be set")
	}

	if len(layout.Letters) == 0 {
		t.Error("Expected letters mapping to be populated")
	}

	if len(layout.Numbers) == 0 {
		t.Error("Expected numbers mapping to be populated")
	}
}

func TestGetKeyboardLayout_SpanishLayout(t *testing.T) {
	layout, err := GetKeyboardLayout("es")
	if err != nil {
		t.Fatalf("Expected no error getting Spanish layout, got: %v", err)
	}

	if layout.Name == "" {
		t.Error("Expected layout name to be set")
	}

	if len(layout.Letters) == 0 {
		t.Error("Expected letters mapping to be populated")
	}
}

func TestGetKeyboardLayout_NonexistentLayout(t *testing.T) {
	layout, err := GetKeyboardLayout("nonexistent")
	if err != nil {
		layout, err = GetKeyboardLayout("us")
		if err != nil {
			t.Fatalf("Expected fallback to US layout to work, got: %v", err)
		}
	}

	if layout.Name == "" {
		t.Error("Expected layout to have a name")
	}
}

func TestGetKeyboardLayout_FallbackBehavior(t *testing.T) {
	layout, err := GetKeyboardLayout("invalid_layout")
	if err != nil {
		t.Logf("Invalid layout correctly returned error: %v", err)
		layout, err = GetKeyboardLayout("us")
		if err != nil {
			t.Fatalf("Expected US layout to be available as fallback, got: %v", err)
		}
	}

	if layout.Name == "" {
		t.Error("Expected layout to have a name")
	}
}

func TestConvertStringMappings(t *testing.T) {
	source := map[byte][2]string{
		0x04: {"a", "A"},
		0x05: {"b", "B"},
		0x06: {"", "C"},
		0x07: {"d", ""},
	}

	target := make(map[byte][2]byte)

	convertStringMappings(source, target)

	expected := map[byte][2]byte{
		0x04: {'a', 'A'},
		0x05: {'b', 'B'},
	}

	if len(target) != len(expected) {
		t.Errorf("Expected %d mappings, got %d", len(expected), len(target))
	}

	for key, expectedValue := range expected {
		if actualValue, exists := target[key]; !exists {
			t.Errorf("Expected key %02x to exist", key)
		} else if actualValue != expectedValue {
			t.Errorf("Expected key %02x to have value %v, got %v", key, expectedValue, actualValue)
		}
	}

	if _, exists := target[0x06]; exists {
		t.Error("Expected key 0x06 with empty first string to be skipped")
	}

	if _, exists := target[0x07]; exists {
		t.Error("Expected key 0x07 with empty second string to be skipped")
	}
}

func TestKeyboardLayoutStructure(t *testing.T) {
	layout, err := GetKeyboardLayout("us")
	if err != nil {
		t.Fatalf("Expected no error getting US layout, got: %v", err)
	}

	if layout.Letters == nil {
		t.Error("Expected Letters map to be initialized")
	}

	if layout.Numbers == nil {
		t.Error("Expected Numbers map to be initialized")
	}

	if layout.Symbols == nil {
		t.Error("Expected Symbols map to be initialized")
	}

	if layout.Ignored == nil {
		t.Error("Expected Ignored slice to be initialized")
	}
}

func TestKeyboardLayoutValidMappings(t *testing.T) {
	layout, err := GetKeyboardLayout("us")
	if err != nil {
		t.Fatalf("Expected no error getting US layout, got: %v", err)
	}

	for keyCode, mapping := range layout.Letters {
		if mapping[0] == 0 && mapping[1] == 0 {
			t.Errorf("Key code %02x has empty mapping", keyCode)
		}
	}

	for keyCode, mapping := range layout.Numbers {
		if mapping[0] == 0 && mapping[1] == 0 {
			t.Errorf("Number key code %02x has empty mapping", keyCode)
		}
	}
}
