package scanner

import "errors"

var (
	ErrDeviceOpenFailed       = errors.New("failed to open device")
	ErrReconnectionInProgress = errors.New("reconnection already in progress")
	ErrScannerStopped         = errors.New("scanner stopped")
)
