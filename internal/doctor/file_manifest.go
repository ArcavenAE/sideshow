package doctor

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/ArcavenAE/sideshow/internal/pack"
)

// fileManifestName is the per-file manifest the sideshow-packs builders
// ship at the pack root (sideshow-packs#40): one sha256,size,relpath
// line per regular file, no header, never listing itself. Unlike
// _config/files-manifest.csv, which is bmad's own and covers only part
// of the tree, it lists every file the pack shipped (aae-orc-xorml).
const fileManifestName = "file-manifest.csv"

type manifestEntry struct {
	sha  string
	size int64
}

// checkStoreFileManifest recomputes every installed version against the
// pack's own file-manifest.csv and reports content that differs, listed
// files that are missing, and files present but not listed.
func checkStoreFileManifest(ctx *Context) []Finding {
	var out []Finding
	for _, p := range ctx.Packs {
		for _, version := range installedVersionDirs(p.Name) {
			dir := filepath.Join(pack.PacksDir(), p.Name, version)
			out = append(out, verifyStoreVersion(p.Name, version, dir))
		}
	}
	return out
}

func verifyStoreVersion(name, version, dir string) Finding {
	f := Finding{Layer: 1, ID: "store-file-manifest", Pack: name, Subject: version, Class: Structural}
	want, err := readFileManifest(filepath.Join(dir, fileManifestName))
	if os.IsNotExist(err) {
		f.Status = Unavailable
		f.Detail = "this version ships no " + fileManifestName + " (packs built before sideshow-packs#40); its store content cannot be re-verified"
		f.Next = fmt.Sprintf("reinstall %s from a release built after sideshow-packs#40", name)
		return f
	}
	if err != nil {
		f.Status = Fail
		f.Detail = fmt.Sprintf("%s does not parse: %v", fileManifestName, err)
		f.Next = fmt.Sprintf("reinstall %s %s from its release artifact and re-run doctor", name, version)
		return f
	}

	var differs, missing, unlisted []string
	seen := make(map[string]bool, len(want))
	walkErr := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == fileManifestName {
			return nil
		}
		entry, listed := want[rel]
		if !listed {
			unlisted = append(unlisted, rel)
			return nil
		}
		seen[rel] = true
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Size() != entry.size {
			differs = append(differs, rel)
			return nil
		}
		sum, err := sha256File(path)
		if err != nil {
			return err
		}
		if sum != entry.sha {
			differs = append(differs, rel)
		}
		return nil
	})
	if walkErr != nil {
		f.Status = Fail
		f.Detail = fmt.Sprintf("cannot read the store tree: %v", walkErr)
		f.Next = fmt.Sprintf("inspect %s", dir)
		return f
	}
	for rel := range want {
		if !seen[rel] {
			missing = append(missing, rel)
		}
	}

	if len(differs)+len(missing)+len(unlisted) == 0 {
		f.Status = OK
		f.Detail = fmt.Sprintf("%d files match %s", len(want), fileManifestName)
		return f
	}
	var parts []string
	for _, g := range []struct {
		label string
		paths []string
	}{{"differ", differs}, {"missing", missing}, {"unlisted", unlisted}} {
		if len(g.paths) > 0 {
			sort.Strings(g.paths)
			parts = append(parts, fmt.Sprintf("%d %s (first: %s)", len(g.paths), g.label, strings.Join(firstN(g.paths, 3), ", ")))
		}
	}
	f.Status = Fail
	f.Detail = fmt.Sprintf("store differs from %s over %d listed files: %s", fileManifestName, len(want), strings.Join(parts, "; "))
	f.Next = fmt.Sprintf("reinstall %s %s from its release artifact and re-run doctor", name, version)
	return f
}

// readFileManifest parses sha256,size,relpath lines. Relpaths may
// contain commas, so only the first two fields are split off.
func readFileManifest(path string) (map[string]manifestEntry, error) {
	fh, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = fh.Close() }()
	out := map[string]manifestEntry{}
	sc := bufio.NewScanner(fh)
	line := 0
	for sc.Scan() {
		line++
		fields := strings.SplitN(sc.Text(), ",", 3)
		if len(fields) != 3 || fields[2] == "" {
			return nil, fmt.Errorf("line %d: want sha256,size,relpath", line)
		}
		size, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("line %d: size %q: %v", line, fields[1], err)
		}
		out[fields[2]] = manifestEntry{sha: fields[0], size: size}
	}
	return out, sc.Err()
}

func firstN(s []string, n int) []string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
