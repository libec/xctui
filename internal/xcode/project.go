package xcode

import (
	"fmt"
	"path/filepath"
)

type Project struct {
	Name string
}

func FindProject(startDir string) (Project, error) {
	res, err := filepath.Glob(filepath.Join(startDir, "*.xcodeproj"))
	if err != nil {
		return Project{}, fmt.Errorf("search error: %w", err)
	}
	if len(res) == 0 {
		return Project{}, fmt.Errorf("no xcode project found in %s", startDir)
	}

	return Project{Name: res[0]}, nil
}
