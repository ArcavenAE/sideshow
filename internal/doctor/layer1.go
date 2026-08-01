package doctor

import (
	"bufio"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ArcavenAE/sideshow/internal/distribute"
	"github.com/ArcavenAE/sideshow/internal/pack"
)

// layer1Checks is the sideshow-native integrity set: the store, the
// sync manifest, and the receipts sideshow wrote, verified against
// disk.
func layer1Checks() []Check {
	return []Check{
		{ID: "registry-parse", Layer: 1, Run: checkRegistryParse},
		{ID: "store-coherence", Layer: 1, Run: checkStoreCoherence},
		{ID: "store-shape", Layer: 1, Run: checkStoreShape},
		{ID: "store-freeze", Layer: 1, Run: checkStoreFreeze},
		{ID: "store-content-census", Layer: 1, Run: checkContentCensus},
		{ID: "sync-manifest", Layer: 1, Run: checkSyncManifest},
		{ID: "receipt-markers", Layer: 1, Run: checkReceiptMarkers},
		{ID: "receipt-symlinks", Layer: 1, Run: checkReceiptSymlinks},
	}
}

func registryPath() string {
	return filepath.Join(filepath.Dir(pack.PacksDir()), "registry.yaml")
}

func checkRegistryParse(ctx *Context) []Finding {
	if ctx.RegistryErr != nil {
		return []Finding{{
			Layer: 1, ID: "registry-parse", Status: Fail, Class: Structural,
			Detail: fmt.Sprintf("registry does not load: %v", ctx.RegistryErr),
			Next:   fmt.Sprintf("inspect %s; a store we cannot read is not a health signal", registryPath()),
		}}
	}
	return []Finding{{
		Layer: 1, ID: "registry-parse", Status: OK, Class: Structural,
		Detail: fmt.Sprintf("registry loads (%d installed packs)", len(ctx.Registry.Packs)),
	}}
}

// checkStoreCoherence verifies that the three records of "which
// version is active" agree: the registry row, the current symlink,
// and the version directory on disk.
func checkStoreCoherence(ctx *Context) []Finding {
	var out []Finding
	for _, p := range ctx.Packs {
		link := filepath.Join(pack.PacksDir(), p.Name, "current")
		target, err := os.Readlink(link)
		if err != nil {
			out = append(out, Finding{
				Layer: 1, ID: "store-coherence", Pack: p.Name, Status: Fail, Class: Structural,
				Detail: fmt.Sprintf("registry records %s installed but the current symlink does not read: %v", p.Version, err),
				Next:   fmt.Sprintf("sideshow use %s %s", p.Name, p.Version),
			})
			continue
		}
		if _, err := os.Stat(link); err != nil {
			out = append(out, Finding{
				Layer: 1, ID: "store-coherence", Pack: p.Name, Status: Fail, Class: Structural,
				Detail: fmt.Sprintf("current symlink dangles: points at %q which does not exist", target),
				Next:   fmt.Sprintf("sideshow install %s --from <artifact>, or sideshow use %s <installed-version>", p.Name, p.Name),
			})
			continue
		}
		if target != p.Version {
			out = append(out, Finding{
				Layer: 1, ID: "store-coherence", Pack: p.Name, Status: Fail, Class: Structural,
				Detail: fmt.Sprintf("registry records version %s but current points at %s; two records of the active version disagree", p.Version, target),
				Next:   fmt.Sprintf("sideshow use %s <version> to realign both records", p.Name),
			})
			continue
		}
		out = append(out, Finding{
			Layer: 1, ID: "store-coherence", Pack: p.Name, Status: OK, Class: Structural,
			Detail: fmt.Sprintf("registry, current symlink, and version dir agree on %s", p.Version),
		})
	}
	return out
}

// checkStoreShape warns when an installed version looks like an
// upstream source tree rather than installer output (aae-orc-xbkl:
// install used to accept source tarballs silently).
func checkStoreShape(ctx *Context) []Finding {
	var out []Finding
	for _, p := range ctx.Packs {
		for _, version := range installedVersionDirs(p.Name) {
			dir := filepath.Join(pack.PacksDir(), p.Name, version)
			if err := pack.ValidateShape(dir); err != nil {
				out = append(out, Finding{
					Layer: 1, ID: "store-shape", Pack: p.Name, Subject: version, Status: Warn, Class: Structural,
					Detail: fmt.Sprintf("installed tree does not look like installer output: %v", err),
					Next:   fmt.Sprintf("reinstall %s %s from a release artifact (sideshow install %s --from <path>)", p.Name, version, p.Name),
				})
			}
		}
	}
	if len(out) == 0 && len(ctx.Packs) > 0 {
		out = append(out, Finding{
			Layer: 1, ID: "store-shape", Status: OK, Class: Structural,
			Detail: "every installed version looks like installer output",
		})
	}
	return out
}

