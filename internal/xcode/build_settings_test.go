package xcode

import (
	"os"
	"testing"
)

func TestBuildSettings(t *testing.T) {
	fixture := func(path string) func() ([]byte, error) {
		return func() ([]byte, error) { return os.ReadFile(path) }
	}
	inline := func(json string) func() ([]byte, error) {
		return func() ([]byte, error) { return []byte(json), nil }
	}
	puzzleDir := "/Users/libec/Library/Developer/Xcode/DerivedData/Puzzle-gkvusrxoqndrzjdchtgruoqfsjhc/Build/Products/Debug-iphonesimulator/"
	puzzle := BuildSettings{AppPath: puzzleDir + "Puzzle.app", BundleID: "com.example.Puzzle", Executable: puzzleDir + "Puzzle.app/Puzzle"}
	clipDir := "/Users/libec/Library/Developer/Xcode/DerivedData/ClipNinja-heohwiqbpzjnicduuygqhwasdobf/Build/Products/Debug/"
	clipNinja := BuildSettings{AppPath: clipDir + "ClipNinja.app", BundleID: "ninja.clip.app", Executable: clipDir + "ClipNinja.app/Contents/MacOS/ClipNinja"}

	tests := []struct {
		name     string
		command  func() ([]byte, error)
		platform string
		want     BuildSettings
		wantErr  bool
	}{
		{"app target", fixture("testdata/xcodebuild-showbuildsettings.json"), "iphonesimulator", puzzle, false},
		// Captured with the test action: PuzzleTests comes first, then Puzzle.
		{"test target listed before the app", fixture("testdata/xcodebuild-showbuildsettings-test.json"), "iphonesimulator", puzzle, false},
		{"macOS app", fixture("testdata/xcodebuild-showbuildsettings-macos.json"), "macosx", clipNinja, false},
		{"iOS settings asked for a Mac app", fixture("testdata/xcodebuild-showbuildsettings.json"), "macosx", BuildSettings{}, true},
		{"no app target", inline(`[{"target": "Lib", "buildSettings": {"WRAPPER_EXTENSION": "framework", "PLATFORM_NAME": "iphonesimulator"}}]`), "iphonesimulator", BuildSettings{}, true},
		{"device build", inline(`[{"target": "App", "buildSettings": {"WRAPPER_EXTENSION": "app", "PLATFORM_NAME": "iphoneos"}}]`), "iphonesimulator", BuildSettings{}, true},
		{"missing bundle id", inline(`[{"target": "App", "buildSettings": {"WRAPPER_EXTENSION": "app", "PLATFORM_NAME": "iphonesimulator", "BUILT_PRODUCTS_DIR": "/b", "FULL_PRODUCT_NAME": "App.app", "EXECUTABLE_PATH": "App.app/App"}}]`), "iphonesimulator", BuildSettings{}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseBuildSettings(tt.command, tt.platform)
			if (err != nil) != tt.wantErr {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}
