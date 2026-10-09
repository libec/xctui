package xcode

import (
	"encoding/json"
	"os/exec"
	"slices"
	"strconv"
	"strings"
)

type Simulator struct {
	Name    string
	Runtime string // human-readable, e.g. "iOS 18.2"
	State   string
	UDID    string
}

const stateBooted = "Booted"

// Mac is the run destination for macOS schemes: the machine xctui runs on.
var Mac = Simulator{Name: "My Mac", Runtime: "macOS", UDID: "mac"}

func (s Simulator) IsMac() bool {
	return s.UDID == Mac.UDID
}

// Platform is the OS the destination runs: iOS, tvOS, watchOS, visionOS or macOS.
func (s Simulator) Platform() string {
	platform, _, _ := strings.Cut(s.Runtime, " ")
	switch platform {
	case "xrOS":
		return "visionOS"
	case "":
		return "iOS" // a bare UDID, as the build subcommand and tests pass
	}
	return platform
}

// Destination is the -destination value xcodebuild wants.
func (s Simulator) Destination() string {
	if s.IsMac() {
		return "platform=macOS"
	}
	return "platform=" + s.Platform() + " Simulator,id=" + s.UDID
}

// PlatformName is the PLATFORM_NAME build setting the destination builds for.
func (s Simulator) PlatformName() string {
	switch s.Platform() {
	case "macOS":
		return "macosx"
	case "tvOS":
		return "appletvsimulator"
	case "watchOS":
		return "watchsimulator"
	case "visionOS":
		return "xrsimulator"
	}
	return "iphonesimulator"
}

// platformOrder is how the destinations list groups platforms.
var platformOrder = []string{"iOS", "tvOS", "watchOS", "visionOS", "macOS"}

type simulatorDevices struct {
	Devices map[string][]rawDevice `json:"devices"`
}

type rawDevice struct {
	Name  string `json:"name"`
	State string `json:"state"`
	UDID  string `json:"udid"`
}

// ParseSimulators returns the run destinations: My Mac, then every available
// simulator, booted first, then by platform, then newest runtime first.
func ParseSimulators() ([]Simulator, error) {
	return parseSimulatorsWith(func() ([]byte, error) {
		return output(exec.Command("xcrun", "simctl", "list", "devices", "available", "-j"))
	})
}

func parseSimulatorsWith(execCommand func() ([]byte, error)) ([]Simulator, error) {
	output, err := execCommand()
	if err != nil {
		return nil, err
	}

	var devices simulatorDevices
	if err := json.Unmarshal(output, &devices); err != nil {
		return nil, err
	}
	var simulators []Simulator
	for runtime, rawDevices := range devices.Devices {
		name := runtimeName(runtime)
		for _, device := range rawDevices {
			d := Simulator{Name: device.Name, Runtime: name, State: device.State, UDID: device.UDID}
			simulators = append(simulators, d)
		}
	}
	sortSimulators(simulators)

	return append([]Simulator{Mac}, simulators...), nil
}

const runtimePrefix = "com.apple.CoreSimulator.SimRuntime."

// runtimeName turns "com.apple.CoreSimulator.SimRuntime.iOS-18-2" into "iOS 18.2".
func runtimeName(identifier string) string {
	name, ok := strings.CutPrefix(identifier, runtimePrefix)
	if !ok {
		return identifier
	}
	platform, version, ok := strings.Cut(name, "-")
	if !ok {
		return name
	}
	return platform + " " + strings.ReplaceAll(version, "-", ".")
}

// runtimeVersion turns "iOS 18.2" into [18 2], so 18.10 sorts after 18.9.
func runtimeVersion(runtime string) []int {
	_, version, _ := strings.Cut(runtime, " ")
	var parts []int
	for p := range strings.SplitSeq(version, ".") {
		n, _ := strconv.Atoi(p)
		parts = append(parts, n)
	}
	return parts
}

// sortSimulators puts booted devices first, then platforms in platformOrder, then
// newer runtimes first. The sort is stable, so devices of one runtime keep
// simctl's order (newest models first).
func sortSimulators(simulators []Simulator) {
	slices.SortStableFunc(simulators, func(a, b Simulator) int {
		aBooted, bBooted := a.State == stateBooted, b.State == stateBooted
		if aBooted != bBooted {
			if aBooted {
				return -1
			}
			return 1
		}
		if ap, bp := slices.Index(platformOrder, a.Platform()), slices.Index(platformOrder, b.Platform()); ap != bp {
			return ap - bp
		}
		// b before a: descending
		return slices.Compare(runtimeVersion(b.Runtime), runtimeVersion(a.Runtime))
	})
}

// Rename gives a simulator a new name; its UDID stays, so prefs keep pointing at it.
func Rename(simulator Simulator, name string) error {
	_, err := output(exec.Command("xcrun", "simctl", "rename", simulator.UDID, name))
	return err
}

// DefaultSimulator picks a booted iOS simulator, else the first iPhone on the
// newest runtime. It expects the order ParseSimulators returns.
func DefaultSimulator(simulators []Simulator) (Simulator, bool) {
	for _, s := range simulators {
		if s.State == stateBooted && s.Platform() == "iOS" {
			return s, true
		}
	}
	for _, s := range simulators {
		if strings.HasPrefix(s.Name, "iPhone") {
			return s, true
		}
	}
	if len(simulators) > 0 {
		return simulators[0], true
	}
	return Simulator{}, false
}
