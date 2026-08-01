package doctor

import (
	"os"
	"path/filepath"

	"github.com/ArcavenAE/sideshow/internal/pack"
	"github.com/ArcavenAE/sideshow/internal/project"
)

// receiptSkip records one receipt subtree doctor could not walk, so
// coverage reporting can say what was skipped and why instead of
// letting "no findings" read as "no drift".
type receiptSkip struct {
	Subject string
	Reason  string
}

// forEachReceiptRepo resolves every reachable receipted repo and
// calls fn once per pack distribution, honoring the pack filter.
// Repo paths resolve the way distribution wrote them: the recorded
// repo path when present, else the installation's repos manifest
// (RecordResults does not populate RepoDistribution.Path today).
func forEachReceiptRepo(ctx *Context, fn func(repoDir string, pd pack.PackDistribution)) []receiptSkip {
	var skips []receiptSkip
	if ctx.Registry == nil {
		return skips
	}
	for _, proj := range ctx.Registry.Projects {
		for _, inst := range proj.Installations {
			if _, err := os.Stat(inst.Root); err != nil {
				skips = append(skips, receiptSkip{Subject: inst.Root, Reason: "installation root no longer exists"})
				continue
			}
			paths := repoPathIndex(inst)
			for _, rd := range inst.Repos {
				repoDir := rd.Path
				if repoDir == "" {
					repoDir = paths[rd.Name]
				}
				if repoDir == "" {
					skips = append(skips, receiptSkip{Subject: rd.Name, Reason: "repo path unresolvable (receipt records no path and the repos manifest does not list it)"})
					continue
				}
				if _, err := os.Stat(repoDir); err != nil {
					skips = append(skips, receiptSkip{Subject: repoDir, Reason: "receipted repo dir no longer exists"})
					continue
				}
				for _, pd := range rd.Packs {
					if ctx.PackFilter != "" && pd.Pack != ctx.PackFilter {
						continue
					}
					fn(repoDir, pd)
				}
			}
		}
	}
	return skips
}

// repoPathIndex maps receipt repo names to absolute paths via the
// installation's recorded repos manifest.
func repoPathIndex(inst pack.Installation) map[string]string {
	idx := map[string]string{}
	manifestPath := inst.Manifest
	if manifestPath == "" {
		manifestPath = project.FindReposManifest(inst.Root)
	}
	if manifestPath == "" {
		return idx
	}
	if !filepath.IsAbs(manifestPath) {
		manifestPath = filepath.Join(inst.Root, manifestPath)
	}
	m, err := project.LoadReposManifest(manifestPath)
	if err != nil {
		return idx
	}
	for _, sub := range project.ResolveSubrepos(inst.Root, m) {
		idx[sub.Name] = sub.AbsPath
	}
	return idx
}
