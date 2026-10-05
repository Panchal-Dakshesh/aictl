package term

import "testing"

func TestDecode(t *testing.T) {
	keys, rest := Decode([]byte("j\x1b[A\x1b[6~\r\x04é\x1b[200~pasted\nline\x1b[201~"), false)
	want := []string{"j", "up", "pgdn", "enter", "C-d", "é", "paste"}
	if len(rest) != 0 || len(keys) != len(want) {
		t.Fatalf("got %v rest %q", keys, rest)
	}
	for i, k := range keys {
		if k.String() != want[i] {
			t.Errorf("key %d: got %s want %s", i, k, want[i])
		}
	}
	if keys[6].Paste != "pasted\nline" {
		t.Errorf("paste: %q", keys[6].Paste)
	}
}

func TestDecodeLoneEscWaitsForMore(t *testing.T) {
	if keys, rest := Decode([]byte("\x1b"), false); len(keys) != 0 || len(rest) != 1 {
		t.Fatal("a lone ESC must wait: it may be the start of a sequence")
	}
	if keys, _ := Decode([]byte("\x1b"), true); len(keys) != 1 || keys[0].Code != KeyEsc {
		t.Fatal("after the timeout a lone ESC is Esc")
	}
}

func TestScreenFlushOnlyWritesChanges(t *testing.T) {
	s := NewScreen(10, 2)
	var b1, b2 stringsBuilder
	s.Text(0, 0, "hello", Style{}, 10)
	s.Flush(&b1)
	s.Clear()
	s.Text(0, 0, "help", Style{}, 10)
	s.Flush(&b2)
	if len(b2.s) >= len(b1.s) || !contains(b2.s, "p") {
		t.Errorf("second frame should be a small diff, got %q", b2.s)
	}
}

type stringsBuilder struct{ s string }

func (b *stringsBuilder) Write(p []byte) (int, error) { b.s += string(p); return len(p), nil }

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestEmojiWidth(t *testing.T) {
	if w := StringWidth("Done ❤️"); w != 7 {
		t.Errorf("heart with VS16 should be 2 wide, got total %d", w)
	}
	s := NewScreen(10, 1)
	if n := s.Text(0, 0, "Done ❤️", Style{}, 10); n != 7 {
		t.Errorf("Text used %d", n)
	}
	if StringWidth("✓ ok") != 4 {
		t.Error("check mark stays narrow")
	}
}
