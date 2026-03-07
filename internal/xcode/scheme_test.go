package xcode

import (
	"os"
	"reflect"
	"testing"
)

type fakeSchemeGetter struct{}

func (fs *fakeSchemeGetter) Get(xcodePath string) ([]byte, error) {
	return os.ReadFile("testdata/xcodebuild-list.json")
}

func TestSchemeParsing(t *testing.T) {
	schemes, err := parseSchemesWith("xcodePath", &fakeSchemeGetter{})
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
