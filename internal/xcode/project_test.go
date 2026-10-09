package xcode

import (
	"os"
	"path/filepath"
	"slices"
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
		{"workspace preferred over project", func(dir string) {
			for _, name := range []string{"MyApp.xcodeproj", "MyApp.xcworkspace"} {
				if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
					t.Fatal(err)
				}
			}
		}, "MyApp.xcworkspace", false},
		{"one folder down, skipping the xcodeproj's inner workspace", func(dir string) {
			if err := os.MkdirAll(filepath.Join(dir, "App", "App.xcodeproj", "project.xcworkspace"), 0o755); err != nil {
				t.Fatal(err)
			}
		}, "App/App.xcodeproj", false},
		{"start dir wins over a subfolder", func(dir string) {
			for _, name := range []string{"Top.xcodeproj", "Sub/Sub.xcworkspace"} {
				if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
					t.Fatal(err)
				}
			}
		}, "Top.xcodeproj", false},
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
			if result.Name != expected {
				t.Errorf("FindProject(%q) = %q, want %q", tmpDir, result, expected)
			}

		})
	}
}

func TestProjectArgs(t *testing.T) {
	tests := []struct {
		project Project
		want    []string
	}{
		{Project{Name: "/src/MyApp.xcworkspace"}, []string{"-workspace", "/src/MyApp.xcworkspace"}},
		{Project{Name: "/src/MyApp.xcodeproj"}, []string{"-project", "/src/MyApp.xcodeproj"}},
	}

	for _, tt := range tests {
		t.Run(tt.project.Name, func(t *testing.T) {
			if got := tt.project.args(); !slices.Equal(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}
