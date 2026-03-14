package xcode

import (
	"encoding/json"
	"os/exec"
)

type Simulator struct {
	Name    string
	Runtime string
	State   string
	UDID    string
}

type simulatorDevices struct {
	Devices map[string][]rawDevice `json:"devices"`
}

type rawDevice struct {
	Name  string `json:"name"`
	State string `json:"state"`
	UDID  string `json:"udid"`
}

func ParseSimulators() ([]Simulator, error) {
	return parseSimulatorsWith(func() ([]byte, error) {
		return exec.Command("xcrun", "simctl", "list", "devices", "available", "-j").Output()
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
		for _, device := range rawDevices {
			d := Simulator{Name: device.Name, Runtime: runtime, State: device.State, UDID: device.UDID}
			simulators = append(simulators, d)
		}
	}

	return simulators, nil
}
