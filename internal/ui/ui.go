// Package ui has the small set of drawing primitives aictl's screens are
// built from: theme, boxes, wrapped text. Everything renders into term.Screen.
package ui

import (
	"strings"

	"github.com/Panchal-Dakshesh/aictl/internal/term"
)

// Theme: follows the terminal's own fg/bg, with a celeste accent. Works on
// light and dark terminals alike.
var (
	Accent    = term.Hex("#0e9f9f")
	AccentInk = term.Hex("#0b6e6d")
	Celeste   = term.Hex("#b8f0f0")
	Ink       = term.Hex("#1f2328")
	Muted     = term.Hex("#8c959f")
	Red       = term.Hex("#cf222e")
	Green     = term.Hex("#1a7f37")
	Yellow    = term.Hex("#bf8700")
	Purple    = term.Hex("#8250df")

	Plain    = term.Style{}
	Dim      = term.Style{Fg: Muted}
	Bold     = term.Style{Bold: true}
	Selected = term.Style{Bg: Celeste, Fg: Ink}
	AccentSt = term.Style{Fg: Accent}
	Border   = term.Style{Fg: Muted}
	BorderOn = term.Style{Fg: Accent}
)

// ToolColor gives each tool a stable colour.
func ToolColor(tool string) term.Color {
	switch tool {
	case "claude":
		return term.Hex("#d97757")
	case "codex":
		return term.Hex("#10a37f")
	case "gemini":
		return term.Hex("#4285f4")
	case "antigravity":
		return term.Hex("#8250df")
	case "cursor":
		return term.Hex("#57606a")
	}
	return Muted
}

// Box draws a rounded border with an optional title; returns the inner rect.
func Box(s *term.Screen, x, y, w, h int, title string, focused bool) (int, int, int, int) {
	if w < 2 || h < 2 {
		return x, y, 0, 0
	}
	st := Border
	if focused {
		st = BorderOn
	}
	s.Set(x, y, '╭', st)
	s.Set(x+w-1, y, '╮', st)
	s.Set(x, y+h-1, '╰', st)
	s.Set(x+w-1, y+h-1, '╯', st)
	for i := x + 1; i < x+w-1; i++ {
		s.Set(i, y, '─', st)
		s.Set(i, y+h-1, '─', st)
	}
	for j := y + 1; j < y+h-1; j++ {
		s.Set(x, j, '│', st)
		s.Set(x+w-1, j, '│', st)
	}
	if title != "" && w > 6 {
		tst := Dim
		if focused {
			tst = term.Style{Fg: Accent, Bold: true}
		}
		s.Text(x+2, y, " "+title+" ", tst, w-4)
	}
	return x + 1, y + 1, w - 2, h - 2
}

// Wrap breaks text into lines no wider than width, keeping explicit newlines.
func Wrap(text string, width int) []string {
	if width < 4 {
		width = 4
	}
	var out []string
	for _, para := range strings.Split(strings.ReplaceAll(text, "\r", ""), "\n") {
		para = strings.ReplaceAll(para, "\t", "    ")
		if strings.TrimSpace(para) == "" {
			out = append(out, "")
			continue
		}
		indent := len(para) - len(strings.TrimLeft(para, " "))
		if indent > width/2 {
			indent = 0
		}
		pad := strings.Repeat(" ", indent)
		line := pad
		lw := indent
		for _, word := range strings.Fields(para) {
			ww := term.StringWidth(word)
			for ww > width-indent { // hard-split very long words (urls, paths)
				room := width - lw
				if room <= 0 {
					out = append(out, line)
					line, lw = pad, indent
					room = width - indent
				}
				head, rest := splitWidth(word, room)
				out = append(out, line+head)
				line, lw = pad, indent
				word = rest
				ww = term.StringWidth(word)
			}
			if lw > indent && lw+1+ww > width {
				out = append(out, line)
				line, lw = pad, indent
			}
			if lw > indent {
				line += " "
				lw++
			}
			line += word
			lw += ww
		}
		out = append(out, line)
	}
	return out
}

func splitWidth(s string, w int) (string, string) {
	n := 0
	for i, r := range s {
		rw := term.RuneWidth(r)
		if n+rw > w {
			return s[:i], s[i:]
		}
		n += rw
	}
	return s, ""
}

// Pad right-pads or truncates s to exactly w columns.
func Pad(s string, w int) string {
	sw := term.StringWidth(s)
	if sw > w {
		h, _ := splitWidth(s, w-1)
		return h + "…"
	}
	return s + strings.Repeat(" ", w-sw)
}