// checkStoreFreeze verifies the store-freeze invariant (installed
// versions are read-only). Advisory: sideshow cannot distinguish a
// pre-freeze install from an interrupted unlock-write-refreeze, so
// the wording names both benign causes and never says "tampered".
func checkStoreFreeze(ctx *Context) []Finding {
	var out []Finding
	for _, p := range ctx.Packs {
		for _, version := range installedVersionDirs(p.Name) {
			dir := filepath.Join(pack.PacksDir(), p.Name, version)
			writable := 0
			_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					return nil
				}
				info, err := d.Info()
				if err != nil || info.Mode()&os.ModeSymlink != 0 {
					return nil
				}
				if info.Mode().Perm()&0o222 != 0 {
					writable++
				}
				return nil
			})
			if writable > 0 {
				out = append(out, Finding{
					Layer: 1, ID: "store-freeze", Pack: p.Name, Subject: version, Status: Warn, Class: Advisory,
					Detail: fmt.Sprintf("%d writable entries under the version dir; freeze invariant broken; cause not recorded (installed before the store freeze, or an unlock-write-refreeze was interrupted)", writable),
					Next:   fmt.Sprintf("reinstall to refreeze: sideshow install %s --from <artifact>", p.Name),
				})
			}
		}
	}
	if len(out) == 0 && len(ctx.Packs) > 0 {
		out = append(out, Finding{
			Layer: 1, ID: "store-freeze", Status: OK, Class: Advisory,
			Detail: "every installed version is frozen read-only",
		})
	}
	return out
}

// checkContentCensus re-hashes the store tree against the per-file
// census the pack itself ships (_config/files-manifest.csv; bmad
// does, vsdd-factory does not). Absent census is unavailable, not
// clean: release-asset manifests are not retained on install today.
func checkContentCensus(ctx *Context) []Finding {
	var out []Finding
	for _, p := range ctx.Packs {
		for _, version := range installedVersionDirs(p.Name) {
			dir := filepath.Join(pack.PacksDir(), p.Name, version)
			census := filepath.Join(dir, "_config", "files-manifest.csv")
			f, err := os.Open(census)
			if err != nil {
				out = append(out, Finding{
					Layer: 1, ID: "store-content-census", Pack: p.Name, Subject: version, Status: Unavailable, Class: Structural,
					Detail: "the pack ships no _config/files-manifest.csv and release-asset manifests are not retained on install; content integrity has no input here",
					Next:   "aae-orc-wk92 (fetch/verify consumer retains release manifests)",
				})
				continue
			}
			verified, mismatched, unresolved, samples := censusCompare(f, dir)
			_ = f.Close()
			if mismatched > 0 {
				out = append(out, Finding{
					Layer: 1, ID: "store-content-census", Pack: p.Name, Subject: version, Status: Fail, Class: Structural,
					Detail: fmt.Sprintf("%d of %d census entries differ from the store tree (first: %s); %d entries name paths not present in this layout", mismatched, verified+mismatched, strings.Join(samples, ", "), unresolved),
					Next:   fmt.Sprintf("reinstall %s %s from its release artifact and re-run doctor", p.Name, version),
				})
			} else {
				out = append(out, Finding{
					Layer: 1, ID: "store-content-census", Pack: p.Name, Subject: version, Status: OK, Class: Structural,
					Detail: fmt.Sprintf("%d census entries verified (%d name paths not present in this layout)", verified, unresolved),
				})
			}
		}
	}
	return out
}

