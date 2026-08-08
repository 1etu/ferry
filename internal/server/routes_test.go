package server

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"gopkg.in/yaml.v3"
)

const uploadsSubtree = "/api/uploads/"

func contractRoutes(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "api", "openapi.yaml"))
	if err != nil {
		t.Fatalf("read contract: %v", err)
	}
	var doc struct {
		Paths map[string]map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse contract: %v", err)
	}
	contractMethods := []string{"get", "put", "post", "delete", "options", "head", "patch", "trace"}
	var routes []string
	for path, item := range doc.Paths {
		for key := range item {
			if !slices.Contains(contractMethods, key) {
				continue
			}
			if strings.HasPrefix(path, uploadsSubtree) {
				routes = append(routes, uploadsSubtree)
				continue
			}
			routes = append(routes, strings.ToUpper(key)+" "+path)
		}
	}
	slices.Sort(routes)
	return slices.Compact(routes)
}

func TestRoutesMatchContractInBothDirections(t *testing.T) {
	t.Parallel()
	got := slices.Sorted(slices.Values(Routes()))
	if diff := cmp.Diff(contractRoutes(t), got); diff != "" {
		t.Fatalf("routes differ from api/openapi.yaml (-contract +server):\n%s", diff)
	}
}

func TestRoutesAreUnique(t *testing.T) {
	t.Parallel()
	routes := Routes()
	if unique := slices.Compact(slices.Sorted(slices.Values(routes))); len(unique) != len(routes) {
		t.Fatalf("duplicate patterns in %v", routes)
	}
}
