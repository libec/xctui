package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/libec/xctui/internal/tui"
	"github.com/libec/xctui/internal/xcode"
)

func main() {
	if len(os.Args) < 2 {
		runTUI("")
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
	case len(os.Args) == 2:
		runTUI(os.Args[1]) // xctui <scheme>
	default:
		printUsage()
	}
}

func runTUI(scheme string) {
	cwd, err := os.Getwd()
	if err != nil {
		log.Fatal("failed to find cwd.")
	}
	if err := tui.Run(cwd, scheme); err != nil {
		fmt.Fprintln(os.Stderr, "xctui:", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("Usage:")
	fmt.Println("- xctui            (opens the TUI for the project in the current directory)")
	fmt.Println("- xctui {scheme}   (same, with that scheme selected)")
	fmt.Println("- xctui info")
	fmt.Println("- xctui build {scheme} {simulator}")
}

func printProjectInfo() {
	cwd, err := os.Getwd()
	if err != nil {
		log.Fatal("failed to find cwd.")
	}
	xcodeProj, err := xcode.FindProject(cwd)
	if err != nil {
		log.Fatal("there's no .xcworkspace or .xcodeproj in the current directory.")
	}
	fmt.Println(xcodeProj)
	schemes, err := xcode.ParseSchemes(xcodeProj)
	if err != nil {
		log.Fatal("failed to parse schemes from xcodeproj.")
	}
	fmt.Printf("schemes found in %s\n", xcodeProj)
	for _, s := range schemes {
		fmt.Println(s.String())
	}
	simulators, err := xcode.ParseSimulators()
	if err != nil {
		log.Fatal("there are no simulators to run")
	}
	fmt.Printf("simulators found: %s", simulators)
}

func build(scheme, simulator string) {
	cwd, err := os.Getwd()
	if err != nil {
		log.Fatal("failed to find cwd.")
	}

	projPath := must(xcode.FindProject(cwd))
	targetSimulator := must(findSimulator(simulator))
	targetScheme := must(findScheme(scheme, projPath))
	stream := must(xcode.Build(context.Background(), xcode.ActionBuild, projPath, targetScheme, targetSimulator))

	for line := range stream.Lines {
		fmt.Println(line)
	}
	if err := stream.Wait(); err != nil {
		log.Fatal(err)
	}
}

func findScheme(scheme string, projPath xcode.Project) (xcode.Scheme, error) {
	schemes, err := xcode.ParseSchemes(projPath)
	if err != nil {
		return xcode.Scheme{}, err
	}
	var targetScheme xcode.Scheme
	for _, s := range schemes {
		if scheme == s.Name {
			targetScheme = s
			break
		}
	}
	if targetScheme == (xcode.Scheme{}) {
		return xcode.Scheme{}, fmt.Errorf("scheme %q not found", scheme)
	}
	return targetScheme, nil
}

func findSimulator(simulator string) (xcode.Simulator, error) {
	simulators, err := xcode.ParseSimulators()
	if err != nil {
		return xcode.Simulator{}, err
	}

	var targetSimulator xcode.Simulator
	for _, s := range simulators {
		if simulator == s.Name {
			targetSimulator = s
			break
		}
	}

	if targetSimulator == (xcode.Simulator{}) {
		return xcode.Simulator{}, fmt.Errorf("simulator %q not found", simulator)
	}
	return targetSimulator, nil
}

func must[T any](val T, err error) T {
	if err != nil {
		log.Fatal(err)
	}
	return val
}
