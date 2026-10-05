package sources

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"time"

	"github.com/Panchal-Dakshesh/aictl/internal/session"
)

// Claude reads Claude Code transcripts: ~/.claude/projects/<dir>/<session>.jsonl
type Claude struct{}

func (c *Claude) Name() string    { return "claude" }
func (c *Claude) Roots() []string { return []string{session.HomePath(".claude", "projects")} }

func (c *Claude) Files() ([]string, error) {
	var out []string
	for _, f := range glob(filepath.Join(c.Roots()[0], "*", "*.jsonl")) {
		if strings.HasPrefix(filepath.Base(f), "agent-") { // sub-agent side chains
			continue
		}
		out = append(out, f)
	}
	return out, nil
}

type claudeLine struct {
	Type        string          `json:"type"`
	SessionID   string          `json:"sessionId"`
	Cwd         string          `json:"cwd"`
	Timestamp   string          `json:"timestamp"`
	GitBranch   string          `json:"gitBranch"`
	IsSidechain bool            `json:"isSidechain"`
	IsMeta      bool            `json:"isMeta"`
	Summary     string          `json:"summary"`
	Message     json.RawMessage `json:"message"`
}

type claudeMsg struct {
	Role    string          `json:"role"`
	Model   string          `json:"model"`
	Content json.RawMessage `json:"content"`
}

func (c *Claude) walk(path string, fn func(l *claudeLine, m *claudeMsg, text string, tools []string)) error {
	return eachLine(path, func(b []byte) error {
		var l claudeLine
		if json.Unmarshal(b, &l) != nil {
			return nil
		}
		var m claudeMsg
		var text string
		var tools []string
		if len(l.Message) > 0 {
			json.Unmarshal(l.Message, &m)
			text, tools = textOf(m.Content)
		}
		fn(&l, &m, text, tools)
		return nil
	})
}

func (c *Claude) Parse(path string) ([]*session.Session, error) {
	s := &session.Session{Tool: c.Name(), ID: strings.TrimSuffix(filepath.Base(path), ".jsonl"), Path: path}
	var summary string
	err := c.walk(path, func(l *claudeLine, m *claudeMsg, text string, _ []string) {
		if l.Type == "summary" && l.Summary != "" && summary == "" {
			summary = l.Summary
			return
		}
		if l.SessionID != "" {
			s.ID = l.SessionID
		}
		if l.Cwd != "" && s.Project == "" {
			s.Project = l.Cwd
		}
		if l.GitBranch != "" {
			s.Branch = l.GitBranch
		}
		if t := parseTime(l.Timestamp); !t.IsZero() {
			if s.Started.IsZero() {
				s.Started = t
			}
			s.Updated = t
		}
		if l.IsSidechain || l.IsMeta || strings.TrimSpace(text) == "" {
			return
		}
		switch l.Type {
		case "user":
			if isInjected(text) {
				return
			}
			s.Messages++
			if s.Title == "" {
				s.Title = session.Title(text)
			}
		case "assistant":
			s.Messages++
			if m.Model != "" && !strings.HasPrefix(m.Model, "<") {
				s.Model = m.Model
			}
		}
	})
	if summary != "" {
		s.Title = session.Title(summary)
	}
	if s.Title == "" {
		s.Title = "(untitled)"
	}
	return []*session.Session{s}, err
}

func (c *Claude) Load(s *session.Session) ([]session.Message, error) {
	var out []session.Message
	err := c.walk(s.Path, func(l *claudeLine, m *claudeMsg, text string, tools []string) {
		if l.IsSidechain || l.IsMeta {
			return
		}
		t := parseTime(l.Timestamp)
		switch l.Type {
		case "user":
			if strings.TrimSpace(text) != "" && !isInjected(text) {
				out = append(out, session.Message{Role: session.User, Text: session.StripNoise(text), Time: t})
			}
		case "assistant":
			if strings.TrimSpace(text) != "" {
				out = append(out, session.Message{Role: session.Assistant, Text: text, Time: t})
			}
			for _, name := range tools {
				out = append(out, session.Message{Role: session.Tool, Name: name, Time: t})
			}
		}
	})
	return out, err
}

func (c *Claude) Resume(s *session.Session) (string, []string, bool) {
	return s.Project, []string{"claude", "--resume", s.ID}, true
}

var _ = time.Now
