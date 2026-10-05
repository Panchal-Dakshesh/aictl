package sources

import (
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/Panchal-Dakshesh/aictl/internal/session"
)

// Codex reads OpenAI Codex CLI rollouts: ~/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl
// It understands both the current {type,payload} envelope and the older flat format.
type Codex struct{}

func (c *Codex) Name() string    { return "codex" }
func (c *Codex) Roots() []string { return []string{session.HomePath(".codex", "sessions")} }

func (c *Codex) Files() ([]string, error) {
	return walkExt(c.Roots()[0], ".jsonl"), nil
}

type codexLine struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
	// legacy flat fields
	ID      string          `json:"id"`
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type codexPayload struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Cwd       string          `json:"cwd"`
	Model     string          `json:"model"`
	Timestamp string          `json:"timestamp"`
	Role      string          `json:"role"`
	Content   json.RawMessage `json:"content"`
	Name      string          `json:"name"`
	Git       struct {
		Branch string `json:"branch"`
	} `json:"git"`
}

func (c *Codex) walk(path string, fn func(ts string, kind string, p *codexPayload)) error {
	return eachLine(path, func(b []byte) error {
		var l codexLine
		if json.Unmarshal(b, &l) != nil {
			return nil
		}
		var p codexPayload
		kind := l.Type
		if len(l.Payload) > 0 {
			json.Unmarshal(l.Payload, &p)
		} else {
			// legacy: the line itself is the item; the first line is session meta
			json.Unmarshal(b, &p)
			if kind == "" && l.ID != "" {
				kind = "session_meta"
			} else {
				kind = "response_item"
			}
		}
		fn(l.Timestamp, kind, &p)
		return nil
	})
}

func (c *Codex) Parse(path string) ([]*session.Session, error) {
	s := &session.Session{Tool: c.Name(), Path: path}
	base := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	if len(base) >= 36 {
		s.ID = base[len(base)-36:]
	}
	err := c.walk(path, func(ts, kind string, p *codexPayload) {
		if t := parseTime(ts); !t.IsZero() {
			if s.Started.IsZero() {
				s.Started = t
			}
			s.Updated = t
		}
		switch kind {
		case "session_meta":
			if p.ID != "" {
				s.ID = p.ID
			}
			if p.Cwd != "" {
				s.Project = p.Cwd
			}
			if p.Git.Branch != "" {
				s.Branch = p.Git.Branch
			}
			if t := parseTime(p.Timestamp); !t.IsZero() && s.Started.IsZero() {
				s.Started, s.Updated = t, t
			}
		case "turn_context":
			if p.Model != "" {
				s.Model = p.Model
			}
			if p.Cwd != "" && s.Project == "" {
				s.Project = p.Cwd
			}
		case "response_item":
			if p.Type != "message" && p.Type != "" {
				return
			}
			text, _ := textOf(p.Content)
			if strings.TrimSpace(text) == "" || isInjected(text) {
				return
			}
			switch p.Role {
			case "user":
				s.Messages++
				if s.Title == "" {
					s.Title = session.Title(text)
				}
			case "assistant":
				s.Messages++
			}
		}
	})
	if s.Title == "" {
		s.Title = "(untitled)"
	}
	return []*session.Session{s}, err
}

func (c *Codex) Load(s *session.Session) ([]session.Message, error) {
	var out []session.Message
	err := c.walk(s.Path, func(ts, kind string, p *codexPayload) {
		if kind != "response_item" {
			return
		}
		t := parseTime(ts)
		switch p.Type {
		case "message", "":
			text, _ := textOf(p.Content)
			if strings.TrimSpace(text) == "" || isInjected(text) {
				return
			}
			role := session.Assistant
			if p.Role == "user" {
				role = session.User
			}
			out = append(out, session.Message{Role: role, Text: text, Time: t})
		case "function_call", "local_shell_call", "custom_tool_call":
			name := p.Name
			if name == "" {
				name = "shell"
			}
			out = append(out, session.Message{Role: session.Tool, Name: name, Time: t})
		}
	})
	return out, err
}

func (c *Codex) Resume(s *session.Session) (string, []string, bool) {
	return s.Project, []string{"codex", "resume", s.ID}, true
}