// censusCompare walks one files-manifest.csv (columns
// type,name,module,path,hash) against root. Paths the census names
// that are absent are counted, not failed: installer output layout
// legitimately differs from some census rows.
func censusCompare(r io.Reader, root string) (verified, mismatched, unresolved int, samples []string) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	first := true
	for {
		rec, err := cr.Read()
		if err != nil {
			break
		}
		if first {
			first = false
			continue // header
		}
		if len(rec) < 5 {
			continue
		}
		rel, want := rec[3], rec[4]
		got, err := sha256File(filepath.Join(root, rel))
		if err != nil {
			unresolved++
			continue
		}
		if got == want {
			verified++
		} else {
			mismatched++
			if len(samples) < 3 {
				samples = append(samples, rel)
			}
		}
	}
	return verified, mismatched, unresolved, samples
}

// checkSyncManifest verifies the commands-sync receipt: every entry
// path still exists, and every entry's version matches the pack's
// active store version. Presence and currency only: the manifest
// records no content hashes (sync-content stays unavailable until a
// receipt change adds them).
func checkSyncManifest(ctx *Context) []Finding {
	if ctx.ManifestErr != nil {
		return []Finding{{
			Layer: 1, ID: "sync-manifest", Status: Unavailable, Class: Structural,
			Detail: fmt.Sprintf("sync manifest does not load: %v", ctx.ManifestErr),
			Next:   "run 'sideshow commands sync' to regenerate it",
		}}
	}
	active := map[string]string{}
	for _, p := range ctx.Packs {
		active[p.Name] = p.Version
	}
	var out []Finding
	entries := 0
	for _, e := range ctx.Manifest.Entries {
		if ctx.PackFilter != "" && e.Pack != ctx.PackFilter {
			continue
		}
		entries++
		if _, err := os.Stat(e.Path); err != nil {
			out = append(out, Finding{
				Layer: 1, ID: "sync-manifest", Pack: e.Pack, Subject: e.Path, Status: Warn, Class: Structural,
				Detail: "the sync manifest records this binding but the file is gone",
				Next:   "sideshow commands sync",
			})
			continue
		}
		// Custom-skills sources record "custom": project content, not
		// store content, so version currency does not apply to them.
		if e.Version == "custom" {
			continue
		}
		if v, ok := active[e.Pack]; ok && v != e.Version {
			out = append(out, Finding{
				Layer: 1, ID: "sync-manifest", Pack: e.Pack, Subject: e.Path, Status: Warn, Class: Structural,
				Detail: fmt.Sprintf("binding synced from %s while the active store version is %s", e.Version, v),
				Next:   "sideshow commands sync",
			})
		}
	}
	if len(out) == 0 {
		out = append(out, Finding{
			Layer: 1, ID: "sync-manifest", Status: OK, Class: Structural,
			Detail: fmt.Sprintf("%d synced bindings exist and match their pack's active version (presence and currency only; the manifest records no content hashes)", entries),
		})
	}
	return out
}

// checkReceiptMarkers verifies the managed-content markers the
// receipts promise: claude_md sections keep paired begin/end markers
// (a lone begin silently duplicates the section on the next
// distribute run), and rules files keep their managed-by first line.
func checkReceiptMarkers(ctx *Context) []Finding {
	var out []Finding
	forEachReceiptRepo(ctx, func(repoDir string, pd pack.PackDistribution) {
		var sections []string
		for _, a := range pd.Artifacts {
			switch a.Type {
			case "claude_md":
				if a.SectionID != "" {
					sections = append(sections, a.SectionID)
				}
			case "rules":
				if a.Path == "" {
					continue
				}
				path := filepath.Join(repoDir, a.Path)
				line, err := firstLine(path)
				if err != nil {
					continue // absence is layer 4's receipt-drift subject
				}
				if !strings.HasPrefix(line, distribute.MarkerPrefix) {
					out = append(out, Finding{
						Layer: 1, ID: "receipt-markers", Pack: pd.Pack, Subject: path, Status: Warn, Class: Structural,
						Detail: "receipted rules file lost its managed-by marker line; the next distribute run will treat it as unmanaged",
						Next:   "restore the marker or re-run 'sideshow init' for this project",
					})
				}
			}
		}
		if len(sections) == 0 {
			return
		}
		claudeMD := filepath.Join(repoDir, "CLAUDE.md")
		content, err := os.ReadFile(claudeMD)
		if err != nil {
			return // absence is layer 4's subject
		}
		text := string(content)
		for _, id := range sections {
			begin := strings.Count(text, distribute.SectionBegin(id))
			end := strings.Count(text, distribute.SectionEnd(id))
			switch {
			case begin == 1 && end == 1:
				// paired; ordering is implied by the writer refusing
				// inverted markers
			case begin == 0 && end == 0:
				out = append(out, Finding{
					Layer: 1, ID: "receipt-markers", Pack: pd.Pack, Subject: claudeMD, Status: Warn, Class: Structural,
					Detail: fmt.Sprintf("receipted section %q is absent from CLAUDE.md", id),
					Next:   "re-run 'sideshow init' for this project to restore it",
				})
			default:
				out = append(out, Finding{
					Layer: 1, ID: "receipt-markers", Pack: pd.Pack, Subject: claudeMD, Status: Fail, Class: Structural,
					Detail: fmt.Sprintf("section %q markers are unpaired (%d begin, %d end); the next distribute run would silently append a duplicate section", id, begin, end),
					Next:   "repair the marker pair by hand before the next 'sideshow init' run",
				})
			}
		}
	})
	if len(out) == 0 {
		out = append(out, Finding{
			Layer: 1, ID: "receipt-markers", Status: OK, Class: Structural,
			Detail: "every receipted marker is paired and present",
		})
	}
	return out
}

