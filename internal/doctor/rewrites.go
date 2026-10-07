package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/ArcavenAE/sideshow/internal/pack"
)

// maxListedRewrites caps how many paths the finding names.
const maxListedRewrites = 5

// rewriteRecord is the `rewrites:` block a pack build writes into the
// in-store pack.yaml: the files it changed after the installer hashed
// them, with the installer's hash for each (aae-orc-hu9z2).
type rewriteRecord struct {
	Rewrites *struct {
		RuleVersion int `yaml:"neutralizer_rule_version"`
		Refreshed   []struct {
			Path string `yaml:"path"`
		} `yaml:"census_refreshed"`
	} `yaml:"rewrites"`
}

// checkDeclaredRewrites reports the pack's declared rewrite record as
// information. It is read-only and never feeds the census: a rewritten
// file is verified against the census like any other, so an edit to it
// still fails store-content-census. A pack with no record says nothing,
// since packs that do not rewrite after the installer have none.
func checkDeclaredRewrites(ctx *Context) []Finding {
	var out []Finding
	for _, p := range ctx.Packs {
		for _, version := range installedVersionDirs(p.Name) {
			data, err := os.ReadFile(filepath.Join(pack.PacksDir(), p.Name, version, "pack.yaml"))
			if err != nil {
				continue // store-shape reports a missing pack.yaml
			}
			var rec rewriteRecord
			if err := yaml.Unmarshal(data, &rec); err != nil {
				if !strings.Contains(string(data), "rewrites:") {
					continue
				}
				out = append(out, Finding{
					Layer: 1, ID: "store-declared-rewrites", Pack: p.Name, Subject: version, Status: Warn, Class: Advisory,
					Detail: fmt.Sprintf("the pack.yaml rewrites record cannot be read: %v", err),
					Next:   fmt.Sprintf("reinstall %s %s from its release artifact with --force and re-run doctor", p.Name, version),
				})
				continue
			}
			if rec.Rewrites == nil {
				continue
			}
			var paths []string
			for _, r := range rec.Rewrites.Refreshed {
				paths = append(paths, r.Path)
			}
			detail := fmt.Sprintf("%d declared rewrites (neutralizer rule version %d)", len(paths), rec.Rewrites.RuleVersion)
			if len(paths) > 0 {
				listed := paths
				if len(listed) > maxListedRewrites {
					listed = listed[:maxListedRewrites]
				}
				detail += ": " + strings.Join(listed, ", ")
				if len(paths) > len(listed) {
					detail += fmt.Sprintf(" and %d more", len(paths)-len(listed))
				}
			}
			out = append(out, Finding{
				Layer: 1, ID: "store-declared-rewrites", Pack: p.Name, Subject: version, Status: OK, Class: Advisory,
				Detail: detail,
			})
		}
	}
	return out
}
