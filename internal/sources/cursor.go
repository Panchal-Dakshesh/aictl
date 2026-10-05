package sources

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Panchal-Dakshesh/aictl/internal/session"
)

// Cursor reads the desktop app's chat/composer history from its SQLite state:
//
//	<app data>/Cursor/User/globalStorage/state.vscdb   table cursorDiskKV
//	  composerData:<id>            -> {name, createdAt, lastUpdatedAt, fullConversationHeadersOnly | conversation}
//	  bubbleId:<composer>:<bubble> -> {type: 1 user | 2 ai, text}
//	<app data>/Cursor/User/workspaceStorage/<hash>/  workspace.json + state.vscdb (maps composers to folders)
type Cursor struct {
	folders map[string]string // composerId -> folder
}

func (c *Cursor) Name() string { return "cursor" }

func (c *Cursor) userDir() string {
	if runtime.GOOS == "darwin" || os.Getenv("AICTL_HOME") != "" && exists(session.HomePath("Library")) {
		return session.HomePath("Library", "Application Support", "Cursor", "User")
	}
	return session.HomePath(".config", "Cursor", "User")
}

func (c *Cursor) Roots() []string { return []string{c.userDir()} }

func (c *Cursor) Files() ([]string, error) {
	db := filepath.Join(c.userDir(), "globalStorage", "state.vscdb")
	if exists(db) {
		return []string{db}, nil
	}
	return nil, nil
}

type cursorComposer struct {
	ComposerID    string `json:"composerId"`
	Name          string `json:"name"`
	CreatedAt     int64  `json:"createdAt"`
	LastUpdatedAt int64  `json:"lastUpdatedAt"`
	Headers       []struct {
		BubbleID string `json:"bubbleId"`
		Type     int    `json:"type"`
	} `json:"fullConversationHeadersOnly"`
	Conversation []cursorBubble `json:"conversation"`
}

type cursorBubble struct {
	BubbleID string `json:"bubbleId"`
	Type     int    `json:"type"`
	Text     string `json:"text"`
}

func (c *Cursor) workspaceFolders() map[string]string {
	if c.folders != nil {
		return c.folders
	}
	c.folders = map[string]string{}
	for _, wj := range glob(filepath.Join(c.userDir(), "workspaceStorage", "*", "workspace.json")) {
		b, err := os.ReadFile(wj)
		if err != nil {
			continue
		}
		var w struct {
			Folder string `json:"folder"`
		}
		if json.Unmarshal(b, &w) != nil || w.Folder == "" {
			continue
		}
		folder := w.Folder
		if u, err := url.Parse(w.Folder); err == nil && u.Scheme == "file" {
			folder = u.Path
		}
		var rows []struct {
			Value string `json:"value"`
		}
		db := filepath.Join(filepath.Dir(wj), "state.vscdb")
		if sqliteJSON(db, "select value from ItemTable where key = 'composer.composerData'", &rows) != nil || len(rows) == 0 {
			continue
		}
		var data struct {
			All []struct {
				ComposerID string `json:"composerId"`
			} `json:"allComposers"`
		}
		json.Unmarshal([]byte(rows[0].Value), &data)
		for _, a := range data.All {
			c.folders[a.ComposerID] = folder
		}
	}
	return c.folders
}

func (c *Cursor) Parse(path string) ([]*session.Session, error) {
	var rows []struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if err := sqliteJSON(path, "select key, value from cursorDiskKV where key like 'composerData:%'", &rows); err != nil {
		return nil, err
	}
	folders := c.workspaceFolders()
	var out []*session.Session
	for _, r := range rows {
		var cd cursorComposer
		if json.Unmarshal([]byte(r.Value), &cd) != nil || cd.ComposerID == "" {
			continue
		}
		n := len(cd.Headers)
		if n == 0 {
			n = len(cd.Conversation)
		}
		s := &session.Session{Tool: c.Name(), ID: cd.ComposerID, Path: path, Messages: n,
			Started: msTime(cd.CreatedAt), Updated: msTime(cd.LastUpdatedAt), Project: folders[cd.ComposerID]}
		if s.Updated.IsZero() {
			s.Updated = s.Started
		}
		s.Title = session.Title(cd.Name)
		if s.Title == "" {
			for _, b := range cd.Conversation {
				if b.Type == 1 && b.Text != "" {
					s.Title = session.Title(b.Text)
					break
				}
			}
		}
		if s.Title == "" {
			s.Title = "(untitled)"
		}
		out = append(out, s)
	}
	return out, nil
}

func msTime(ms int64) time.Time {
	if ms <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms)
}

func (c *Cursor) Load(s *session.Session) ([]session.Message, error) {
	var rows []struct {
		Value string `json:"value"`
	}
	if err := sqliteJSON(s.Path, "select value from cursorDiskKV where key = "+sqlQuote("composerData:"+s.ID), &rows); err != nil || len(rows) == 0 {
		return nil, err
	}
	var cd cursorComposer
	json.Unmarshal([]byte(rows[0].Value), &cd)
	bubbles := cd.Conversation
	if len(bubbles) == 0 && len(cd.Headers) > 0 {
		var brows []struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		}
		sqliteJSON(s.Path, "select key, value from cursorDiskKV where key like "+sqlQuote("bubbleId:"+s.ID+":%"), &brows)
		byID := map[string]cursorBubble{}
		for _, br := range brows {
			var b cursorBubble
			if json.Unmarshal([]byte(br.Value), &b) == nil {
				id := b.BubbleID
				if id == "" {
					id = br.Key[strings.LastIndex(br.Key, ":")+1:]
				}
				byID[id] = b
			}
		}
		for _, h := range cd.Headers {
			if b, ok := byID[h.BubbleID]; ok {
				bubbles = append(bubbles, b)
			}
		}
	}
	var out []session.Message
	for _, b := range bubbles {
		if strings.TrimSpace(b.Text) == "" {
			continue
		}
		role := session.Assistant
		if b.Type == 1 {
			role = session.User
		}
		out = append(out, session.Message{Role: role, Text: b.Text})
	}
	return out, nil
}

// The desktop app can't be told to open a specific chat from outside.
func (c *Cursor) Resume(s *session.Session) (string, []string, bool) {
	return s.Project, nil, false
}
