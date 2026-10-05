package handoff

import (
	"strings"
	"testing"

	"github.com/Panchal-Dakshesh/aictl/internal/session"
)

func TestRedact(t *testing.T) {
	in := "key sk-ant-abcdefghijklmnopqrstuvwxyz0123 and AKIAABCDEFGHIJKLMNOP and password=hunter2hunter2 and ghp_" + strings.Repeat("a", 36)
	out := Redact(in)
	for _, leak := range []string{"sk-ant-abc", "AKIAABCD", "hunter2", "ghp_aaaa"} {
		if strings.Contains(out, leak) {
			t.Errorf("leaked %s: %s", leak, out)
		}
	}
	if !strings.Contains(out, "password=[redacted]") {
		t.Errorf("label should survive: %s", out)
	}
}

func TestCompactKeepsFirstAskAndTail(t *testing.T) {
	var msgs []session.Message
	msgs = append(msgs, session.Message{Role: session.User, Text: "the original ask"})
	for i := 0; i < 40; i++ {
		msgs = append(msgs, session.Message{Role: session.Assistant, Text: "turn"})
	}
	msgs = append(msgs, session.Message{Role: session.User, Text: "latest ask"})
	md := Context(&session.Session{Tool: "claude", ID: "x", Title: "t"}, msgs, Compact)
	if !strings.Contains(md, "the original ask") || !strings.Contains(md, "latest ask") || strings.Count(md, "## Assistant") > compactTail {
		t.Errorf("compact brief is wrong:\n%s", md)
	}
}

func TestQuote(t *testing.T) {
	got := Quote("/tmp/my dir", []string{"claude", "--resume", "abc"})
	if got != "cd '/tmp/my dir' && claude --resume abc" {
		t.Errorf("got %s", got)
	}
}
