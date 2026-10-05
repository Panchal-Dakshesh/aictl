package memory

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiff(t *testing.T) {
	d := Diff("a\nb\nc\n", "a\nc\nd\n")
	got := ""
	for _, l := range d {
		got += string(l.Op) + l.Text + "|"
	}
	if got != " a|-b| c|+d|" {
		t.Errorf("got %s", got)
	}
}

func TestSyncBacksUp(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AICTL_HOME", dir)
	os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("new rules"), 0o644)
	os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("old rules"), 0o644)
	files := Find(dir)
	var src, dst File
	for _, f := range files {
		switch filepath.Base(f.Path) {
		case "CLAUDE.md":
			if f.Scope == "project" {
				src = f
			}
		case "AGENTS.md":
			if f.Scope == "project" {
				dst = f
			}
		}
	}
	bak, err := Sync(src, dst)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(dst.Path)
	old, _ := os.ReadFile(bak)
	if string(b) != "new rules" || string(old) != "old rules" {
		t.Errorf("sync wrong: %q backup %q", b, old)
	}
}
