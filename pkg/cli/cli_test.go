package cli

import (
	"testing"

	"github.com/karalabe/hid"
)

func TestGenerateScannerID(t *testing.T) {
	tests := []struct {
		name     string
		devName  string
		device   hid.DeviceInfo
		expected string
	}{
		{
			name:     "simple name",
			devName:  "Honeywell Scanner",
			device:   hid.DeviceInfo{},
			expected: "honeywell_scanner",
		},
		{
			name:     "special characters collapsed",
			devName:  "ACME  Corp. Scanner-3000!",
			device:   hid.DeviceInfo{},
			expected: "acme_corp_scanner_3000",
		},
		{
			name:     "leading digit gets prefix",
			devName:  "3000 Scanner",
			device:   hid.DeviceInfo{},
			expected: "scanner_3000_scanner",
		},
		{
			name:     "empty name falls back",
			devName:  "!!!",
			device:   hid.DeviceInfo{},
			expected: "scanner",
		},
		{
			name:     "interface suffix",
			devName:  "Scanner",
			device:   hid.DeviceInfo{Interface: 2},
			expected: "scanner_2",
		},
		{
			name:     "serial suffix",
			devName:  "Scanner",
			device:   hid.DeviceInfo{Serial: "AB-12"},
			expected: "scanner_ab_12",
		},
		{
			name:     "interface and serial suffix",
			devName:  "Scanner",
			device:   hid.DeviceInfo{Interface: 1, Serial: "XYZ"},
			expected: "scanner_1_xyz",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := generateScannerID(tt.devName, &tt.device)
			if got != tt.expected {
				t.Errorf("generateScannerID(%q) = %q, want %q", tt.devName, got, tt.expected)
			}
		})
	}
}

func TestGenerateScannerID_AlwaysValidYAMLKey(t *testing.T) {
	inputs := []string{"", "  ", "ñandú scanner", "123", "---", "UPPER CASE"}

	for _, input := range inputs {
		id := generateScannerID(input, &hid.DeviceInfo{})
		if id == "" {
			t.Errorf("generateScannerID(%q) produced empty ID", input)
			continue
		}
		if id[0] >= '0' && id[0] <= '9' {
			t.Errorf("generateScannerID(%q) = %q starts with a digit", input, id)
		}
		for _, r := range id {
			valid := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_'
			if !valid {
				t.Errorf("generateScannerID(%q) = %q contains invalid character %q", input, id, r)
				break
			}
		}
	}
}
