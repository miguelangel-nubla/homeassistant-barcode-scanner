package scanner

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed layouts/*.yaml
var layoutFiles embed.FS

type LayoutDefinition struct {
	Name        string              `yaml:"name"`
	Description string              `yaml:"description"`
	Letters     map[uint8][2]string `yaml:"letters"`
	Numbers     map[uint8][2]string `yaml:"numbers"`
	Symbols     map[uint8][2]string `yaml:"symbols"`
	Ignored     []uint8             `yaml:"ignored"`
}

type LoadedKeyboardLayout struct {
	Name        string
	Description string
	Letters     map[byte][2]byte
	Numbers     map[byte][2]byte
	Symbols     map[byte][2]byte
	Ignored     []byte
}

var (
	loadedLayouts map[string]LoadedKeyboardLayout
)

func convertStringMappings(source map[byte][2]string, target map[byte][2]byte) {
	for keyCode, chars := range source {
		if len(chars) == 2 && chars[0] != "" && chars[1] != "" {
			target[keyCode] = [2]byte{chars[0][0], chars[1][0]}
		}
	}
}

func processLayoutFile(entry os.DirEntry) (string, LoadedKeyboardLayout, error) {
	layoutName := strings.TrimSuffix(entry.Name(), ".yaml")
	layoutPath := filepath.Join("layouts", entry.Name())

	data, err := layoutFiles.ReadFile(layoutPath)
	if err != nil {
		return "", LoadedKeyboardLayout{}, fmt.Errorf("failed to read layout file %s: %w", layoutPath, err)
	}

	var layoutDef LayoutDefinition
	if err := yaml.Unmarshal(data, &layoutDef); err != nil {
		return "", LoadedKeyboardLayout{}, fmt.Errorf("failed to parse layout file %s: %w", layoutPath, err)
	}

	layout := LoadedKeyboardLayout{
		Name:        layoutDef.Name,
		Description: layoutDef.Description,
		Letters:     make(map[byte][2]byte),
		Numbers:     make(map[byte][2]byte),
		Symbols:     make(map[byte][2]byte),
		Ignored:     layoutDef.Ignored,
	}

	convertStringMappings(layoutDef.Letters, layout.Letters)
	convertStringMappings(layoutDef.Numbers, layout.Numbers)
	convertStringMappings(layoutDef.Symbols, layout.Symbols)

	return layoutName, layout, nil
}

func LoadKeyboardLayouts() error {
	loadedLayouts = make(map[string]LoadedKeyboardLayout)

	entries, err := layoutFiles.ReadDir("layouts")
	if err != nil {
		return fmt.Errorf("failed to read layouts directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}

		layoutName, layout, err := processLayoutFile(entry)
		if err != nil {
			return err
		}

		loadedLayouts[layoutName] = layout
	}

	if _, exists := loadedLayouts["us"]; !exists {
		return fmt.Errorf("required US keyboard layout not found")
	}

	return nil
}

func GetKeyboardLayout(name string) (LoadedKeyboardLayout, error) {
	if loadedLayouts == nil {
		if err := LoadKeyboardLayouts(); err != nil {
			return LoadedKeyboardLayout{}, err
		}
	}

	if layout, exists := loadedLayouts[name]; exists {
		return layout, nil
	}

	if usLayout, exists := loadedLayouts["us"]; exists {
		return usLayout, nil
	}

	return LoadedKeyboardLayout{}, fmt.Errorf("keyboard layout '%s' not found and US fallback unavailable", name)
}

func GetAvailableLayouts() []string {
	if loadedLayouts == nil {
		if err := LoadKeyboardLayouts(); err != nil {
			return []string{}
		}
	}

	var layouts []string
	for name := range loadedLayouts {
		layouts = append(layouts, name)
	}

	slices.Sort(layouts)
	return layouts
}

func IsLayoutAvailable(name string) bool {
	availableLayouts := GetAvailableLayouts()
	return slices.Contains(availableLayouts, name)
}
