package xcode

import (
	"fmt"
	"path/filepath"
)

// Project is the container xcodebuild works on: a .xcworkspace or a .xcodeproj.
type Project struct {
	Name string // path to the container
	Root string // the folder xctui started in; local packages are looked up below it
}

// FindProject looks in startDir, then one folder down (repos often keep the app
// in a subfolder next to packages/ and scripts/). At each depth a workspace wins:
// CocoaPods and similar tools only build through it.
func FindProject(startDir string) (Project, error) {
	for _, depth := range []string{"", "*"} {
		for _, pattern := range []string{"*.xcworkspace", "*.xcodeproj"} {
			res, err := filepath.Glob(filepath.Join(startDir, depth, pattern))
			if err != nil {
				return Project{}, fmt.Errorf("search error: %w", err)
			}
			for _, path := range res {
				// Every .xcodeproj holds a project.xcworkspace; that is not a container.
				if filepath.Ext(filepath.Dir(path)) != ".xcodeproj" {
					return Project{Name: path, Root: startDir}, nil
				}
			}
		}
	}
	return Project{}, fmt.Errorf("no .xcworkspace or .xcodeproj found in %s or one folder down", startDir)
}

// args returns the flag that tells xcodebuild which container to use.
func (p Project) args() []string {
	if filepath.Ext(p.Name) == ".xcworkspace" {
		return []string{"-workspace", p.Name}
	}
	return []string{"-project", p.Name}
}
