package xcode

import (
	"os"
	"reflect"
	"testing"
)

func TestSchemeParsing(t *testing.T) {
	data, err := os.ReadFile("testdata/xcodebuild-list.json")
	if err != nil {
		t.Fatal(err)
	}
	schemes, err := parseToSchemes(data)
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
