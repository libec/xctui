package xcode

import (
	"encoding/json"
	"fmt"
	"os/exec"
)

type xcodebuildlist struct {
	Project struct {
		Schemes []string `json:"schemes"`
	} `json:"project"`
}

type Scheme struct {
	// consider https://pkg.go.dev/encoding/json#Unmarshaler
	// and passing Scheme to xcodebuildlist for easier parsing
	Name string
}

func (s *Scheme) String() string {
	return fmt.Sprintf("scheme: %s", s.Name)
}

func ParseSchemes(xcodePath string) ([]Scheme, error) {
	cmd := exec.Command("xcodebuild", "-list", "-json", "-project", xcodePath)
	output, err := cmd.Output()
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
	var schemes []Scheme
	for _, s := range parsedRaw.Project.Schemes {
		schemes = append(schemes, Scheme{Name: s})
	}
	return schemes, nil
}
