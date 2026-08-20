package scanner

import (
	"io"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/sirupsen/logrus"
)

func testLogger() *logrus.Logger {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	return logger
}

// report builds an 8-byte HID boot-protocol keyboard report.
func report(modifier byte, keys ...byte) []byte {
	data := make([]byte, 8)
	data[0] = modifier
	copy(data[2:], keys)
	return data
}

var releaseReport = report(0)

// barcodeAB is the expected result of typing keys 0x04, 0x05 on a US layout.
const barcodeAB = "ab"

// scanProcessor returns a processor plus a pointer to the barcodes it emits.
func scanProcessor(t *testing.T, terminationChar, layout string) (p *HIDProcessor, scans *[]string) {
	t.Helper()
	p = NewHIDProcessor(terminationChar, layout, testLogger())
	scans = &[]string{}
	p.SetOnScanCallback(func(barcode string) {
		*scans = append(*scans, barcode)
	})
	return p, scans
}

// typeKeys feeds each key as a press report followed by a release report,
// the way real scanners emit their reports.
func typeKeys(p *HIDProcessor, modifier byte, keys ...byte) {
	for _, key := range keys {
		p.ProcessData(report(modifier, key))
		p.ProcessData(releaseReport)
	}
}

func TestScanBarcodeWithEnterTermination(t *testing.T) {
	p, scans := scanProcessor(t, "enter", "us")

	// "abc123"
	typeKeys(p, 0, 0x04, 0x05, 0x06, 0x1e, 0x1f, 0x20)
	p.ProcessData(report(0, hidKeyEnter))

	if len(*scans) != 1 {
		t.Fatalf("expected 1 scan, got %d: %v", len(*scans), *scans)
	}
	if (*scans)[0] != "abc123" {
		t.Errorf("expected barcode 'abc123', got %q", (*scans)[0])
	}
}

func TestScanBarcodeWithTabTermination(t *testing.T) {
	p, scans := scanProcessor(t, "tab", "us")

	typeKeys(p, 0, 0x04, 0x05)
	p.ProcessData(report(0, hidKeyTab))

	if len(*scans) != 1 || (*scans)[0] != barcodeAB {
		t.Fatalf("expected scan 'ab', got %v", *scans)
	}
}

func TestShiftedCharacters(t *testing.T) {
	tests := []struct {
		name     string
		modifier byte
	}{
		{"left shift", 0x02},
		{"right shift", 0x20},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, scans := scanProcessor(t, "enter", "us")

			typeKeys(p, tt.modifier, 0x04)    // 'A'
			typeKeys(p, 0, 0x05)              // 'b'
			typeKeys(p, tt.modifier, 0x1e)    // '!'
			p.ProcessData(report(0, hidKeyEnter))

			if len(*scans) != 1 || (*scans)[0] != "Ab!" {
				t.Fatalf("expected scan 'Ab!', got %v", *scans)
			}
		})
	}
}

func TestSpanishLayoutProducesValidUTF8(t *testing.T) {
	p, scans := scanProcessor(t, "enter", "es")

	typeKeys(p, 0, 0x33)    // ñ
	typeKeys(p, 0x02, 0x33) // Ñ
	typeKeys(p, 0, 0x31)    // ç
	typeKeys(p, 0x02, 0x2E) // ¿
	p.ProcessData(report(0, hidKeyEnter))

	if len(*scans) != 1 {
		t.Fatalf("expected 1 scan, got %v", *scans)
	}

	got := (*scans)[0]
	if got != "ñÑç¿" {
		t.Errorf("expected barcode 'ñÑç¿', got %q", got)
	}
	if !utf8.ValidString(got) {
		t.Errorf("expected valid UTF-8, got %q", got)
	}
}

func TestKeyHeldAcrossReportsIsNotDuplicated(t *testing.T) {
	p, scans := scanProcessor(t, "enter", "us")

	// The same key pressed in three consecutive reports without a release
	// must produce a single character.
	p.ProcessData(report(0, 0x04))
	p.ProcessData(report(0, 0x04))
	p.ProcessData(report(0, 0x04))
	p.ProcessData(report(0, hidKeyEnter))

	if len(*scans) != 1 || (*scans)[0] != "a" {
		t.Fatalf("expected scan 'a', got %v", *scans)
	}
}

func TestRepeatedCharacterWithReleaseInBetween(t *testing.T) {
	p, scans := scanProcessor(t, "enter", "us")

	// "aa": same key twice, separated by a release report.
	typeKeys(p, 0, 0x04, 0x04)
	p.ProcessData(report(0, hidKeyEnter))

	if len(*scans) != 1 || (*scans)[0] != "aa" {
		t.Fatalf("expected scan 'aa', got %v", *scans)
	}
}

func TestMultipleKeysInSingleReport(t *testing.T) {
	p, scans := scanProcessor(t, "enter", "us")

	p.ProcessData(report(0, 0x04, 0x05, 0x06))
	p.ProcessData(report(0, hidKeyEnter))

	if len(*scans) != 1 || (*scans)[0] != "abc" {
		t.Fatalf("expected scan 'abc', got %v", *scans)
	}
}

