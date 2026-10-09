package xcode

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSchemeParsing(t *testing.T) {
	tests := []struct {
		name    string
		fixture string
		want    []Scheme
	}{
		{"project", "testdata/xcodebuild-list.json", []Scheme{
			{Name: "TheWholePicture-Debug"},
			{Name: "TheWholePicture-Production"},
			{Name: "Watch App"},
		}},
		{"workspace", "testdata/xcodebuild-list-workspace.json", []Scheme{
			{Name: "SyncUps"},
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schemes, err := parseSchemesWith(func() ([]byte, error) {
				return os.ReadFile(tt.fixture)
			})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(schemes, tt.want) {
				t.Errorf("got %v, want %v", schemes, tt.want)
			}
		})
	}
}

func TestSchemesFromDisk(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"App/App.xcodeproj/xcshareddata/xcschemes/Mine.xcscheme":             "",
		"App/App.xcodeproj/xcuserdata/me.xcuserdatad/xcschemes/App.xcscheme": "",
		"packages/Net/Package.swift":                                         "let package = Package(name: \"Net\", products: [.library(name: \"Net\", targets: [\"Net\"]), .library(\n    name: \"NetMocks\", targets: [])])",
		"packages/Tool/Package.swift":                                        "let package = Package(\n    name: \"Tool\",\n    targets: [])",
		"packages/Net/.build/checkouts/Remote/Package.swift":                 `.library(name: "Remote")`,
	}
	for f, body := range files {
		if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(f)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, f), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if got, want := schemeFiles(filepath.Join(dir, "App/App.xcodeproj")), []string{"App", "Mine"}; !reflect.DeepEqual(got, want) {
		t.Errorf("scheme files: got %v, want %v", got, want)
	}
	if got, want := schemeFiles(filepath.Join(dir, "App/App.xcworkspace")), []string{"App", "Mine"}; !reflect.DeepEqual(got, want) {
		t.Errorf("workspace scheme files: got %v, want the sibling project's", got)
	}
	if got, want := localPackageProducts(dir), []string{"Net", "NetMocks", "Tool"}; !reflect.DeepEqual(got, want) {
		t.Errorf("products: got %v, want %v", got, want)
	}

	// xcodebuild -list (fallback) lists local products among the schemes; they move behind.
	listed := []Scheme{{Name: "Net"}, {Name: "App"}}
	want := []Scheme{{Name: "App"}, {Name: "Net", Package: true}, {Name: "NetMocks", Package: true}, {Name: "Tool", Package: true}}
	if got := withPackages(listed, localPackageProducts(dir)); !reflect.DeepEqual(got, want) {
		t.Errorf("with packages: got %v, want %v", got, want)
	}
}

func TestDefaultScheme(t *testing.T) {
	tests := []struct {
		name    string
		project string
		schemes []Scheme
		want    Scheme
		wantOK  bool
	}{
		{"named after container", "/src/Puzzle.xcworkspace", []Scheme{{Name: "Alamofire"}, {Name: "Puzzle"}}, Scheme{Name: "Puzzle"}, true},
		{"first when no name matches", "/src/Puzzle.xcodeproj", []Scheme{{Name: "App-Debug"}, {Name: "App-Release"}}, Scheme{Name: "App-Debug"}, true},
		{"no schemes", "/src/Puzzle.xcodeproj", nil, Scheme{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := DefaultScheme(Project{Name: tt.project}, tt.schemes)
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("got %v %v, want %v %v", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}
