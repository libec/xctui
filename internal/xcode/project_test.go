package xcode

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindProject(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(dir string)
		want    string
		wantErr bool
	}{
		{"found in start dir", func(dir string) {
			err := os.MkdirAll(filepath.Join(dir, "MyApp.xcodeproj"), 0o755)
			if err != nil {
				t.Fatal(err)
			}
		}, "MyApp.xcodeproj", false},
		{"not found", func(dir string) {}, "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			tt.setup(tmpDir)
			result, err := FindProject(tmpDir)
			if (err != nil) != tt.wantErr {
				t.Errorf("FindProject() returned unexpected error: %v", err)
			}
			expected := tt.want
			if expected != "" {
				expected = filepath.Join(tmpDir, expected)
			}
			if result != expected {
				t.Errorf("FindProject(%q) = %q, want %q", tmpDir, result, expected)
			}

		})
	}
}
