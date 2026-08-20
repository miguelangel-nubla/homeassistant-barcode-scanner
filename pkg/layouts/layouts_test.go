package layouts

import (
	"slices"
	"sync"
	"testing"
)

func TestGetAvailableLayouts(t *testing.T) {
	names, err := GetAvailableLayouts()
	if err != nil {
		t.Fatalf("GetAvailableLayouts() error: %v", err)
	}

	if !slices.Contains(names, "us") {
		t.Errorf("expected 'us' in available layouts, got %v", names)
	}
	if !slices.Contains(names, "es") {
		t.Errorf("expected 'es' in available layouts, got %v", names)
	}
	if !slices.IsSorted(names) {
		t.Errorf("expected layouts to be sorted, got %v", names)
	}
}

func TestGet_USLayout(t *testing.T) {
	layout, err := Get("us")
	if err != nil {
		t.Fatalf("Get(us) error: %v", err)
	}

	if layout.Name == "" {
		t.Error("expected layout name to be set")
	}

	tests := []struct {
		keyCode byte
		shifted bool
		want    rune
	}{
		{0x04, false, 'a'},
		{0x04, true, 'A'},
		{0x1e, false, '1'},
		{0x1e, true, '!'},
		{0x2C, false, ' '},
		{0x2D, true, '_'},
		{0x38, true, '?'},
	}

	for _, tt := range tests {
		got, ok := layout.Rune(tt.keyCode, tt.shifted)
		if !ok {
			t.Errorf("Rune(0x%02x, shifted=%v): expected mapping to exist", tt.keyCode, tt.shifted)
			continue
		}
		if got != tt.want {
			t.Errorf("Rune(0x%02x, shifted=%v) = %q, want %q", tt.keyCode, tt.shifted, got, tt.want)
		}
	}
}

func TestGet_SpanishLayoutMultiByteRunes(t *testing.T) {
	layout, err := Get("es")
	if err != nil {
		t.Fatalf("Get(es) error: %v", err)
	}

	tests := []struct {
		keyCode byte
		shifted bool
		want    rune
	}{
		{0x33, false, 'ñ'},
		{0x33, true, 'Ñ'},
		{0x31, false, 'ç'},
		{0x31, true, 'Ç'},
		{0x2E, false, '¡'},
		{0x2E, true, '¿'},
		{0x20, true, '·'},
		{0x32, false, 'º'},
	}

	for _, tt := range tests {
		got, ok := layout.Rune(tt.keyCode, tt.shifted)
		if !ok {
			t.Errorf("Rune(0x%02x, shifted=%v): expected mapping to exist", tt.keyCode, tt.shifted)
			continue
		}
		if got != tt.want {
			t.Errorf("Rune(0x%02x, shifted=%v) = %q, want %q", tt.keyCode, tt.shifted, got, tt.want)
		}
	}
}

func TestLayout_IgnoredAndUnmappedKeys(t *testing.T) {
	layout, err := Get("us")
	if err != nil {
		t.Fatalf("Get(us) error: %v", err)
	}

	// 0x3A is F1, explicitly ignored.
	if _, ok := layout.Rune(0x3A, false); ok {
		t.Error("expected ignored key F1 (0x3A) to return no rune")
	}

	// 0xFF is not mapped by any layout.
	if _, ok := layout.Rune(0xFF, false); ok {
		t.Error("expected unmapped key 0xFF to return no rune")
	}
}

func TestGet_UnknownLayout(t *testing.T) {
	_, err := Get("klingon")
	if err == nil {
		t.Error("expected error for unknown layout")
	}
}

func TestGet_ConcurrentAccess(t *testing.T) {
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, name := range []string{"us", "es"} {
				layout, err := Get(name)
				if err != nil {
					t.Errorf("Get(%s) error: %v", name, err)
					return
				}
				if _, ok := layout.Rune(0x04, false); !ok {
					t.Errorf("Get(%s): expected key 0x04 to be mapped", name)
					return
				}
			}
		}()
	}
	wg.Wait()
}
