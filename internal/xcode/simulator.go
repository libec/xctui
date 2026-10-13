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
	Device  bool // a physical device paired with this Mac
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
	switch {
	case s.IsMac():
		return "platform=macOS"
	case s.Device:
		return "platform=" + s.Platform() + ",id=" + s.UDID
	}
	return "platform=" + s.Platform() + " Simulator,id=" + s.UDID
}

// PlatformName is the PLATFORM_NAME build setting the destination builds for.
func (s Simulator) PlatformName() string {
	sdk := "iphone"
	switch s.Platform() {
	case "macOS":
		return "macosx"
	case "tvOS":
		sdk = "appletv"
	case "watchOS":
		sdk = "watch"
	case "visionOS":
		sdk = "xr"
	}
	if s.Device {
		return sdk + "os"
	}
	return sdk + "simulator"
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

// devicectlDevices is the part of `devicectl list devices --json-output -` xctui reads.
type devicectlDevices struct {
	Result struct {
		Devices []struct {
			ConnectionProperties struct {
				PairingState string `json:"pairingState"`
				TunnelState  string `json:"tunnelState"`
			} `json:"connectionProperties"`
			DeviceProperties struct {
				Name            string `json:"name"`
				OSVersionNumber string `json:"osVersionNumber"`
			} `json:"deviceProperties"`
			HardwareProperties struct {
				Platform string `json:"platform"`
				UDID     string `json:"udid"`
			} `json:"hardwareProperties"`
		} `json:"devices"`
	} `json:"result"`
}

// ParseSimulators returns the run destinations: My Mac, the paired devices in
// reach, then every available simulator, booted first, then by platform, then
// newest runtime first.
func ParseSimulators() ([]Simulator, error) {
	return parseSimulatorsWith(func() ([]byte, error) {
		return output(exec.Command("xcrun", "simctl", "list", "devices", "available", "-j"))
	}, func() ([]byte, error) {
		return output(exec.Command("xcrun", "devicectl", "list", "devices", "--json-output", "-", "-q"))
	})
}

func parseSimulatorsWith(simctl, devicectl func() ([]byte, error)) ([]Simulator, error) {
	output, err := simctl()
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

	paired, _ := parseDevices(devicectl) // best effort: devicectl needs Xcode 15, simulators run without it
	return slices.Concat([]Simulator{Mac}, paired, simulators), nil
}

// parseDevices keeps devicectl's order and drops what cannot take a build:
// unpaired devices and unavailable ones (switched off or out of reach). An idle
// device in reach flips between connected and disconnected; both stay.
func parseDevices(devicectl func() ([]byte, error)) ([]Simulator, error) {
	output, err := devicectl()
	if err != nil {
		return nil, err
	}
	var list devicectlDevices
	if err := json.Unmarshal(output, &list); err != nil {
		return nil, err
	}
	var devices []Simulator
	for _, d := range list.Result.Devices {
		if d.ConnectionProperties.PairingState != "paired" || d.ConnectionProperties.TunnelState == "unavailable" {
			continue
		}
		devices = append(devices, Simulator{
			Name:    d.DeviceProperties.Name,
			Runtime: d.HardwareProperties.Platform + " " + d.DeviceProperties.OSVersionNumber,
			UDID:    d.HardwareProperties.UDID, // xcodebuild's id; devicectl's own identifier differs
			Device:  true,
		})
	}
	return devices, nil
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

// DefaultSimulator picks a booted iOS simulator, else the first iPhone simulator
// on the newest runtime; a device is only ever chosen by hand. It expects the
// order ParseSimulators returns.
func DefaultSimulator(simulators []Simulator) (Simulator, bool) {
	for _, s := range simulators {
		if s.State == stateBooted && s.Platform() == "iOS" {
			return s, true
		}
	}
	for _, s := range simulators {
		if strings.HasPrefix(s.Name, "iPhone") && !s.Device {
			return s, true
		}
	}
	if len(simulators) > 0 {
		return simulators[0], true
	}
	return Simulator{}, false
}
