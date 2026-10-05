package term

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// Color is 0 for "terminal default", otherwise 0x01RRGGBB.
type Color uint32

func RGB(r, g, b uint8) Color { return Color(1<<24 | uint32(r)<<16 | uint32(g)<<8 | uint32(b)) }

func Hex(s string) Color {
	v, err := strconv.ParseUint(strings.TrimPrefix(s, "#"), 16, 32)
	if err != nil {
		return 0
	}
	return RGB(uint8(v>>16), uint8(v>>8), uint8(v))
}

type Style struct {
	Fg, Bg                                Color
	Bold, Dim, Italic, Underline, Reverse bool
}

type Cell struct {
	R  rune // 0 marks the right half of a wide rune
	St Style
}

// Screen is a double-buffered cell grid. Draw into it, then Flush writes only
// the cells that changed since the last frame.
type Screen struct {
	W, H      int
	back      []Cell
	front     []Cell
	full      bool
	truecolor bool
	sb        strings.Builder
}

func NewScreen(w, h int) *Screen {
	s := &Screen{}
	ct := os.Getenv("COLORTERM")
	s.truecolor = ct == "truecolor" || ct == "24bit" || os.Getenv("TERM_PROGRAM") == "iTerm.app" || os.Getenv("TERM_PROGRAM") == "WezTerm"
	s.Resize(w, h)
	return s
}

func (s *Screen) Resize(w, h int) {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	s.W, s.H = w, h
	s.back = make([]Cell, w*h)
	s.front = make([]Cell, w*h)
	s.full = true
	s.Clear()
}

func (s *Screen) Clear() {
	for i := range s.back {
		s.back[i] = Cell{R: ' '}
	}
}

func (s *Screen) Set(x, y int, r rune, st Style) {
	if x < 0 || y < 0 || x >= s.W || y >= s.H {
		return
	}
	s.back[y*s.W+x] = Cell{R: r, St: st}
}

// Get returns the cell at x,y of the frame being drawn.
func (s *Screen) Get(x, y int) Cell {
	if x < 0 || y < 0 || x >= s.W || y >= s.H {
		return Cell{}
	}
	return s.back[y*s.W+x]
}

// Text draws s starting at x,y, clipped to maxw columns. Returns columns used.
func (s *Screen) Text(x, y int, str string, st Style, maxw int) int {
	used := 0
	rs := []rune(str)
	for i, r := range rs {
		if r == '\t' {
			r = ' '
		}
		if r < 32 || r == 0x7f {
			continue
		}
		w := RuneWidth(r)
		if w == 0 {
			continue // variation selectors, joiners, combining marks
		}
		if i+1 < len(rs) && rs[i+1] == 0xFE0F {
			w = 2 // emoji presentation, e.g. ❤️
		}
		if used+w > maxw {
			break
		}
		s.Set(x+used, y, r, st)
		if w == 2 {
			s.Set(x+used+1, y, 0, st)
		}
		used += w
	}
	return used
}

func (s *Screen) Fill(x, y, w, h int, r rune, st Style) {
	for j := y; j < y+h; j++ {
		for i := x; i < x+w; i++ {
			s.Set(i, j, r, st)
		}
	}
}

// Flush writes the diff between this frame and the last one.
func (s *Screen) Flush(w io.Writer) error {
	b := &s.sb
	b.Reset()
	if s.full {
		b.WriteString("\x1b[0m\x1b[2J")
	}
	var cur Style
	styled := false
	lastX, lastY := -1, -1
	for y := 0; y < s.H; y++ {
		for x := 0; x < s.W; x++ {
			i := y*s.W + x
			c := s.back[i]
			if !s.full && c == s.front[i] {
				continue
			}
			if c.R == 0 { // right half of a wide rune: the terminal already advanced
				s.front[i] = c
				continue
			}
			if x != lastX || y != lastY {
				fmt.Fprintf(b, "\x1b[%d;%dH", y+1, x+1)
			}
			if !styled || c.St != cur {
				b.WriteString(s.sgr(c.St))
				cur, styled = c.St, true
			}
			b.WriteRune(c.R)
			lastX, lastY = x+RuneWidth(c.R), y
			s.front[i] = c
		}
	}
	s.full = false
	if b.Len() == 0 {
		return nil
	}
	b.WriteString("\x1b[0m")
	_, err := io.WriteString(w, b.String())
	return err
}

