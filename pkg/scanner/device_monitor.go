package scanner

import (
	"time"

	"github.com/karalabe/hid"
	"github.com/sirupsen/logrus"
)

// DeviceMonitor monitors USB HID device changes
type DeviceMonitor struct {
	logger        *logrus.Logger
	stopCh        chan struct{}
	changesCh     chan bool
	lastDevices   []hid.DeviceInfo
	targetChecker func(*hid.DeviceInfo) bool
	pollInterval  time.Duration
}

// NewDeviceMonitor creates a new device monitor
func NewDeviceMonitor(targetChecker func(*hid.DeviceInfo) bool, logger *logrus.Logger) *DeviceMonitor {
	return &DeviceMonitor{
		logger:        logger,
		stopCh:        make(chan struct{}),
		changesCh:     make(chan bool, 1),
		targetChecker: targetChecker,
		pollInterval:  200 * time.Millisecond,
	}
}

// Start begins monitoring for device changes
func (m *DeviceMonitor) Start() {
	m.lastDevices = hid.Enumerate(0, 0)
	go m.monitorLoop()
}

// Stop stops device monitoring
func (m *DeviceMonitor) Stop() {
	close(m.stopCh)
}

// Changes returns a channel that signals when target device availability changes
func (m *DeviceMonitor) Changes() <-chan bool {
	return m.changesCh
}

// monitorLoop continuously monitors for device changes
func (m *DeviceMonitor) monitorLoop() {
	defer m.logger.Debug("Device monitoring stopped")

	ticker := time.NewTicker(m.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
			currentDevices := hid.Enumerate(0, 0)

			wasAvailable := m.isTargetDeviceInList(m.lastDevices)
			isAvailable := m.isTargetDeviceInList(currentDevices)

			if wasAvailable != isAvailable {
				m.logger.Debugf("Device availability changed: was=%v, now=%v", wasAvailable, isAvailable)
				select {
				case m.changesCh <- isAvailable:
				default:
					// Channel full, skip this signal
				}
			}

			m.lastDevices = currentDevices
		}
	}
}

// isTargetDeviceInList checks if target device is in the device list
func (m *DeviceMonitor) isTargetDeviceInList(devices []hid.DeviceInfo) bool {
	for _, device := range devices {
		if m.targetChecker(&device) {
			return true
		}
	}
	return false
}
