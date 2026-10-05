package ui

import "testing"

func TestWrap(t *testing.T) {
	got := Wrap("one two three four\n\n  indented line here", 10)
	want := []string{"one two", "three four", "", "  indented", "  line", "  here"}
	if len(got) != len(want) {
		t.Fatalf("got %q", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d: %q want %q", i, got[i], want[i])
		}
	}
	for _, l := range Wrap("https://example.com/a/very/long/url/that/does/not/fit", 12) {
		if len(l) > 12 {
			t.Errorf("long word not split: %q", l)
		}
	}
}
