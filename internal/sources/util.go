// Package sources holds one adapter per AI tool. Each adapter reads the
// tool's own files read-only and normalises them into session.Session.
package sources

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Panchal-Dakshesh/aictl/internal/session"
)

// All returns every adapter aictl knows about. Missing tools simply find no files.
func All() []session.Source {
	return []session.Source{&Claude{}, &Codex{}, &Gemini{}, &Antigravity{}, &Cursor{}}
}

// eachLine calls fn for every line of a JSONL file, however long the line is.
func eachLine(path string, fn func(line []byte) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 1<<20)
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			if e := fn(line); e != nil {
				if errors.Is(e, errStop) {
					return nil
				}
				return e
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

var errStop = errors.New("stop")

// textOf pulls readable text out of the many shapes "content" takes:
// a plain string, or an array of {type, text} parts.
func textOf(raw json.RawMessage) (text string, tools []string) {
	if len(raw) == 0 {
		return "", nil
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s, nil
	}
	var parts []struct {
		Type  string          `json:"type"`
		Text  string          `json:"text"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		return "", nil
	}
	var b []string
	for _, p := range parts {
		switch p.Type {
		case "text", "input_text", "output_text":
			if p.Text != "" {
				b = append(b, p.Text)
			}
		case "tool_use":
			tools = append(tools, p.Name)
		}
	}
	return strings.Join(b, "\n"), tools
}

func parseTime(s string) time.Time {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05.000Z"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func glob(pattern string) []string {
	m, _ := filepath.Glob(pattern)
	return m
}

func walkExt(root, ext string) []string {
	var out []string
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() && strings.HasSuffix(p, ext) {
			out = append(out, p)
		}
		return nil
	})
	return out
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

// isInjected spots prompts the tools add themselves (environment context,
// instructions, caveats) so they don't become titles or count as turns.
func isInjected(t string) bool {
	t = strings.TrimSpace(t)
	for _, p := range []string{"<environment_context>", "<user_instructions>", "<system-reminder>", "Caveat:", "<local-command-stdout>", "<command-name>/clear", "# AGENTS.md instructions", "<INSTRUCTIONS>"} {
		if strings.HasPrefix(t, p) {
			return true
		}
	}
	return false
}

// sqliteJSON runs a read-only query through the sqlite3 CLI that ships with
// macOS, keeping aictl free of cgo and third-party modules.
func sqliteJSON(db, query string, out any) error {
	bin, err := exec.LookPath("sqlite3")
	if err != nil {
		return errors.New("sqlite3 not found on PATH")
	}
	cmd := exec.Command(bin, "-readonly", "-json", db, query)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	b, err := cmd.Output()
	if err != nil {
		return errors.New(strings.TrimSpace(stderr.String() + " " + err.Error()))
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		b = []byte("[]")
	}
	return json.Unmarshal(b, out)
}

func sqlQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
