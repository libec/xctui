package main

import (
	"fmt"
	"log"
	"os"

	"github.com/libec/xctui/internal/xcode"
)

func main() {
	cwd, err := os.Getwd()
	if err != nil {
		log.Fatal("failed to find cwd.")
	}
	projPath, err := xcode.FindProject(cwd)
	if err != nil {
		log.Fatal("there's no .xcodeproj in the current directory.")
	}
	fmt.Println(projPath)
	schemes, err := xcode.ParseSchemes(projPath)
	if err != nil {
		log.Fatal("failed to parse schemes from xcodeproj.")
	}
	fmt.Printf("schemes found in %s\n", projPath)
	for _, s := range schemes {
		fmt.Println(s.String())
	}
}
