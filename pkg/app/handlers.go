package app

import (
	"fmt"

	"github.com/sirupsen/logrus"

	"github.com/miguelangel-nubla/homeassistant-barcode-scanner/pkg/homeassistant"
	"github.com/miguelangel-nubla/homeassistant-barcode-scanner/pkg/scanner"
)

type EventHandlers struct {
	logger *logrus.Logger
}

func NewEventHandlers(logger *logrus.Logger) *EventHandlers {
	return &EventHandlers{
		logger: logger,
	}
}

func (h *EventHandlers) SetupHandlers(
	services *ServiceManager,
	haManager *homeassistant.Integration,
	scannerManager *scanner.ScannerManager,
) {
	scannerManager.SetOnScanCallback(h.createBarcodeHandler(haManager))

	scannerManager.SetOnConnectionChangeCallback(h.createConnectionHandler(services, haManager))
}

func (h *EventHandlers) createBarcodeHandler(haManager *homeassistant.Integration) func(string, string) {
	return func(scannerID, barcode string) {
		logger := h.logger.WithFields(map[string]any{
			"scanner_id": scannerID,
			"barcode":    barcode,
			"length":     len(barcode),
		})
		logger.Info("Barcode scanned")

		if err := haManager.PublishBarcode(scannerID, barcode); err != nil {
			logger.WithError(err).Error("Failed to publish barcode to Home Assistant")
		}
	}
}

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
			scannerInstance := scannerManager.GetScanner(scannerID)
			if scannerInstance != nil && scannerInstance.IsConnected() {
				if deviceInfo := scannerInstance.GetConnectedDeviceInfo(); deviceInfo != nil {
					fields := map[string]any{
						"manufacturer": deviceInfo.Manufacturer,
						"product":      deviceInfo.Product,
						"vendor_id":    fmt.Sprintf("%04x", deviceInfo.VendorID),
						"product_id":   fmt.Sprintf("%04x", deviceInfo.ProductID),
						"interface":    deviceInfo.Interface,
						"serial":       deviceInfo.Serial,
					}
					logger.WithFields(fields).Info("Scanner device detected")
					haManager.SetScannerDeviceInfo(scannerID, deviceInfo)
				} else {
					logger.Warn("Scanner connected but device info unavailable")
				}
			} else {
				logger.Error("Scanner instance not found or not connected - this indicates a bug")
			}
		} else {
			logger.Error("Scanner disconnected")
		}

		if err := haManager.SetScannerConnected(scannerID, connected); err != nil {
			logger.WithError(err).Error("Failed to update Home Assistant sensor state")
		}
	}
}
