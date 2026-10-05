// Package session defines the one shape every tool's history is normalised into.
package session

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

type Role string

const (
	User      Role = "user"
	Assistant Role = "assistant"
	Tool      Role = "tool"
	System    Role = "system"
)

type Message struct {
	Role Role      `json:"role"`
	Text string    `json:"text"`
	Time time.Time `json:"time,omitempty"`
	Name string    `json:"name,omitempty"` // tool name for tool calls
}

// Session is one conversation in one tool.
type Session struct {
	Tool     string    `json:"tool"`
	ID       string    `json:"id"`
	Title    string    `json:"title"`
	Project  string    `json:"project"` // working directory, if the tool records one
	Path     string    `json:"path"`    // file the session was read from
	Started  time.Time `json:"started"`
	Updated  time.Time `json:"updated"`
	Messages int       `json:"messages"` // user + assistant turns
	Model    string    `json:"model,omitempty"`
	Branch   string    `json:"branch,omitempty"`
}

// Source is implemented once per tool.
type Source interface {
	Name() string
	// Roots returns where this tool keeps its data on this machine.
	Roots() []string
	// Scan lists session files. It must be cheap: the index decides which
	// files actually need parsing.
	Files() ([]string, error)
	// Parse reads one file into sessions (a file may hold more than one).
	Parse(path string) ([]*Session, error)
	// Load returns the full transcript for a session.
	Load(s *Session) ([]Message, error)
	// Resume returns the directory and argv that reopen the session,
	// or ok=false if the tool can't resume from outside its own UI.
	Resume(s *Session) (dir string, argv []string, ok bool)
}

// Home returns the user's home, overridable for tests via AICTL_HOME.
func Home() string {
	if h := os.Getenv("AICTL_HOME"); h != "" {
		return h
	}
	h, _ := os.UserHomeDir()
	return h
}

func HomePath(parts ...string) string {
	return filepath.Join(append([]string{Home()}, parts...)...)
}

// CacheDir is where aictl keeps its own index and handoff files.
func CacheDir() string {
	if d := os.Getenv("AICTL_CACHE"); d != "" {
		return d
	}
	if d := os.Getenv("XDG_CACHE_HOME"); d != "" {
		return filepath.Join(d, "aictl")
	}
	return HomePath(".cache", "aictl")
}

// Title makes a one-line title out of a first prompt.
func Title(s string) string {
	s = strings.TrimSpace(StripNoise(s))
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	s = strings.Join(strings.Fields(s), " ")
	return Truncate(s, 90)
}

// StripNoise removes the XML-ish wrappers tools put around prompts
// (<command-name>, <system-reminder> ...) so titles read like what you typed.
func StripNoise(s string) string {
	for {
		i := strings.Index(s, "<")
		if i < 0 {
			return s
		}
		j := strings.Index(s[i:], ">")
		if j < 0 {
			return s
		}
		tag := s[i+1 : i+j]
		if tag == "" || strings.ContainsAny(tag, " =\"'") || strings.HasPrefix(tag, "/") {
			return s
		}
		end := strings.Index(s, "</"+tag+">")
		if end < 0 {
			return s
		}
		inner := s[i+j+1 : end]
		switch tag {
		case "command-name", "command-message", "command-args", "user_query", "user_message":
			s = s[:i] + inner + s[end+len(tag)+3:]
		default:
			s = s[:i] + s[end+len(tag)+3:]
		}
	}
}

func Truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}

// Sort newest first.
func Sort(ss []*Session) {
	sort.SliceStable(ss, func(i, j int) bool { return ss[i].Updated.After(ss[j].Updated) })
}

// Ago renders a compact relative time: 5m, 3h, 2d, 6w, 2y.
func Ago(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return itoa(int(d.Minutes())) + "m"
	case d < 48*time.Hour:
		return itoa(int(d.Hours())) + "h"
	case d < 14*24*time.Hour:
		return itoa(int(d.Hours()/24)) + "d"
	case d < 365*24*time.Hour:
		return itoa(int(d.Hours()/24/7)) + "w"
	}
	return itoa(int(d.Hours()/24/365)) + "y"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// ShortPath replaces $HOME with ~.
func ShortPath(p string) string {
	h := Home()
	if h != "" && strings.HasPrefix(p, h) {
		return "~" + p[len(h):]
	}
	return p
}
