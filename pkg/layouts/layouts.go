package layouts

import (
	"embed"
	"fmt"
	"slices"
	"strings"
)

//go:embed *.yaml
var layoutFiles embed.FS

// GetAvailableLayouts returns a list of available keyboard layout names
func GetAvailableLayouts() ([]string, error) {
	entries, err := layoutFiles.ReadDir(".")
	if err != nil {
		return nil, fmt.Errorf("failed to read embedded layouts directory: %w", err)
	}

	var layouts []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		layoutName := strings.TrimSuffix(entry.Name(), ".yaml")
		layouts = append(layouts, layoutName)
	}

	slices.Sort(layouts)
	return layouts, nil
}
