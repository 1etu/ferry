package server_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/1etu/ferry/internal/server"
	"github.com/1etu/ferry/tests/kit"
)

const uploadsSubtree = "/api/uploads/"

func contractRoutes(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "api", "openapi.yaml"))
	kit.NoError(t, err, "read contract")
	var doc struct {
		Paths map[string]map[string]any `yaml:"paths"`
	}
	kit.NoError(t, yaml.Unmarshal(raw, &doc), "parse contract")
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
	got := slices.Sorted(slices.Values(server.Routes()))
	kit.Equal(t, got, contractRoutes(t))
}

func TestRoutesAreUnique(t *testing.T) {
	t.Parallel()
	routes := server.Routes()
	if unique := slices.Compact(slices.Sorted(slices.Values(routes))); len(unique) != len(routes) {
		t.Fatalf("duplicate patterns in %v", routes)
	}
}
