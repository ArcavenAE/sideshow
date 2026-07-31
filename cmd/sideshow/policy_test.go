package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// conversionExempt are source prefixes (relative to the module root)
// allowed to touch claude plugin state: the conversion surface reads
// foreign installs and, with consent, flips foreign enables
// (docs/claude-plugin-conversion-reference.md). Adding a prefix here
// is a deliberate policy act, which is the point of this test.
var conversionExempt = []string{
	"internal/foreign/",
}

// forbidden are the marks of plugin-shaped delivery. Sideshow's own
// channel is repo bindings (docs/unshaping-spec.md): it never invokes
// claude plugin verbs and never touches enabledPlugins for delivery
// (aae-orc finding-094 contract items 2 and 4; aae-orc-d3nq.43).
var forbidden = []string{
	"plugin marketplace add",
	"plugin install",
	"enabledPlugins",
}

func TestPolicy_NoPluginDeliveryVerbsInShippedSource(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve module root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("module root not found at %s: %v", root, err)
	}

	walkErr := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		for _, exempt := range conversionExempt {
			if strings.HasPrefix(filepath.ToSlash(rel), exempt) {
				return nil
			}
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		for _, needle := range forbidden {
			if strings.Contains(string(data), needle) {
				t.Errorf("%s contains %q: plugin-shaped delivery is retired (finding-094); conversion code belongs under an exempt prefix", rel, needle)
			}
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk: %v", walkErr)
	}
}
