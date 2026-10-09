package xcode

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// xcodebuild -list -json nests the schemes under "workspace" or "project",
// depending on which container it was given.
type xcodebuildlist struct {
	Workspace struct {
		Schemes []Scheme `json:"schemes"`
	} `json:"workspace"`
	Project struct {
		Schemes []Scheme `json:"schemes"`
	} `json:"project"`
}

type Scheme struct {
	Name    string
	Package bool // a product of a local Swift package, not a scheme file of the container
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
	if s.Package {
		return fmt.Sprintf("package: %s", s.Name)
	}
	return fmt.Sprintf("scheme: %s", s.Name)
}

// ParseSchemes lists the container's own schemes, then the products of the local
// Swift packages under the project root. Scheme names come from the .xcscheme
// files on disk: `xcodebuild -list` takes seconds on a project with packages and
// also invents a scheme per remote package product. It runs only when the
// container has no scheme files, because Xcode autocreates the app scheme then.
func ParseSchemes(project Project) ([]Scheme, error) {
	var schemes []Scheme
	for _, name := range schemeFiles(project.Name) {
		schemes = append(schemes, Scheme{Name: name})
	}
	if len(schemes) == 0 {
		var err error
		schemes, err = parseSchemesWith(func() ([]byte, error) {
			args := append([]string{"-list", "-json"}, project.args()...)
			return output(exec.Command("xcodebuild", args...))
		})
		if err != nil {
			return nil, err
		}
	}
	return withPackages(schemes, localPackageProducts(project.Root)), nil
}

// schemeFiles names the .xcscheme files under the container (shared and per-user);
// for a workspace also those of the .xcodeproj folders next to it, which it builds.
func schemeFiles(container string) []string {
	roots := []string{container}
	if filepath.Ext(container) == ".xcworkspace" {
		roots = append(roots, filepath.Join(filepath.Dir(container), "*.xcodeproj"))
	}
	var names []string
	for _, root := range roots {
		for _, pattern := range []string{"xcshareddata/xcschemes/*.xcscheme", "xcuserdata/*/xcschemes/*.xcscheme"} {
			matches, _ := filepath.Glob(filepath.Join(root, pattern))
			for _, m := range matches {
				names = append(names, strings.TrimSuffix(filepath.Base(m), ".xcscheme"))
			}
		}
	}
	slices.Sort(names)
	return slices.Compact(names)
}

var (
	productRE     = regexp.MustCompile(`\.(?:library|executable)\(\s*name:\s*"([^"]+)"`)
	packageNameRE = regexp.MustCompile(`Package\(\s*name:\s*"([^"]+)"`)
)

// localPackageProducts finds Package.swift up to two folders below root and names
// their products (or the package itself when it declares none). Checkouts of
// remote packages and build folders are skipped.
func localPackageProducts(root string) []string {
	if root == "" {
		return nil
	}
	var products []string
	for _, depth := range []string{"", "*", "*/*"} {
		matches, _ := filepath.Glob(filepath.Join(root, depth, "Package.swift"))
		for _, m := range matches {
			if strings.Contains(m, "/.build/") || strings.Contains(m, "/checkouts/") {
				continue
			}
			data, err := os.ReadFile(m)
			if err != nil {
				continue
			}
			found := productRE.FindAllSubmatch(data, -1)
			if len(found) == 0 {
				found = packageNameRE.FindAllSubmatch(data, 1)
			}
			for _, f := range found {
				products = append(products, string(f[1]))
			}
		}
	}
	return products
}

// withPackages marks schemes that are local package products and appends the
// products xcodebuild did not list, so packages come after the container's own
// schemes.
func withPackages(schemes []Scheme, products []string) []Scheme {
	listed := map[string]bool{}
	for i, s := range schemes {
		listed[s.Name] = true
		schemes[i].Package = slices.Contains(products, s.Name)
	}
	for _, p := range products {
		if !listed[p] {
			schemes = append(schemes, Scheme{Name: p, Package: true})
			listed[p] = true
		}
	}
	slices.SortStableFunc(schemes, func(a, b Scheme) int {
		switch {
		case a.Package == b.Package:
			return 0
		case a.Package:
			return 1
		}
		return -1
	})
	return schemes
}

func parseSchemesWith(execCommand func() ([]byte, error)) ([]Scheme, error) {
	output, err := execCommand()
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
	if parsedRaw.Workspace.Schemes != nil {
		return parsedRaw.Workspace.Schemes, nil
	}
	return parsedRaw.Project.Schemes, nil
}

// DefaultScheme picks the scheme named after the container (Puzzle.xcodeproj -> "Puzzle"),
// else the first one. A workspace lists every scheme of its pods and packages too.
func DefaultScheme(project Project, schemes []Scheme) (Scheme, bool) {
	name := strings.TrimSuffix(filepath.Base(project.Name), filepath.Ext(project.Name))
	for _, s := range schemes {
		if s.Name == name {
			return s, true
		}
	}
	if len(schemes) > 0 {
		return schemes[0], true
	}
	return Scheme{}, false
}
