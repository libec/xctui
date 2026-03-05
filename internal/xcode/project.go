package xcode

import (
	"fmt"
	"path/filepath"
)

func FindProject(startDir string) (string, error) {
	res, err := filepath.Glob(filepath.Join(startDir, "*.xcodeproj"))
	if err != nil {
		return "", fmt.Errorf("search error: %w", err)
	}
	if len(res) == 0 {
		return "", fmt.Errorf("no xcode project found in %s", startDir)
	}

	return res[0], nil
}
