package xcode

import (
	"slices"
	"testing"
)

func TestXcodebuildArgs(t *testing.T) {
	scheme := Scheme{Name: "Puzzle"}
	simulator := Simulator{UDID: "ABC-123"}

	tests := []struct {
		name    string
		project Project
		want    []string
	}{
		{"project", Project{Name: "Puzzle.xcodeproj"}, []string{
			"-project", "Puzzle.xcodeproj", "-scheme", "Puzzle", "-destination", "platform=iOS Simulator,id=ABC-123",
		}},
		{"workspace", Project{Name: "Puzzle.xcworkspace"}, []string{
			"-workspace", "Puzzle.xcworkspace", "-scheme", "Puzzle", "-destination", "platform=iOS Simulator,id=ABC-123",
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := xcodebuildArgs(tt.project, scheme, simulator); !slices.Equal(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
	t.Run("mac", func(t *testing.T) {
		want := []string{"-project", "X.xcodeproj", "-scheme", "Puzzle", "-destination", "platform=macOS"}
		if got := xcodebuildArgs(Project{Name: "X.xcodeproj"}, scheme, Mac); !slices.Equal(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
}
