package tui

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestPrefs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "xctui", "prefs.json")

	if got := loadPrefs(path); len(got) != 0 {
		t.Errorf("missing file: got %v, want none", got)
	}

	puzzle := selection{Scheme: "Puzzle", Simulator: "A"}
	walk := selection{Scheme: "PhotoWalk", Simulator: "B"}
	for project, s := range map[string]selection{"/src/Puzzle.xcodeproj": puzzle, "/src/PhotoWalk.xcodeproj": walk} {
		if err := savePrefs(path, project, s); err != nil {
			t.Fatal(err)
		}
	}
	want := map[string]selection{"/src/Puzzle.xcodeproj": puzzle, "/src/PhotoWalk.xcodeproj": walk}
	if got := loadPrefs(path); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}

	if err := os.WriteFile(path, []byte("null"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := savePrefs(path, "/src/Puzzle.xcodeproj", puzzle); err != nil {
		t.Errorf("a file holding null should not break saving: %v", err)
	}
}
