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

type SchemeGetter interface {
	Get(xcodePath string) ([]byte, error)
}

type XcodeSchemeGetter struct{}

func (xg *XcodeSchemeGetter) Get(xcodePath string) ([]byte, error) {
	cmd := exec.Command("xcodebuild", "-list", "-json", "-project", xcodePath)
	return cmd.Output()
}

func ParseSchemes(xcodePath string) ([]Scheme, error) {
	return parseSchemesWith(xcodePath, &XcodeSchemeGetter{})
}

func parseSchemesWith(xcodePath string, jsonReader SchemeGetter) ([]Scheme, error) {
	output, err := jsonReader.Get(xcodePath)
	if err != nil {
		return nil, err
	}
	parsed, err := parseToSchemes(output)
	if err != nil {
		return nil, err
	}
	return parsed, nil
}

func parseToSchemes(data []byte) ([]Scheme, error) {
	var parsedRaw xcodebuildlist
	if err := json.Unmarshal(data, &parsedRaw); err != nil {
		return nil, err
	}
	return parsedRaw.Project.Schemes, nil
}
