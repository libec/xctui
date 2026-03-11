package xcode

import (
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
)

type fakeSimulatorGetter struct{}

func (fs *fakeSimulatorGetter) Get() ([]byte, error) {
	return os.ReadFile("testdata/simctl-list.json")
}

func TestSimulatorParsing(t *testing.T) {
	simulators, err := parseSimulatorsWith(&fakeSimulatorGetter{})
	if err != nil {
		t.Fatal(err)
	}
	expectedSimulators := []Simulator{
		{Name: "PairedWatch", Runtime: "com.apple.CoreSimulator.SimRuntime.watchOS-26-2", State: "Shutdown", UDID: "27E8581C-4CD7-45C3-A394-491F2E375A9F"},
		{Name: "Apple TV 4K", Runtime: "com.apple.CoreSimulator.SimRuntime.tvOS-26-2", State: "Booted", UDID: "D640FDA5-7FDA-47F1-928F-E630815BB484"},
		{Name: "iPhone 17 Pro", Runtime: "com.apple.CoreSimulator.SimRuntime.iOS-26-2", State: "Shutdown", UDID: "6AFBF4E8-BDB3-408A-9458-21A2A91D7899"},
		{Name: "iPhone+Watch", Runtime: "com.apple.CoreSimulator.SimRuntime.iOS-26-2", State: "Shutdown", UDID: "5FE0CE7B-707A-4371-8174-6807E1D0F51B"},
	}
	sortSimulators(expectedSimulators)
	sortSimulators(simulators)
	if !reflect.DeepEqual(expectedSimulators, simulators) {
		t.Errorf("got: %s, want: %s", simulators, expectedSimulators)
	}
}

func sortSimulators(s []Simulator) {
	slices.SortFunc(s, func(a, b Simulator) int {
		return strings.Compare(a.UDID, b.UDID)
	})
}
