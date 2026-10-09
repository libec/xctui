package xcode

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
)

type BuildSettings struct {
	AppPath    string
	BundleID   string
	Executable string // the binary inside the app bundle; a macOS app runs from here
}

// ParseBuildSettings asks xcodebuild where the scheme's app lands when built for the destination.
func ParseBuildSettings(ctx context.Context, project Project, scheme Scheme, simulator Simulator) (BuildSettings, error) {
	return parseBuildSettings(func() ([]byte, error) {
		args := append([]string{"-showBuildSettings", "-json"}, xcodebuildArgs(project, scheme, simulator)...)
		return output(exec.CommandContext(ctx, "xcodebuild", args...))
	}, simulator.PlatformName())
}

const (
	buildProductsDirKey = "BUILT_PRODUCTS_DIR"
	fullProductNameKey  = "FULL_PRODUCT_NAME"
	bundleIDKey         = "PRODUCT_BUNDLE_IDENTIFIER"
	wrapperExtensionKey = "WRAPPER_EXTENSION"
	platformNameKey     = "PLATFORM_NAME"
	executablePathKey   = "EXECUTABLE_PATH"
)

// -showBuildSettings -json returns one entry per target the scheme builds.
type targetSettings struct {
	Target        string            `json:"target"`
	BuildSettings map[string]string `json:"buildSettings"`
}

// parseBuildSettings picks the app target built for platform ("iphonesimulator" or "macosx").
func parseBuildSettings(command func() ([]byte, error), platform string) (BuildSettings, error) {
	output, err := command()
	if err != nil {
		return BuildSettings{}, err
	}
	var targets []targetSettings
	if err := json.Unmarshal(output, &targets); err != nil {
		return BuildSettings{}, err
	}

	for _, target := range targets {
		settings := target.BuildSettings
		// Skip test bundles (.xctest), frameworks and extensions, and a watch app embedded in the iOS app.
		if settings[wrapperExtensionKey] != "app" || settings[platformNameKey] != platform {
			continue
		}
		for _, key := range []string{buildProductsDirKey, fullProductNameKey, bundleIDKey, executablePathKey} {
			if settings[key] == "" {
				return BuildSettings{}, fmt.Errorf("target %s: missing %s", target.Target, key)
			}
		}
		return BuildSettings{
			AppPath:    filepath.Join(settings[buildProductsDirKey], settings[fullProductNameKey]),
			BundleID:   settings[bundleIDKey],
			Executable: filepath.Join(settings[buildProductsDirKey], settings[executablePathKey]),
		}, nil
	}
	return BuildSettings{}, fmt.Errorf("the scheme builds no %s app", platform)
}
