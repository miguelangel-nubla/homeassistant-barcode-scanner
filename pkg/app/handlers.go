package app

import (
	"github.com/sirupsen/logrus"

	"github.com/miguelangel-nubla/homeassistant-barcode-scanner/pkg/homeassistant"
	"github.com/miguelangel-nubla/homeassistant-barcode-scanner/pkg/scanner"
)

// EventHandlers manages all event handling logic
type EventHandlers struct {
	logger *logrus.Logger
}

// NewEventHandlers creates a new event handlers instance
func NewEventHandlers(logger *logrus.Logger) *EventHandlers {
	return &EventHandlers{
		logger: logger,
	}
}

// SetupHandlers configures all event handlers between services
func (h *EventHandlers) SetupHandlers(
	services *ServiceManager,
	haManager *homeassistant.Integration,
	scannerManager *scanner.ScannerManager,
) {
	// Set up barcode scan callback (includes scanner ID)
	scannerManager.SetOnScanCallback(h.createMultiBarcodeHandler(haManager))

	// Set up scanner connection change callback (includes scanner ID)
	scannerManager.SetOnConnectionChangeCallback(h.createMultiConnectionHandler(services, haManager))
}

// createMultiBarcodeHandler creates a handler for multi-scanner barcode scan events
func (h *EventHandlers) createMultiBarcodeHandler(haManager *homeassistant.Integration) func(string, string) {
	return func(scannerID, barcode string) {
		h.logger.Infof("Barcode scanned from %s: %s", scannerID, barcode)
		h.logger.Debugf("Barcode details - scanner: %s, length: %d, hex: %x", scannerID, len(barcode), []byte(barcode))

		// Publish to Home Assistant
		h.logger.Debugf("Sending barcode to Home Assistant integration for scanner %s...", scannerID)
		if err := haManager.PublishBarcode(scannerID, barcode); err != nil {
			h.logger.Errorf("Failed to publish barcode from scanner %s to Home Assistant: %v", scannerID, err)
		} else {
			h.logger.Debugf("Barcode from scanner %s successfully sent to Home Assistant", scannerID)
		}
	}
}

// createMultiConnectionHandler creates a handler for multi-scanner connection change events
func (h *EventHandlers) createMultiConnectionHandler(
	services *ServiceManager,
	haManager *homeassistant.Integration,
) func(string, bool) {
	return func(scannerID string, connected bool) {
		scannerManager := services.GetScannerManager()
		if scannerManager == nil {
			h.logger.Error("Scanner manager service not available in connection handler")
			return
		}

		if connected {
			h.logger.Infof("Scanner %s connected", scannerID)

			// Get device info and pass to Home Assistant
			scannerInstance := scannerManager.GetScanner(scannerID)
			if scannerInstance != nil {
				h.logger.Debugf("Scanner instance found for %s, connected: %v", scannerID, scannerInstance.IsConnected())
				if scannerInstance.IsConnected() {
					if deviceInfo := scannerInstance.GetConnectedDeviceInfo(); deviceInfo != nil {
						h.logger.Infof("Scanner %s device: %s %s (VID:PID %04x:%04x)",
							scannerID, deviceInfo.Manufacturer, deviceInfo.Product, deviceInfo.VendorID, deviceInfo.ProductID)
						h.logger.Debugf("Creating Home Assistant device for scanner %s", scannerID)
						haManager.SetScannerDeviceInfo(scannerID, deviceInfo)
					} else {
						h.logger.Warnf("Scanner %s is connected but device info is nil", scannerID)
					}
				} else {
					h.logger.Warnf("Scanner %s instance found but not connected", scannerID)
				}
			} else {
				h.logger.Errorf("Scanner instance not found for %s - this is a bug!", scannerID)
			}
		} else {
			h.logger.Infof("Scanner %s disconnected", scannerID)
		}

		// Update Home Assistant sensor state
		if err := haManager.SetScannerConnected(scannerID, connected); err != nil {
			h.logger.Errorf("Failed to update HA sensor state for scanner %s: %v", scannerID, err)
		}
	}
}