func (s *Screen) sgr(st Style) string {
	p := []string{"0"}
	if st.Bold {
		p = append(p, "1")
	}
	if st.Dim {
		p = append(p, "2")
	}
	if st.Italic {
		p = append(p, "3")
	}
	if st.Underline {
		p = append(p, "4")
	}
	if st.Reverse {
		p = append(p, "7")
	}
	if st.Fg != 0 {
		p = append(p, s.color(st.Fg, 38))
	}
	if st.Bg != 0 {
		p = append(p, s.color(st.Bg, 48))
	}
	return "\x1b[" + strings.Join(p, ";") + "m"
}

func (s *Screen) color(c Color, base int) string {
	r, g, b := uint8(c>>16), uint8(c>>8), uint8(c)
	if s.truecolor {
		return fmt.Sprintf("%d;2;%d;%d;%d", base, r, g, b)
	}
	return fmt.Sprintf("%d;5;%d", base, to256(r, g, b))
}

// to256 maps RGB to the nearest xterm-256 colour (cube or grey ramp).
func to256(r, g, b uint8) int {
	q := func(v uint8) int {
		if v < 48 {
			return 0
		}
		if v < 115 {
			return 1
		}
		return (int(v) - 35) / 40
	}
	cr, cg, cb := q(r), q(g), q(b)
	levels := []int{0, 95, 135, 175, 215, 255}
	cube := 16 + 36*cr + 6*cg + cb
	dc := sq(int(r)-levels[cr]) + sq(int(g)-levels[cg]) + sq(int(b)-levels[cb])
	avg := (int(r) + int(g) + int(b)) / 3
	gi := (avg - 8) / 10
	if gi < 0 {
		gi = 0
	}
	if gi > 23 {
		gi = 23
	}
	gv := 8 + gi*10
	dg := sq(int(r)-gv) + sq(int(g)-gv) + sq(int(b)-gv)
	if dg < dc {
		return 232 + gi
	}
	return cube
}

func sq(v int) int { return v * v }

// RuneWidth is a small East-Asian-width / emoji approximation.
func RuneWidth(r rune) int {
	switch {
	case r == 0,
		r == 0x200B, r == 0x200C, r == 0x200D, r == 0x2060,
		r >= 0xFE00 && r <= 0xFE0F,
		r >= 0x0300 && r <= 0x036F,
		r >= 0x20D0 && r <= 0x20FF,
		r >= 0x1F3FB && r <= 0x1F3FF, // skin tones
		r >= 0xE0000 && r <= 0xE007F:
		return 0
	case r >= 0x1100 && r <= 0x115F,
		r >= 0x231A && r <= 0x231B, r >= 0x23E9 && r <= 0x23EC, r == 0x23F0, r == 0x23F3,
		r >= 0x25FD && r <= 0x25FE, r >= 0x2614 && r <= 0x2615, r >= 0x2648 && r <= 0x2653,
		r == 0x267F, r == 0x2693, r == 0x26A1, r >= 0x26AA && r <= 0x26AB, r >= 0x26BD && r <= 0x26BE,
		r >= 0x26C4 && r <= 0x26C5, r == 0x26CE, r == 0x26D4, r == 0x26EA, r >= 0x26F2 && r <= 0x26F5,
		r == 0x26FA, r == 0x26FD, r == 0x2705, r >= 0x270A && r <= 0x270B, r == 0x2728, r == 0x274C,
		r == 0x274E, r >= 0x2753 && r <= 0x2755, r == 0x2757, r >= 0x2795 && r <= 0x2797, r == 0x27B0,
		r == 0x27BF, r >= 0x2B1B && r <= 0x2B1C, r == 0x2B50, r == 0x2B55,
		r >= 0x2E80 && r <= 0xA4CF && r != 0x303F,
		r >= 0xAC00 && r <= 0xD7A3,
		r >= 0xF900 && r <= 0xFAFF,
		r >= 0xFE30 && r <= 0xFE4F,
		r >= 0xFF00 && r <= 0xFF60,
		r >= 0xFFE0 && r <= 0xFFE6,
		r >= 0x1F000 && r <= 0x1FAFF,
		r >= 0x20000 && r <= 0x3FFFD:
		return 2
	}
	return 1
}

// StringWidth is the display width of s.
func StringWidth(s string) int {
	n := 0
	rs := []rune(s)
	for i, r := range rs {
		w := RuneWidth(r)
		if w == 1 && i+1 < len(rs) && rs[i+1] == 0xFE0F {
			w = 2
		}
		n += w
	}
	return n
}
