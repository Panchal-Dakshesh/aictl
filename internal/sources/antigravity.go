package sources

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Panchal-Dakshesh/aictl/internal/session"
)

// Antigravity reads Google Antigravity agent conversations:
// ~/.gemini/antigravity/conversations/<id>.db (SQLite, one per conversation,
// steps stored as protobuf). Titles come from agyhub_summaries_proto.pb.
//
// Field numbers below were worked out from real files, not a published schema:
//
//	steps.step_type 14 = user input,     payload 19.2 = text
//	steps.step_type 15 = model response, payload 20.1 = text
//	tool steps carry 5.4.2 = tool name
//	payload 5.1.1 = unix seconds
//	trajectory_metadata_blob: 5 = parent conversation (sub-agents), 18 = workspace
type Antigravity struct {
	titles map[string]agSummary
}

type agSummary struct {
	Title   string
	Updated time.Time
}

const (
	agUser  = 14
	agModel = 15
)

func (a *Antigravity) Name() string { return "antigravity" }
func (a *Antigravity) Roots() []string {
	return []string{session.HomePath(".gemini", "antigravity")}
}

func (a *Antigravity) Files() ([]string, error) {
	return glob(filepath.Join(a.Roots()[0], "conversations", "*.db")), nil
}

func (a *Antigravity) summaries() map[string]agSummary {
	if a.titles != nil {
		return a.titles
	}
	a.titles = map[string]agSummary{}
	b, err := os.ReadFile(filepath.Join(a.Roots()[0], "agyhub_summaries_proto.pb"))
	if err != nil {
		return a.titles
	}
	// repeated field 1 { 1: id, 2: { 1: title, 3.1: updated } }
	var id string
	var cur agSummary
	flush := func() {
		if id != "" {
			a.titles[id] = cur
		}
		id, cur = "", agSummary{}
	}
	for _, f := range pbFields(b) {
		switch {
		case f.Path == ".1.1" && f.IsS:
			flush()
			id = f.Str
		case f.Path == ".1.2.1" && f.IsS:
			cur.Title = f.Str
		case f.Path == ".1.2.3.1" && !f.IsS:
			cur.Updated = time.Unix(int64(f.Int), 0)
		}
	}
	flush()
	return a.titles
}

type agRow struct {
	Idx  int    `json:"idx"`
	Type int    `json:"step_type"`
	Hex  string `json:"p"`
}

func (a *Antigravity) rows(db string, types string) ([]agRow, error) {
	q := "select idx, step_type, hex(step_payload) as p from steps"
	if types != "" {
		q += " where step_type in (" + types + ")"
	}
	var rows []agRow
	err := sqliteJSON(db, q+" order by idx", &rows)
	return rows, err
}

func (a *Antigravity) meta(db string) []pbField {
	var rows []struct {
		Hex string `json:"p"`
	}
	if sqliteJSON(db, "select hex(data) as p from trajectory_metadata_blob limit 1", &rows) != nil || len(rows) == 0 {
		return nil
	}
	b, _ := hex.DecodeString(rows[0].Hex)
	return pbFields(b)
}

func (a *Antigravity) Parse(path string) ([]*session.Session, error) {
	id := strings.TrimSuffix(filepath.Base(path), ".db")
	meta := a.meta(path)
	if pbStr(meta, "5") != "" { // a sub-agent spawned by another conversation
		return nil, nil
	}
	s := &session.Session{Tool: a.Name(), ID: id, Path: path}
	if ws := pbStr(meta, "18"); strings.HasPrefix(ws, "/") || strings.HasPrefix(ws, "file://") {
		s.Project = strings.TrimPrefix(ws, "file://")
	}
	rows, err := a.rows(path, "14,15")
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		b, _ := hex.DecodeString(r.Hex)
		fs := pbFields(b)
		if ts := pbInt(fs, "5.1.1"); ts > 0 {
			t := time.Unix(int64(ts), 0)
			if s.Started.IsZero() {
				s.Started = t
			}
			s.Updated = t
		}
		switch r.Type {
		case agUser:
			if t := pbStr(fs, "19.2"); t != "" {
				s.Messages++
				if s.Title == "" {
					s.Title = session.Title(t)
				}
			}
		case agModel:
			if pbStr(fs, "20.1") != "" {
				s.Messages++
			}
		}
	}
	if sum, ok := a.summaries()[id]; ok {
		if sum.Title != "" {
			s.Title = session.Title(sum.Title)
		}
		if sum.Updated.After(s.Updated) {
			s.Updated = sum.Updated
		}
	}
	if s.Title == "" {
		s.Title = "(untitled)"
	}
	return []*session.Session{s}, nil
}

func (a *Antigravity) Load(s *session.Session) ([]session.Message, error) {
	rows, err := a.rows(s.Path, "")
	if err != nil {
		return nil, err
	}
	var out []session.Message
	for _, r := range rows {
		b, _ := hex.DecodeString(r.Hex)
		fs := pbFields(b)
		t := time.Unix(int64(pbInt(fs, "5.1.1")), 0)
		switch r.Type {
		case agUser:
			if x := pbStr(fs, "19.2"); x != "" {
				out = append(out, session.Message{Role: session.User, Text: x, Time: t})
			}
		case agModel:
			if x := pbStr(fs, "20.1"); x != "" {
				out = append(out, session.Message{Role: session.Assistant, Text: x, Time: t})
			}
		default:
			if name := pbStr(fs, "5.4.2"); name != "" {
				out = append(out, session.Message{Role: session.Tool, Name: name, Time: t})
			}
		}
	}
	return out, nil
}

// Antigravity has no CLI to reopen a conversation; aictl offers handoff instead.
func (a *Antigravity) Resume(s *session.Session) (string, []string, bool) {
	return s.Project, nil, false
}