func TestRolloverKeepsNewKeysOnly(t *testing.T) {
	p, scans := scanProcessor(t, "enter", "us")

	// Overlapping reports as produced by fast typing (n-key rollover):
	// [a], [a,b], [b] must produce "ab", not "abb".
	p.ProcessData(report(0, 0x04))
	p.ProcessData(report(0, 0x04, 0x05))
	p.ProcessData(report(0, 0x05))
	p.ProcessData(report(0, hidKeyEnter))

	if len(*scans) != 1 || (*scans)[0] != barcodeAB {
		t.Fatalf("expected scan 'ab', got %v", *scans)
	}
}

func TestTimeoutFinalizesWithoutTerminationChar(t *testing.T) {
	p, scans := scanProcessor(t, "none", "us")

	typeKeys(p, 0, 0x04, 0x05)

	// Nothing should be emitted while input is fresh.
	p.CheckTimeout()
	if len(*scans) != 0 {
		t.Fatalf("expected no scan before timeout, got %v", *scans)
	}

	p.lastActivity = time.Now().Add(-2 * scanTimeout)
	p.CheckTimeout()

	if len(*scans) != 1 || (*scans)[0] != barcodeAB {
		t.Fatalf("expected scan 'ab' after timeout, got %v", *scans)
	}
}

func TestTerminationKeyIgnoredWhenConfiguredNone(t *testing.T) {
	p, scans := scanProcessor(t, "none", "us")

	typeKeys(p, 0, 0x04)
	p.ProcessData(report(0, hidKeyEnter))

	if len(*scans) != 0 {
		t.Fatalf("expected enter to not terminate when configured 'none', got %v", *scans)
	}
}

func TestIgnoredKeysProduceNothing(t *testing.T) {
	p, scans := scanProcessor(t, "enter", "us")

	typeKeys(p, 0, 0x04)
	typeKeys(p, 0, 0x3A) // F1, ignored
	typeKeys(p, 0, 0x05)
	p.ProcessData(report(0, hidKeyEnter))

	if len(*scans) != 1 || (*scans)[0] != barcodeAB {
		t.Fatalf("expected scan 'ab', got %v", *scans)
	}
}

func TestBarcodeTruncatedAtMaxLength(t *testing.T) {
	p, scans := scanProcessor(t, "enter", "us")

	for range maxBarcodeLength + 50 {
		typeKeys(p, 0, 0x04)
	}
	p.ProcessData(report(0, hidKeyEnter))

	if len(*scans) != 1 {
		t.Fatalf("expected 1 scan, got %d", len(*scans))
	}
	if got := len((*scans)[0]); got != maxBarcodeLength {
		t.Errorf("expected barcode truncated to %d characters, got %d", maxBarcodeLength, got)
	}
	if (*scans)[0] != strings.Repeat("a", maxBarcodeLength) {
		t.Error("expected truncated barcode to contain the first characters")
	}
}

func TestResetDiscardsPartialScan(t *testing.T) {
	p, scans := scanProcessor(t, "enter", "us")

	typeKeys(p, 0, 0x04, 0x05)
	p.Reset()
	p.ProcessData(report(0, hidKeyEnter))

	if len(*scans) != 0 {
		t.Fatalf("expected no scan after reset, got %v", *scans)
	}
}

func TestEmptyScanIsNotEmitted(t *testing.T) {
	p, scans := scanProcessor(t, "enter", "us")

	// Termination without any prior input.
	p.ProcessData(report(0, hidKeyEnter))
	// Whitespace-only input is trimmed to nothing.
	typeKeys(p, 0, 0x2C)
	p.ProcessData(report(0, hidKeyEnter))

	if len(*scans) != 0 {
		t.Fatalf("expected no scans, got %v", *scans)
	}
}

func TestShortReportsAreIgnored(t *testing.T) {
	p, scans := scanProcessor(t, "enter", "us")

	p.ProcessData([]byte{})
	p.ProcessData([]byte{0x00})
	p.ProcessData([]byte{0x00, 0x00})
	p.ProcessData(report(0, hidKeyEnter))

	if len(*scans) != 0 {
		t.Fatalf("expected no scans from short reports, got %v", *scans)
	}
}

func TestUnknownLayoutFallsBackToUS(t *testing.T) {
	p, scans := scanProcessor(t, "enter", "nonexistent")

	if p.layoutName != "us" {
		t.Errorf("expected fallback layout 'us', got %q", p.layoutName)
	}

	typeKeys(p, 0, 0x04)
	p.ProcessData(report(0, hidKeyEnter))

	if len(*scans) != 1 || (*scans)[0] != "a" {
		t.Fatalf("expected scan 'a' via fallback layout, got %v", *scans)
	}
}

func TestLayoutNameCaseInsensitive(t *testing.T) {
	p := NewHIDProcessor("enter", "ES", testLogger())
	if p.layoutName != "es" {
		t.Errorf("expected layout name normalized to 'es', got %q", p.layoutName)
	}
}
