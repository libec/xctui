package xcode

import (
	"os"
	"reflect"
	"testing"
)

func TestSchemeParsing(t *testing.T) {
	schemes, err := parseSchemesWith("xcodePath", func(_ string) ([]byte, error) {
		return os.ReadFile("testdata/xcodebuild-list.json")
	})
	if err != nil {
		t.Fatal("parseToSchemes failed to load schemes.")
	}
	expectedSchemes := []Scheme{
		{Name: "TheWholePicture-Debug"},
		{Name: "TheWholePicture-Production"},
		{Name: "Watch App"},
	}
	if !reflect.DeepEqual(schemes, expectedSchemes) {
		t.Errorf("got %v, want %v", schemes, expectedSchemes)
	}
}
