package common

import (
	"testing"
)

const devVersion = "dev"

func TestGetVersion_Development(t *testing.T) {
	// Reset version for test
	originalVersion := VERSION
	originalCommit := COMMIT
	defer func() {
		VERSION = originalVersion
		COMMIT = originalCommit
	}()

	VERSION = devVersion
	COMMIT = "unknown"

	version := GetVersion()

	if version != "1.0.0-dev" {
		t.Errorf("Expected development version '1.0.0-dev', got '%s'", version)
	}
}

func TestGetVersion_Release(t *testing.T) {
	// Reset version for test
	originalVersion := VERSION
	defer func() {
		VERSION = originalVersion
	}()

	VERSION = "1.2.3"

	version := GetVersion()

	if version != "1.2.3" {
		t.Errorf("Expected version '1.2.3', got '%s'", version)
	}
}

func TestGetVersion_Format(t *testing.T) {
	// Reset version for test
	originalVersion := VERSION
	originalCommit := COMMIT
	defer func() {
		VERSION = originalVersion
		COMMIT = originalCommit
	}()

	VERSION = "2.0.0"
	COMMIT = "def456"

	version := GetVersion()

	// Should be in format "VERSION-COMMIT" or similar
	if version == "" {
		t.Error("Expected version to be non-empty")
	}

	// Should not be just "dev"
	if version == devVersion {
		t.Error("Expected formatted version, got 'dev'")
	}
}
