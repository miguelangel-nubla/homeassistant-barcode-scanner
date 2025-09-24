package scanner

import "errors"

var (
	// Connection errors
	ErrDeviceOpenFailed       = errors.New("failed to open device")
	ErrReconnectionInProgress = errors.New("reconnection already in progress")
	ErrScannerStopped         = errors.New("scanner stopped")
)
