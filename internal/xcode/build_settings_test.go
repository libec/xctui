package xcode

import (
	"os"
	"testing"
)

func TestBuildSettings(t *testing.T) {
	got, err := parseBuildSettings(func() ([]byte, error) {
		return os.ReadFile("testdata/build_settings")
	})
	if err != nil {
		t.Fatal(err)
	}
	want := BuildSettings{AppPath: "/Users/libec/Developer/GitHub/thewholepicture/ios/build/Release-iphoneos/TheWholePicture.app", BundleID: "app.thewholepicture"}
	if want != got {
		t.Errorf("got %s, want %s", got, want)
	}
}
