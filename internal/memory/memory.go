// Package memory finds, diffs and syncs the instruction/memory files each AI
// tool reads: CLAUDE.md, AGENTS.md, GEMINI.md, Cursor rules and friends.
package memory

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Panchal-Dakshesh/aictl/internal/session"
)

type File struct {
	Tool   string
	Scope  string // "project" or "global"
	Path   string
	Exists bool
	Size   int64
}

func (f File) Label() string {
	p := session.ShortPath(f.Path)
	if !f.Exists {
		return p + "  (missing)"
	}
	return p
}

// Find lists memory files for a project directory plus the global ones.
// The four main per-project files are listed even when missing so they can be created.
func Find(project string) []File {
	var out []File
	add := func(tool, scope, p string, always bool) {
		fi, err := os.Stat(p)
		if err != nil && !always {
			return
		}
		f := File{Tool: tool, Scope: scope, Path: p, Exists: err == nil}
		if fi != nil {
			f.Size = fi.Size()
		}
		out = append(out, f)
	}
	if project != "" {
		add("claude", "project", filepath.Join(project, "CLAUDE.md"), true)
		add("claude", "project", filepath.Join(project, "CLAUDE.local.md"), false)
		add("claude", "project", filepath.Join(project, ".claude", "CLAUDE.md"), false)
		add("codex", "project", filepath.Join(project, "AGENTS.md"), true)
		add("gemini", "project", filepath.Join(project, "GEMINI.md"), true)
		add("cursor", "project", filepath.Join(project, ".cursorrules"), false)
		rules, _ := filepath.Glob(filepath.Join(project, ".cursor", "rules", "*.mdc"))
		sort.Strings(rules)
		for _, r := range rules {
			add("cursor", "project", r, false)
		}
		if len(rules) == 0 {
			add("cursor", "project", filepath.Join(project, ".cursor", "rules", "project.mdc"), true)
		}
		for _, dir := range []string{".agent", ".agents"} {
			ag, _ := filepath.Glob(filepath.Join(project, dir, "rules", "*.md"))
			for _, r := range ag {
				add("antigravity", "project", r, false)
			}
		}
	}
	add("claude", "global", session.HomePath(".claude", "CLAUDE.md"), true)
	add("codex", "global", session.HomePath(".codex", "AGENTS.md"), true)
	add("gemini", "global", session.HomePath(".gemini", "GEMINI.md"), true)
	return out
}

func Read(f File) string {
	b, err := os.ReadFile(f.Path)
	if err != nil {
		return ""
	}
	return string(b)
}

// Sync copies src's content into dst. An existing dst is backed up first.
func Sync(src, dst File) (backup string, err error) {
	b, err := os.ReadFile(src.Path)
	if err != nil {
		return "", err
	}
	if dst.Exists {
		backup = dst.Path + ".aictl.bak"
		old, err := os.ReadFile(dst.Path)
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(backup, old, 0o644); err != nil {
			return "", err
		}
	}
	if err := os.MkdirAll(filepath.Dir(dst.Path), 0o755); err != nil {
		return backup, err
	}
	return backup, os.WriteFile(dst.Path, b, 0o644)
}

// DiffLine is one line of a unified-style diff.
type DiffLine struct {
	Op   byte // ' ', '-', '+'
	Text string
}

// Diff is a plain LCS line diff; memory files are small.
func Diff(a, b string) []DiffLine {
	x := splitLines(a)
	y := splitLines(b)
	n, m := len(x), len(y)
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if x[i] == y[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}
	var out []DiffLine
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case x[i] == y[j]:
			out = append(out, DiffLine{' ', x[i]})
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			out = append(out, DiffLine{'-', x[i]})
			i++
		default:
			out = append(out, DiffLine{'+', y[j]})
			j++
		}
	}
	for ; i < n; i++ {
		out = append(out, DiffLine{'-', x[i]})
	}
	for ; j < m; j++ {
		out = append(out, DiffLine{'+', y[j]})
	}
	return out
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimRight(s, "\n"), "\n")
}
