package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// selection is what xctui remembers per project path.
type selection struct {
	Scheme    string `json:"scheme"`
	Simulator string `json:"simulator"` // UDID: names repeat across runtimes
}

// defaultPrefsPath is ~/Library/Application Support/xctui/prefs.json on macOS.
func defaultPrefsPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "" // remembering is optional
	}
	return filepath.Join(dir, "xctui", "prefs.json")
}

// loadPrefs returns the selections keyed by project path. A missing or broken file means none yet.
func loadPrefs(path string) map[string]selection {
	var prefs map[string]selection
	data, err := os.ReadFile(path)
	if err == nil {
		err = json.Unmarshal(data, &prefs)
	}
	if err != nil || prefs == nil { // a file holding "null" decodes to a nil map
		return map[string]selection{}
	}
	return prefs
}

func savePrefs(path, project string, s selection) error {
	prefs := loadPrefs(path)
	prefs[project] = s
	data, err := json.MarshalIndent(prefs, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
