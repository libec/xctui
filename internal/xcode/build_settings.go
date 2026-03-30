package xcode

import (
	"fmt"
	"os/exec"
	"strings"
)

type BuildSettings struct {
	AppPath  string
	BundleID string
}

func ParseBuildSettings() (BuildSettings, error) {
	return parseBuildSettings(func() ([]byte, error) {
		return exec.Command("xcodebuild", "-showBuildSettings").Output()
	})
}

const (
	buildProductsDirKey = "BUILT_PRODUCTS_DIR"
	fullProductNameKey  = "FULL_PRODUCT_NAME"
	bundleIDKey         = "PRODUCT_BUNDLE_IDENTIFIER"
)

func parseBuildSettings(command func() ([]byte, error)) (BuildSettings, error) {
	output, err := command()
	if err != nil {
		return BuildSettings{}, err
	}
	lines := strings.Split(string(output), "\n")

	lineDictionaries := make(map[string]string)
	for _, line := range lines {
		before, after, found := strings.Cut(line, " = ")
		if !found {
			continue
		}
		lineDictionaries[strings.TrimSpace(before)] = strings.TrimSpace(after)
	}

	appPath := lineDictionaries[buildProductsDirKey]
	productName := lineDictionaries[fullProductNameKey]
	bundleID := lineDictionaries[bundleIDKey]

	if appPath == "" {
		return BuildSettings{}, fmt.Errorf("missing %s", buildProductsDirKey)
	}

	if productName == "" {
		return BuildSettings{}, fmt.Errorf("missing %s", fullProductNameKey)
	}

	if bundleID == "" {
		return BuildSettings{}, fmt.Errorf("missing %s", bundleIDKey)
	}

	return BuildSettings{AppPath: fmt.Sprintf("%s/%s", appPath, productName), BundleID: bundleID}, nil
}
