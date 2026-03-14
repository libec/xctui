package main

import (
	"fmt"
	"log"
	"os"

	"github.com/libec/xctui/internal/xcode"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		return
	}

	switch {
	case os.Args[1] == "info":
		printProjectInfo()
	case os.Args[1] == "build":
		if len(os.Args) < 4 {
			printUsage()
			return
		}

		build(os.Args[2], os.Args[3])
	default:
		printUsage()
	}
}

func printUsage() {
	fmt.Println("Usage:")
	fmt.Println("- xctui info")
	fmt.Println("- xctui build {scheme} {simulator}")
}

func build(scheme, simulator string) {
	cwd, err := os.Getwd()
	if err != nil {
		log.Fatal("failed to find cwd.")
	}
	projPath, err := xcode.FindProject(cwd)
	if err != nil {
		log.Fatal("there's no .xcodeproj in the current directory.")
	}
	xcode.Build(projPath, xcode.Scheme{Name: scheme}, xcode.Simulator{Name: simulator})
}

func printProjectInfo() {
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
	simulators, err := xcode.ParseSimulators()
	if err != nil {
		log.Fatal("there are no simulators to run")
	}
	fmt.Printf("simulators found: %s", simulators)
}