// checkReceiptSymlinks verifies receipted symlink artifacts still
// exist, are symlinks, and point at their recorded target (the
// customization bridge and runtime links are receipted this way).
func checkReceiptSymlinks(ctx *Context) []Finding {
	var out []Finding
	forEachReceiptRepo(ctx, func(repoDir string, pd pack.PackDistribution) {
		for _, a := range pd.Artifacts {
			if a.Type != "symlink" || a.Path == "" {
				continue
			}
			path := filepath.Join(repoDir, a.Path)
			fi, err := os.Lstat(path)
			if err != nil {
				out = append(out, Finding{
					Layer: 1, ID: "receipt-symlinks", Pack: pd.Pack, Subject: path, Status: Warn, Class: Structural,
					Detail: "receipted symlink is gone",
					Next:   fmt.Sprintf("sideshow project init %s (recreates it)", pd.Pack),
				})
				continue
			}
			if fi.Mode()&os.ModeSymlink == 0 {
				out = append(out, Finding{
					Layer: 1, ID: "receipt-symlinks", Pack: pd.Pack, Subject: path, Status: Fail, Class: Structural,
					Detail: "a real file or directory occupies a receipted symlink path",
					Next:   "move the occupant aside, then sideshow project init " + pd.Pack,
				})
				continue
			}
			got, _ := os.Readlink(path)
			if got != a.Target {
				out = append(out, Finding{
					Layer: 1, ID: "receipt-symlinks", Pack: pd.Pack, Subject: path, Status: Fail, Class: Structural,
					Detail: fmt.Sprintf("symlink points at %q, receipt records %q", got, a.Target),
					Next:   fmt.Sprintf("sideshow project init %s (recreates it)", pd.Pack),
				})
				continue
			}
			if _, err := os.Stat(path); err != nil {
				out = append(out, Finding{
					Layer: 1, ID: "receipt-symlinks", Pack: pd.Pack, Subject: path, Status: Fail, Class: Structural,
					Detail: fmt.Sprintf("symlink dangles: target %q does not resolve", a.Target),
					Next:   fmt.Sprintf("reinstall or 'sideshow use %s <version>' so the target exists, then re-run doctor", pd.Pack),
				})
			}
		}
	})
	if len(out) == 0 {
		out = append(out, Finding{
			Layer: 1, ID: "receipt-symlinks", Status: OK, Class: Structural,
			Detail: "every receipted symlink resolves to its recorded target",
		})
	}
	return out
}

// installedVersionDirs lists the version dirs of one pack, ignoring
// the current symlink. Errors surface through store-coherence.
func installedVersionDirs(name string) []string {
	entries, err := os.ReadDir(filepath.Join(pack.PacksDir(), name))
	if err != nil {
		return nil
	}
	var versions []string
	for _, e := range entries {
		if e.Name() == "current" || !e.IsDir() {
			continue
		}
		versions = append(versions, e.Name())
	}
	return versions
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }() // read-only handle
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func firstLine(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }() // read-only handle
	s := bufio.NewScanner(f)
	if s.Scan() {
		return s.Text(), nil
	}
	return "", s.Err()
}
