// Package handoff moves context between tools: it renders a session as a
// Markdown brief, masks secrets, writes it to disk, and starts the target tool.
package handoff

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Panchal-Dakshesh/aictl/internal/session"
)

type Mode int

const (
	Compact Mode = iota // first prompt + the most recent turns
	Full                // everything
)

const compactTail = 16

// Context renders a session as Markdown another agent can pick up from.
func Context(s *session.Session, msgs []session.Message, mode Mode) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Context handoff from %s\n\n", s.Tool)
	fmt.Fprintf(&b, "- Session: `%s`\n", s.ID)
	if s.Project != "" {
		fmt.Fprintf(&b, "- Project: `%s`\n", s.Project)
	}
	if s.Branch != "" {
		fmt.Fprintf(&b, "- Branch: `%s`\n", s.Branch)
	}
	if !s.Updated.IsZero() {
		fmt.Fprintf(&b, "- Last active: %s\n", s.Updated.Format("2006-01-02 15:04"))
	}
	fmt.Fprintf(&b, "- Title: %s\n\n", s.Title)

	var conv []session.Message
	for _, m := range msgs {
		if m.Role == session.User || m.Role == session.Assistant {
			conv = append(conv, m)
		}
	}
	if mode == Compact && len(conv) > compactTail+1 {
		fmt.Fprintf(&b, "_Compact handoff: the opening request and the last %d of %d messages._\n\n", compactTail, len(conv))
		writeMsg(&b, conv[0])
		b.WriteString("\n---\n\n_…earlier turns omitted…_\n\n")
		conv = conv[len(conv)-compactTail:]
	}
	for _, m := range conv {
		writeMsg(&b, m)
	}
	return Redact(b.String())
}

func writeMsg(b *strings.Builder, m session.Message) {
	who := "User"
	if m.Role == session.Assistant {
		who = "Assistant"
	}
	fmt.Fprintf(b, "## %s\n\n%s\n\n", who, strings.TrimSpace(m.Text))
}

// Write saves the brief and returns its path.
func Write(s *session.Session, md string) (string, error) {
	dir := filepath.Join(session.CacheDir(), "handoff")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	id := s.ID
	if len(id) > 8 {
		id = id[:8]
	}
	p := filepath.Join(dir, fmt.Sprintf("%s-%s-%s.md", s.Tool, id, time.Now().Format("20060102-150405")))
	return p, os.WriteFile(p, []byte(md), 0o600)
}

// Prompt is the opening message given to the target tool.
func Prompt(from, path string) string {
	return fmt.Sprintf("I'm continuing work from a previous %s session. The conversation so far is in %s. "+
		"Read it first, then give me a short summary of where we left off and what the next step should be. "+
		"Don't change anything until I confirm.", from, path)
}

// Target is a tool aictl can start with a context brief.
type Target struct {
	Name string
	Bin  string
	Args func(prompt string) []string
}

var Targets = []Target{
	{"claude", "claude", func(p string) []string { return []string{p} }},
	{"codex", "codex", func(p string) []string { return []string{p} }},
	{"gemini", "gemini", func(p string) []string { return []string{"-i", p} }},
	{"cursor-agent", "cursor-agent", func(p string) []string { return []string{p} }},
}

func (t Target) Available() bool {
	_, err := exec.LookPath(t.Bin)
	return err == nil
}

func FindTarget(name string) (Target, bool) {
	for _, t := range Targets {
		if t.Name == name || (name == "cursor" && t.Name == "cursor-agent") {
			return t, true
		}
	}
	return Target{}, false
}

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?-----END [A-Z ]*PRIVATE KEY-----`),
	regexp.MustCompile(`\bsk-(?:ant-|proj-)?[A-Za-z0-9_\-]{20,}`),
	regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{30,}`),
	regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{30,}`),
	regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`),
	regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9\-]{10,}`),
	regexp.MustCompile(`\bAIza[0-9A-Za-z_\-]{35}\b`),
	regexp.MustCompile(`(?i)\b(api[_-]?key|secret|token|password|passwd)(\s*[:=]\s*)["']?[^\s"']{8,}`),
}

// Redact masks things that look like credentials.
func Redact(s string) string {
	for i, re := range secretPatterns {
		if i == len(secretPatterns)-1 {
			s = re.ReplaceAllString(s, "$1$2[redacted]")
			continue
		}
		s = re.ReplaceAllString(s, "[redacted]")
	}
	return s
}

// Copy puts text on the system clipboard, falling back to OSC 52 so it also
// works over SSH.
func Copy(text string) error {
	for _, c := range [][]string{{"pbcopy"}, {"wl-copy"}, {"xclip", "-selection", "clipboard"}, {"xsel", "--clipboard", "--input"}} {
		if _, err := exec.LookPath(c[0]); err != nil {
			continue
		}
		cmd := exec.Command(c[0], c[1:]...)
		cmd.Stdin = strings.NewReader(text)
		if cmd.Run() == nil {
			return nil
		}
	}
	tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer tty.Close()
	_, err = fmt.Fprintf(tty, "\x1b]52;c;%s\a", base64.StdEncoding.EncodeToString([]byte(text)))
	return err
}

// Quote renders argv as a copy-pasteable shell command.
func Quote(dir string, argv []string) string {
	q := make([]string, len(argv))
	for i, a := range argv {
		if a != "" && !strings.ContainsAny(a, " \t\n'\"$`\\!*?&;|<>(){}[]#~") {
			q[i] = a
		} else {
			q[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
		}
	}
	cmd := strings.Join(q, " ")
	if dir != "" {
		cmd = "cd " + Quote("", []string{dir}) + " && " + cmd
	}
	return cmd
}
