package scanner

import "errors"

// ErrDeviceOpenFailed indicates a matching device was found but could not be
// opened, which usually points to missing permissions (udev rules on Linux).
var ErrDeviceOpenFailed = errors.New("failed to open device")
