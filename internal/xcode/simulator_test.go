package xcode

import (
	"errors"
	"os"
	"reflect"
	"slices"
	"testing"
)

func readFixture(name string) func() ([]byte, error) {
	return func() ([]byte, error) { return os.ReadFile("testdata/" + name) }
}

func TestSimulatorParsing(t *testing.T) {
	simulators, err := parseSimulatorsWith(readFixture("simctl-list.json"), readFixture("devicectl-list.json"))
	if err != nil {
		t.Fatal(err)
	}
	// My Mac first, then paired devices in reach in devicectl's order, then
	// simulators: booted, then iOS before tvOS before watchOS.
	expectedSimulators := []Simulator{
		Mac,
		{Name: "Test iPad", Runtime: "iOS 26.3.1", UDID: "00008120-000CCCCCCCCC0032", Device: true},
		{Name: "Test iPhone", Runtime: "iOS 26.7.1", UDID: "00008140-000DDDDDDDDD801C", Device: true},
		{Name: "Apple TV 4K", Runtime: "tvOS 26.2", State: "Booted", UDID: "D640FDA5-7FDA-47F1-928F-E630815BB484"},
		{Name: "iPhone 17 Pro", Runtime: "iOS 26.2", State: "Shutdown", UDID: "6AFBF4E8-BDB3-408A-9458-21A2A91D7899"},
		{Name: "iPhone+Watch", Runtime: "iOS 26.2", State: "Shutdown", UDID: "5FE0CE7B-707A-4371-8174-6807E1D0F51B"},
		{Name: "PairedWatch", Runtime: "watchOS 26.2", State: "Shutdown", UDID: "27E8581C-4CD7-45C3-A394-491F2E375A9F"},
	}
	if !reflect.DeepEqual(expectedSimulators, simulators) {
		t.Errorf("got: %v, want: %v", simulators, expectedSimulators)
	}
}

func TestSimulatorsWithoutDevicectl(t *testing.T) {
	noDevicectl := func() ([]byte, error) { return nil, errors.New("xcrun: error: unable to find utility \"devicectl\"") }
	simulators, err := parseSimulatorsWith(readFixture("simctl-list.json"), noDevicectl)
	if err != nil || len(simulators) != 5 || slices.ContainsFunc(simulators, func(s Simulator) bool { return s.Device }) {
		t.Errorf("got %v, %v; want My Mac and the four simulators", simulators, err)
	}
}

func TestRuntimeName(t *testing.T) {
	tests := []struct {
		identifier string
		want       string
	}{
		{"com.apple.CoreSimulator.SimRuntime.iOS-18-2", "iOS 18.2"},
		{"com.apple.CoreSimulator.SimRuntime.iOS-26-0", "iOS 26.0"},
		{"com.apple.CoreSimulator.SimRuntime.watchOS-26-2", "watchOS 26.2"},
		{"com.apple.CoreSimulator.SimRuntime.xrOS-2-0", "xrOS 2.0"},
		{"something.else", "something.else"},
	}

	for _, tt := range tests {
		t.Run(tt.identifier, func(t *testing.T) {
			if got := runtimeName(tt.identifier); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSortSimulators(t *testing.T) {
	tests := []struct {
		name  string
		input []Simulator
		want  []string // names in order
	}{
		{
			"newest runtime first, simctl order kept within a runtime",
			[]Simulator{
				{Name: "iPhone 16", Runtime: "iOS 18.2", State: "Shutdown"},
				{Name: "iPhone 17 Pro", Runtime: "iOS 26.5", State: "Shutdown"},
				{Name: "iPhone Air", Runtime: "iOS 26.5", State: "Shutdown"},
				{Name: "iPhone 17", Runtime: "iOS 26.10", State: "Shutdown"},
			},
			[]string{"iPhone 17", "iPhone 17 Pro", "iPhone Air", "iPhone 16"},
		},
		{
			"booted first, even on an older runtime",
			[]Simulator{
				{Name: "iPhone 17 Pro", Runtime: "iOS 26.5", State: "Shutdown"},
				{Name: "iPhone 16", Runtime: "iOS 18.2", State: "Booted"},
			},
			[]string{"iPhone 16", "iPhone 17 Pro"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sortSimulators(tt.input)
			var got []string
			for _, s := range tt.input {
				got = append(got, s.Name)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDefaultSimulator(t *testing.T) {
	iPad := Simulator{Name: "iPad Air", Runtime: "iOS 26.5", State: "Shutdown"}
	iPhone := Simulator{Name: "iPhone 17 Pro", Runtime: "iOS 26.5", State: "Shutdown"}
	bootedIPad := Simulator{Name: "iPad mini", Runtime: "iOS 26.4", State: "Booted"}
	bootedTV := Simulator{Name: "Apple TV", Runtime: "tvOS 26.2", State: "Booted"}

	tests := []struct {
		name       string
		simulators []Simulator
		want       Simulator
		wantOK     bool
	}{
		{"booted wins", []Simulator{bootedIPad, iPad, iPhone}, bootedIPad, true},
		{"a booted Apple TV does not beat an iPhone", []Simulator{bootedTV, iPad, iPhone}, iPhone, true},
		{"first iPhone", []Simulator{iPad, iPhone}, iPhone, true},
		{"anything when no iPhone", []Simulator{iPad}, iPad, true},
		{"a device is never picked, even one named iPhone", []Simulator{{Name: "iPhone", Runtime: "iOS 26.7.1", Device: true}, iPad, iPhone}, iPhone, true},
		{"none", nil, Simulator{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := DefaultSimulator(tt.simulators)
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("got %v %v, want %v %v", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestDestinationPerPlatform(t *testing.T) {
	tests := []struct {
		sim          Simulator
		wantDest     string
		wantPlatform string
	}{
		{Simulator{Runtime: "iOS 26.5", UDID: "A"}, "platform=iOS Simulator,id=A", "iphonesimulator"},
		{Simulator{Runtime: "tvOS 26.2", UDID: "B"}, "platform=tvOS Simulator,id=B", "appletvsimulator"},
		{Simulator{Runtime: "watchOS 26.2", UDID: "C"}, "platform=watchOS Simulator,id=C", "watchsimulator"},
		{Simulator{Runtime: "xrOS 26.0", UDID: "D"}, "platform=visionOS Simulator,id=D", "xrsimulator"},
		{Simulator{Runtime: "iOS 26.7.1", UDID: "E", Device: true}, "platform=iOS,id=E", "iphoneos"},
		{Simulator{Runtime: "tvOS 26.4", UDID: "F", Device: true}, "platform=tvOS,id=F", "appletvos"},
		{Simulator{Runtime: "watchOS 26.1", UDID: "G", Device: true}, "platform=watchOS,id=G", "watchos"},
		{Simulator{Runtime: "visionOS 26.0", UDID: "H", Device: true}, "platform=visionOS,id=H", "xros"},
		{Mac, "platform=macOS", "macosx"},
	}
	for _, tt := range tests {
		if got := tt.sim.Destination(); got != tt.wantDest {
			t.Errorf("%s: destination %q, want %q", tt.sim.Runtime, got, tt.wantDest)
		}
		if got := tt.sim.PlatformName(); got != tt.wantPlatform {
			t.Errorf("%s: platform name %q, want %q", tt.sim.Runtime, got, tt.wantPlatform)
		}
	}
}
