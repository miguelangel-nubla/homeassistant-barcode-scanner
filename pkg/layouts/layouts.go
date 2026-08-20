// Package layouts is the single owner of embedded keyboard layout
// definitions. Both config validation and runtime HID decoding use this
// package, so the set of layouts they see is always identical.
package layouts

import (
	"embed"
	"fmt"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

//go:embed *.yaml
var layoutFiles embed.FS

// Fallback is the layout used when a requested layout does not exist.
const Fallback = "us"

// definition mirrors the YAML layout file structure.
type definition struct {
	Name        string              `yaml:"name"`
	Description string              `yaml:"description"`
	Letters     map[uint8][2]string `yaml:"letters"`
	Numbers     map[uint8][2]string `yaml:"numbers"`
	Symbols     map[uint8][2]string `yaml:"symbols"`
	Ignored     []uint8             `yaml:"ignored"`
}

// Layout maps HID key codes to the runes they produce.
type Layout struct {
	Name        string
	Description string
	// keys[code][0] is the unshifted rune, keys[code][1] the shifted one.
	keys    map[byte][2]rune
	ignored map[byte]struct{}
}

// Rune returns the rune produced by a key code, or (0, false) when the key
// is ignored or unmapped.
func (l Layout) Rune(keyCode byte, shifted bool) (rune, bool) {
	if _, ignored := l.ignored[keyCode]; ignored {
		return 0, false
	}
	chars, ok := l.keys[keyCode]
	if !ok {
		return 0, false
	}
	if shifted {
		return chars[1], true
	}
	return chars[0], true
}

var (
	loadOnce sync.Once
	loaded   map[string]Layout
	loadErr  error
)

func load() (map[string]Layout, error) {
	loadOnce.Do(func() {
		loaded, loadErr = loadAll()
	})
	return loaded, loadErr
}

func loadAll() (map[string]Layout, error) {
	entries, err := layoutFiles.ReadDir(".")
	if err != nil {
		return nil, fmt.Errorf("failed to read embedded layouts directory: %w", err)
	}

	result := make(map[string]Layout)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}

		name := strings.TrimSuffix(entry.Name(), ".yaml")
		layout, err := parseLayoutFile(entry.Name())
		if err != nil {
			return nil, err
		}
		result[name] = layout
	}

	if _, exists := result[Fallback]; !exists {
		return nil, fmt.Errorf("required fallback layout %q not found", Fallback)
	}

	return result, nil
}

func parseLayoutFile(filename string) (Layout, error) {
	data, err := layoutFiles.ReadFile(filename)
	if err != nil {
		return Layout{}, fmt.Errorf("failed to read layout file %s: %w", filename, err)
	}

	var def definition
	if err := yaml.Unmarshal(data, &def); err != nil {
		return Layout{}, fmt.Errorf("failed to parse layout file %s: %w", filename, err)
	}

	layout := Layout{
		Name:        def.Name,
		Description: def.Description,
		keys:        make(map[byte][2]rune),
		ignored:     make(map[byte]struct{}, len(def.Ignored)),
	}

	for _, section := range []map[uint8][2]string{def.Letters, def.Numbers, def.Symbols} {
		for keyCode, chars := range section {
			mapped, err := toRunePair(chars)
			if err != nil {
				return Layout{}, fmt.Errorf("layout file %s, key 0x%02x: %w", filename, keyCode, err)
			}
			layout.keys[keyCode] = mapped
		}
	}

	for _, keyCode := range def.Ignored {
		layout.ignored[keyCode] = struct{}{}
	}

	return layout, nil
}

func toRunePair(chars [2]string) ([2]rune, error) {
	unshifted, err := singleRune(chars[0])
	if err != nil {
		return [2]rune{}, err
	}
	shifted, err := singleRune(chars[1])
	if err != nil {
		return [2]rune{}, err
	}
	return [2]rune{unshifted, shifted}, nil
}

func singleRune(s string) (rune, error) {
	if utf8.RuneCountInString(s) != 1 {
		return 0, fmt.Errorf("mapping %q must be exactly one character", s)
	}
	r, _ := utf8.DecodeRuneInString(s)
	return r, nil
}

// Get returns the layout with the given name.
func Get(name string) (Layout, error) {
	all, err := load()
	if err != nil {
		return Layout{}, err
	}

	layout, exists := all[name]
	if !exists {
		return Layout{}, fmt.Errorf("keyboard layout %q not found", name)
	}
	return layout, nil
}

// GetAvailableLayouts returns the sorted names of all embedded layouts.
func GetAvailableLayouts() ([]string, error) {
	all, err := load()
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(all))
	for name := range all {
		names = append(names, name)
	}
	slices.Sort(names)
	return names, nil
}
