package xcode

import (
	"encoding/json"
	"fmt"
	"os/exec"
)

type xcodebuildlist struct {
	Project struct {
		Schemes []Scheme `json:"schemes"`
	} `json:"project"`
}

type Scheme struct {
	Name string
}

func (s *Scheme) UnmarshalJSON(data []byte) error {
	var name string
	if err := json.Unmarshal(data, &name); err != nil {
		return err
	}

	s.Name = name
	return nil
}

func (s *Scheme) String() string {
	return fmt.Sprintf("scheme: %s", s.Name)
}

func ParseSchemes(xcodePath string) ([]Scheme, error) {
	return parseSchemesWith(xcodePath, func(path string) ([]byte, error) {
		return exec.Command("xcodebuild", "-list", "-json", "-project", path).Output()
	})
}

func parseSchemesWith(xcodePath string, execCommand func(string) ([]byte, error)) ([]Scheme, error) {
	output, err := execCommand(xcodePath)
	if err != nil {
		return nil, err
	}
	return parseToSchemes(output)
}

func parseToSchemes(data []byte) ([]Scheme, error) {
	var parsedRaw xcodebuildlist
	if err := json.Unmarshal(data, &parsedRaw); err != nil {
		return nil, err
	}
	return parsedRaw.Project.Schemes, nil
}
