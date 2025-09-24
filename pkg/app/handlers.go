package app

import (
	"fmt"

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
	scannerManager.SetOnScanCallback(h.createBarcodeHandler(haManager))

	// Set up scanner connection change callback (includes scanner ID)
	scannerManager.SetOnConnectionChangeCallback(h.createConnectionHandler(services, haManager))
}

// createBarcodeHandler creates a handler for barcode scan events
func (h *EventHandlers) createBarcodeHandler(haManager *homeassistant.Integration) func(string, string) {
	return func(scannerID, barcode string) {
		logger := h.logger.WithFields(map[string]interface{}{
			"scanner_id": scannerID,
			"barcode":    barcode,
			"length":     len(barcode),
		})
		logger.Info("Barcode scanned")

		// Publish to Home Assistant
		if err := haManager.PublishBarcode(scannerID, barcode); err != nil {
			logger.WithError(err).Error("Failed to publish barcode to Home Assistant")
		}
	}
}

// createConnectionHandler creates a handler for scanner connection change events
func (h *EventHandlers) createConnectionHandler(
	services *ServiceManager,
	haManager *homeassistant.Integration,
) func(string, bool) {
	return func(scannerID string, connected bool) {
		logger := h.logger.WithField("scanner_id", scannerID)
		scannerManager := services.GetScannerManager()
		if scannerManager == nil {
			logger.Error("Scanner manager service not available in connection handler")
			return
		}

		if connected {
			logger.Info("Scanner connected")

			// Get device info and pass to Home Assistant
			scannerInstance := scannerManager.GetScanner(scannerID)
			if scannerInstance != nil && scannerInstance.IsConnected() {
				if deviceInfo := scannerInstance.GetConnectedDeviceInfo(); deviceInfo != nil {
					logger.WithFields(map[string]interface{}{
						"manufacturer": deviceInfo.Manufacturer,
						"product":      deviceInfo.Product,
						"vendor_id":    fmt.Sprintf("%04x", deviceInfo.VendorID),
						"product_id":   fmt.Sprintf("%04x", deviceInfo.ProductID),
					}).Info("Scanner device detected")
					haManager.SetScannerDeviceInfo(scannerID, deviceInfo)
				} else {
					logger.Warn("Scanner connected but device info unavailable")
				}
			} else {
				logger.Error("Scanner instance not found or not connected - this indicates a bug")
			}
		} else {
			logger.Info("Scanner disconnected")
		}

		// Update Home Assistant sensor state
		if err := haManager.SetScannerConnected(scannerID, connected); err != nil {
			logger.WithError(err).Error("Failed to update Home Assistant sensor state")
		}
	}
}
