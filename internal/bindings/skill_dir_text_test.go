package bindings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// aae-orc-k5vro: the bound-skill rewrite covered an extension list, so a
// {project-root}/_bmad/ reference that resolves in the store stayed
// literal in an .xml, .py, .toml or .sh file under a skill. Eligibility
// is now the file's content: valid UTF-8 and no NUL byte.

type textFixture struct {
	pack, skills string
	src          map[string][]byte // skill-relative path to source bytes
}

// newTextFixture builds a pack with one skill whose files exercise the
// eligibility rule, and syncs it into a scratch config directory.
func newTextFixture(t *testing.T) textFixture {
	t.Helper()
	pack := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")

	writeFile(t, filepath.Join(pack, "core", "workflow.md"), "workflow\n")
	base := filepath.Join(pack, ".claude", "skills", "demo-skill")
	ref := "{project-root}/_bmad/core/workflow.md"
	state := "{project-root}/_bmad/config.yaml" // absent from the store: project state
	files := map[string]string{
		"SKILL.md":         "# demo\nLoad " + ref + "\n",
		"workflow.xml":     "<w><require file=\"" + ref + "\"/><note>" + state + "</note></w>\n",
		"scripts/run.py":   "# reads " + ref + "\nprint(\"ok\")\n",
		"customize.toml":   "# " + ref + "\nkey = \"v\"\n",
		"scripts/run.sh":   "#!/bin/sh\n# " + ref + "\n",
		"scripts/utf8.py":  "# héllo ✓ no reference here\nprint(\"✓\")\n",
		"blob.dat":         "\x00\x01" + ref + "\x00",
		"latin1.txt.bak":   "\xff\xfe " + ref + "\n",
		"nested/notes.txt": "see " + ref + "\n",
	}
	src := map[string][]byte{}
	for rel, content := range files {
		p := filepath.Join(base, filepath.FromSlash(rel))
		writeFile(t, p, content)
		src[rel] = []byte(content)
	}
	if err := os.Chmod(filepath.Join(base, "scripts", "run.sh"), 0o755); err != nil {
		t.Fatal(err)
	}

	b := NewSkillDirBinding("bmad", "6.12.1", pack)
	if _, err := b.Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	return textFixture{pack: pack, skills: filepath.Join(home, ".claude", "skills", "demo-skill"), src: src}
}

func (f textFixture) read(t *testing.T, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(f.skills, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestSkillDirBinding_Sync_RewritesEveryTextFile(t *testing.T) {
	f := newTextFixture(t)
	want := f.pack + "/core/workflow.md"

	for _, rel := range []string{"workflow.xml", "scripts/run.py", "customize.toml", "scripts/run.sh"} {
		got := f.read(t, rel)
		if !strings.Contains(got, want) {
			t.Errorf("%s: resolvable reference not rewritten:\n%s", rel, got)
		}
		if strings.Contains(got, "{project-root}/_bmad/core/workflow.md") {
			t.Errorf("%s: literal reference left beside the rewrite:\n%s", rel, got)
		}
	}
	if got := f.read(t, "workflow.xml"); !strings.Contains(got, "{project-root}/_bmad/config.yaml") {
		t.Errorf("workflow.xml: project-state reference was redirected into the store:\n%s", got)
	}
}

func TestSkillDirBinding_Sync_LeavesNonTextAndTokenlessFilesByteIdentical(t *testing.T) {
	f := newTextFixture(t)

	for _, rel := range []string{"scripts/utf8.py", "blob.dat", "latin1.txt.bak"} {
		if got := f.read(t, rel); got != string(f.src[rel]) {
			t.Errorf("%s changed:\n got %q\nwant %q", rel, got, f.src[rel])
		}
	}
}

// The parity condition: undoing the declared rewrite (store path back to
// the token) reproduces the source bytes of every file but SKILL.md,
// which also carries the fallback footer.
func TestSkillDirBinding_Sync_OnlyTheDeclaredRewriteChangesBytes(t *testing.T) {
	f := newTextFixture(t)

	for rel, src := range f.src {
		if rel == "SKILL.md" {
			continue
		}
		undone := strings.ReplaceAll(f.read(t, rel), f.pack+"/", "{project-root}/_bmad/")
		if undone != string(src) {
			t.Errorf("%s: bytes differ from source beyond the declared rewrite:\n got %q\nwant %q", rel, undone, src)
		}
	}
}

func TestSkillDirBinding_Sync_KeepsModeOfRewrittenFiles(t *testing.T) {
	f := newTextFixture(t)

	info, err := os.Stat(filepath.Join(f.skills, "scripts", "run.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("run.sh lost its exec bit after rewrite: %v", info.Mode())
	}
}

// Symlinks keep the extension rule they had: content eligibility does
// not reach through a link.
func TestSkillDirBinding_Sync_SymlinkKeepsExtensionRule(t *testing.T) {
	pack := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	writeFile(t, filepath.Join(pack, "core", "workflow.md"), "workflow\n")
	base := filepath.Join(pack, ".claude", "skills", "demo-skill")
	ref := "{project-root}/_bmad/core/workflow.md"
	writeFile(t, filepath.Join(base, "SKILL.md"), "# demo\n")
	writeFile(t, filepath.Join(base, "real.py"), "# "+ref+"\n")
	if err := os.Symlink("real.py", filepath.Join(base, "link.py")); err != nil {
		t.Fatal(err)
	}

	if _, err := NewSkillDirBinding("bmad", "6.12.1", pack).Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	dst := filepath.Join(os.Getenv("HOME"), ".claude", "skills", "demo-skill")
	link, err := os.ReadFile(filepath.Join(dst, "link.py"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(link), ref) {
		t.Errorf("a symlinked .py was rewritten by content eligibility:\n%s", link)
	}
}
