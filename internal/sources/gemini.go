package sources

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/Panchal-Dakshesh/aictl/internal/session"
)

// Gemini reads Gemini CLI chats: ~/.gemini/tmp/<project-hash>/chats/session-*.json
type Gemini struct{}

func (g *Gemini) Name() string    { return "gemini" }
func (g *Gemini) Roots() []string { return []string{session.HomePath(".gemini", "tmp")} }

func (g *Gemini) Files() ([]string, error) {
	return glob(filepath.Join(g.Roots()[0], "*", "chats", "*.json")), nil
}

type geminiFile struct {
	SessionID   string `json:"sessionId"`
	ProjectHash string `json:"projectHash"`
	StartTime   string `json:"startTime"`
	LastUpdated string `json:"lastUpdated"`
	Messages    []struct {
		Timestamp string          `json:"timestamp"`
		Type      string          `json:"type"`
		Content   json.RawMessage `json:"content"`
		Model     string          `json:"model"`
		ToolCalls []struct {
			Name string `json:"name"`
		} `json:"toolCalls"`
	} `json:"messages"`
}

func (g *Gemini) read(path string) (*geminiFile, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f geminiFile
	return &f, json.Unmarshal(b, &f)
}

// projectFor looks for the project root Gemini CLI records next to the chats.
func projectFor(path string) string {
	dir := filepath.Dir(filepath.Dir(path))
	for _, name := range []string{".project_root", "project_root"} {
		if b, err := os.ReadFile(filepath.Join(dir, name)); err == nil {
			return strings.TrimSpace(string(b))
		}
	}
	return ""
}

func geminiText(raw json.RawMessage) string {
	t, _ := textOf(raw)
	return t
}

func (g *Gemini) Parse(path string) ([]*session.Session, error) {
	f, err := g.read(path)
	if err != nil {
		return nil, err
	}
	s := &session.Session{Tool: g.Name(), ID: f.SessionID, Path: path, Project: projectFor(path),
		Started: parseTime(f.StartTime), Updated: parseTime(f.LastUpdated)}
	if s.ID == "" {
		s.ID = strings.TrimSuffix(filepath.Base(path), ".json")
	}
	for _, m := range f.Messages {
		text := geminiText(m.Content)
		if strings.TrimSpace(text) == "" {
			continue
		}
		switch m.Type {
		case "user":
			s.Messages++
			if s.Title == "" {
				s.Title = session.Title(text)
			}
		case "gemini", "model", "assistant":
			s.Messages++
			if m.Model != "" {
				s.Model = m.Model
			}
		}
		if t := parseTime(m.Timestamp); !t.IsZero() && t.After(s.Updated) {
			s.Updated = t
		}
	}
	if s.Title == "" {
		s.Title = "(untitled)"
	}
	return []*session.Session{s}, nil
}

func (g *Gemini) Load(s *session.Session) ([]session.Message, error) {
	f, err := g.read(s.Path)
	if err != nil {
		return nil, err
	}
	var out []session.Message
	for _, m := range f.Messages {
		t := parseTime(m.Timestamp)
		text := geminiText(m.Content)
		switch m.Type {
		case "user":
			if text != "" {
				out = append(out, session.Message{Role: session.User, Text: text, Time: t})
			}
		case "gemini", "model", "assistant":
			if text != "" {
				out = append(out, session.Message{Role: session.Assistant, Text: text, Time: t})
			}
			for _, tc := range m.ToolCalls {
				out = append(out, session.Message{Role: session.Tool, Name: tc.Name, Time: t})
			}
		}
	}
	return out, nil
}

func (g *Gemini) Resume(s *session.Session) (string, []string, bool) {
	return s.Project, []string{"gemini", "--resume", s.ID}, true
}
