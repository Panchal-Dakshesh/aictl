package sources

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Panchal-Dakshesh/aictl/internal/session"
)

// setup builds a fake $HOME from testdata/home, renaming dot-<x> to .<x>.
func setup(t *testing.T) {
	t.Helper()
	root, _ := filepath.Abs("../../testdata")
	home := t.TempDir()
	src := filepath.Join(root, "home")
	err := filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		parts := strings.Split(rel, string(filepath.Separator))
		for i, part := range parts {
			if strings.HasPrefix(part, "dot-") {
				parts[i] = "." + part[4:]
			}
		}
		dst := filepath.Join(home, filepath.Join(parts...))
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("AICTL_HOME", home)
	t.Setenv("AICTL_CACHE", t.TempDir())
	t.Setenv("PATH", filepath.Join(root, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func one(t *testing.T, src session.Source) *session.Session {
	t.Helper()
	files, err := src.Files()
	if err != nil {
		t.Fatal(err)
	}
	var got []*session.Session
	for _, f := range files {
		ss, err := src.Parse(f)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		got = append(got, ss...)
	}
	session.Sort(got)
	if len(got) == 0 {
		t.Fatalf("%s: no sessions", src.Name())
	}
	return got[0]
}

func TestClaude(t *testing.T) {
	setup(t)
	c := &Claude{}
	files, _ := c.Files()
	if len(files) != 1 {
		t.Fatalf("agent-* side chains should be skipped, got %v", files)
	}
	s := one(t, c)
	if s.Title != "Fix flaky checkout test" {
		t.Errorf("summary should win as title, got %q", s.Title)
	}
	if s.Messages != 4 || s.Project != "/Users/demo/Work/shop" || s.Branch != "main" || s.Model != "claude-sonnet-4" {
		t.Errorf("unexpected %+v", s)
	}
	msgs, _ := c.Load(s)
	var tools int
	for _, m := range msgs {
		if m.Role == session.Tool {
			tools++
		}
		if strings.Contains(m.Text, "/clear") {
			t.Errorf("meta prompt leaked into transcript")
		}
	}
	if tools != 1 {
		t.Errorf("want 1 tool call, got %d", tools)
	}
	if _, argv, ok := c.Resume(s); !ok || argv[2] != s.ID {
		t.Errorf("resume argv %v", argv)
	}
}

func TestCodexBothFormats(t *testing.T) {
	setup(t)
	c := &Codex{}
	files, _ := c.Files()
	var got []*session.Session
	for _, f := range files {
		ss, _ := c.Parse(f)
		got = append(got, ss...)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 sessions, got %d", len(got))
	}
	session.Sort(got)
	cur := got[0]
	if cur.Title != "add a dead letter topic to the kafka consumer" {
		t.Errorf("environment_context must not become the title: %q", cur.Title)
	}
	if cur.Messages != 2 || cur.Model != "gpt-5-codex" || cur.Branch != "feat/cdc" || cur.Project != "/Users/demo/Work/pipeline" {
		t.Errorf("unexpected %+v", cur)
	}
	legacy := got[1]
	if legacy.ID != "33333333-cccc-4ddd-8eee-000000000003" || legacy.Messages != 2 {
		t.Errorf("legacy format: %+v", legacy)
	}
}

func TestGemini(t *testing.T) {
	setup(t)
	g := &Gemini{}
	s := one(t, g)
	if s.Project != "/Users/demo/Work/site" || s.Messages != 2 || s.Model != "gemini-2.5-pro" {
		t.Errorf("unexpected %+v", s)
	}
	msgs, _ := g.Load(s)
	if len(msgs) != 3 {
		t.Errorf("want user, assistant, tool; got %d", len(msgs))
	}
}

func TestAntigravity(t *testing.T) {
	setup(t)
	a := &Antigravity{}
	files, _ := a.Files()
	var got []*session.Session
	for _, f := range files {
		ss, err := a.Parse(f)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, ss...)
	}
	if len(got) != 1 {
		t.Fatalf("sub-agent conversations should be hidden, got %d", len(got))
	}
	s := got[0]
	if s.Title != "Voice assistant backend" || s.Messages != 2 || s.Project != "/Users/demo/Work/jarvis" {
		t.Errorf("unexpected %+v", s)
	}
	msgs, err := a.Load(s)
	if err != nil || len(msgs) != 3 || msgs[1].Name != "run_command" {
		t.Errorf("load: %v %+v", err, msgs)
	}
	if _, _, ok := a.Resume(s); ok {
		t.Error("antigravity shouldn't claim to resume")
	}
}

func TestCursor(t *testing.T) {
	setup(t)
	c := &Cursor{}
	s := one(t, c)
	if s.Title != "Landing page copy" || s.Messages != 2 || s.Project != "/Users/demo/Work/landing" {
		t.Errorf("unexpected %+v", s)
	}
	msgs, err := c.Load(s)
	if err != nil || len(msgs) != 2 || msgs[0].Role != session.User {
		t.Errorf("load: %v %+v", err, msgs)
	}
}

func TestIndexIsIncremental(t *testing.T) {
	setup(t)
	ix := session.OpenIndex()
	all, _, st := ix.Build(All())
	if len(all) != 6 {
		t.Fatalf("want 6 sessions across tools, got %d", len(all))
	}
	if st.Parsed == 0 || st.Cached != 0 {
		t.Errorf("first build should parse: %+v", st)
	}
	if err := ix.Save(); err != nil {
		t.Fatal(err)
	}
	_, _, st2 := session.OpenIndex().Build(All())
	if st2.Parsed != 0 || st2.Cached != st.Parsed {
		t.Errorf("second build should be all cache hits: %+v", st2)
	}
}

func TestProtowire(t *testing.T) {
	// field 19 { field 2: "hi" }, field 5 { field 1 { field 1: 300 } }
	b := []byte{0x9a, 0x01, 0x04, 0x12, 0x02, 'h', 'i', 0x2a, 0x05, 0x0a, 0x03, 0x08, 0xac, 0x02}
	fs := pbFields(b)
	if pbStr(fs, "19.2") != "hi" || pbInt(fs, "5.1.1") != 300 {
		t.Errorf("got %+v", fs)
	}
}
