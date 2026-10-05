package app

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/Panchal-Dakshesh/aictl/internal/handoff"
	"github.com/Panchal-Dakshesh/aictl/internal/memory"
	"github.com/Panchal-Dakshesh/aictl/internal/session"
	"github.com/Panchal-Dakshesh/aictl/internal/term"
	"github.com/Panchal-Dakshesh/aictl/internal/ui"
)

func (a *App) draw() {
	s := a.scr
	s.Clear()
	switch a.mode {
	case modeView:
		a.drawView()
	case modeMemory, modeConfirm:
		a.drawMemory()
	default:
		a.drawBrowse()
	}
	if a.mode == modePick {
		a.drawPick()
	}
	if a.mode == modeHelp {
		a.drawHelp()
	}
	a.drawStatus()
	s.Flush(a.t)
}

func (a *App) listHeight() int { return a.scr.H - 4 }

// ------------------------------------------------------------------ browse

func (a *App) drawBrowse() {
	s := a.scr
	W, H := s.W, s.H
	top, h := 0, H-2
	leftW := 0
	if W >= 100 {
		leftW = 24
	}
	prevW := 0
	if W >= 80 {
		prevW = (W - leftW) * 45 / 100
	}
	listW := W - leftW - prevW

	if leftW > 0 {
		x, y, w, ih := ui.Box(s, 0, top, leftW, h, "sources", a.focus == 0)
		for i, f := range a.facets {
			row := y + i
			if i > 0 && f.proj != "" && a.facets[i-1].proj == "" {
				s.Text(x+1, row, "projects", ui.Dim, w-1)
				y++
				row++
			}
			if row >= y+ih {
				break
			}
			st := ui.Plain
			if i == a.fsel {
				st = ui.Selected
				if a.focus != 0 {
					st = term.Style{Bg: ui.Celeste, Fg: ui.Ink, Dim: true}
				}
				s.Fill(x, row, w, 1, ' ', st)
			}
			dot := ' '
			dst := st
			if f.tool != "" {
				dot = '●'
				dst.Fg = ui.ToolColor(f.tool)
			}
			s.Set(x+1, row, dot, dst)
			cnt := itoa(f.count)
			s.Text(x+3, row, f.label, st, w-5-len(cnt))
			cst := st
			if i != a.fsel {
				cst = ui.Dim
			}
			s.Text(x+w-1-len(cnt), row, cnt, cst, len(cnt))
		}
	}

	title := fmt.Sprintf("sessions %d", len(a.list))
	if a.query != "" {
		title += "  /" + a.query
	}
	if a.grepQ != "" {
		title += "  grep:" + a.grepQ
	}
	x, y, w, ih := ui.Box(s, leftW, top, listW, h, title, a.focus == 1)
	if a.sel < a.off {
		a.off = a.sel
	}
	if a.sel >= a.off+ih {
		a.off = a.sel - ih + 1
	}
	if len(a.all) == 0 {
		s.Text(x+2, y+1, "no sessions yet. indexing, or nothing found in:", ui.Dim, w-3)
		for i, src := range a.sources {
			s.Text(x+4, y+3+i, session.ShortPath(src.Roots()[0]), ui.Dim, w-5)
		}
	}
	toolW := 12
	projW := 0
	if w > 70 {
		projW = 16
	}
	for i := 0; i < ih && a.off+i < len(a.list); i++ {
		ss := a.list[a.off+i]
		row := y + i
		sel := a.off+i == a.sel
		st := ui.Plain
		if sel {
			st = ui.Selected
			s.Fill(x, row, w, 1, ' ', st)
		}
		dim := ui.Dim
		if sel {
			dim = st
		}
		cx := x + 1
		cx += s.Text(cx, row, ui.Pad(session.Ago(ss.Updated), 5), dim, 5)
		ts := st
		ts.Fg = ui.ToolColor(ss.Tool)
		if sel {
			ts.Bold = true
		}
		cx += s.Text(cx, row, ui.Pad(ss.Tool, toolW), ts, toolW)
		titleW := w - (cx - x) - projW - 1
		cx += s.Text(cx, row, ui.Pad(ss.Title, titleW), st, titleW)
		if projW > 0 {
			s.Text(cx+1, row, ui.Pad(filepath.Base(ss.Project), projW-1), dim, projW-1)
		}
	}

	if prevW > 0 {
		a.drawPreview(leftW+listW, top, prevW, h)
	}
}

func (a *App) drawPreview(px, py, pw, ph int) {
	s := a.scr
	x, y, w, ih := ui.Box(s, px, py, pw, ph, "preview", a.focus == 2)
	ss := a.current()
	if ss == nil {
		return
	}
	var lines []line
	add := func(t string, st term.Style) {
		for _, l := range ui.Wrap(t, w-2) {
			lines = append(lines, line{l, st})
		}
	}
	add(ss.Title, ui.Bold)
	meta := ss.Tool
	if ss.Project != "" {
		meta += " · " + session.ShortPath(ss.Project)
	}
	if ss.Branch != "" {
		meta += " · ⎇ " + ss.Branch
	}
	add(meta, term.Style{Fg: ui.ToolColor(ss.Tool)})
	info := fmt.Sprintf("%d msgs · %s", ss.Messages, ss.Updated.Format("Jan 2 15:04"))
	if ss.Model != "" {
		info += " · " + ss.Model
	}
	add(info, ui.Dim)
	if dir, argv, ok := a.src(ss).Resume(ss); ok {
		_ = dir
		show := append([]string{}, argv...)
		if n := len(show); n > 0 && len(show[n-1]) > 12 {
			show[n-1] = show[n-1][:8] + "…"
		}
		add("↵ "+strings.Join(show, " "), ui.Dim)
	} else {
		add("↵ not resumable outside "+ss.Tool+" · s to hand off", ui.Dim)
	}
	lines = append(lines, line{"", ui.Plain})

	msgs, ok := a.messages(ss)
	if !ok {
		lines = append(lines, line{"loading…", ui.Dim})
	}
	for _, m := range msgs {
		if len(lines) > ih+4 {
			break
		}
		lines = append(lines, msgLines(m, w-2)...)
	}
	for i := 0; i < ih && i < len(lines); i++ {
		s.Text(x+1, y+i, lines[i].text, lines[i].st, w-2)
	}
}

func msgLines(m session.Message, w int) []line {
	var out []line
	switch m.Role {
	case session.Tool:
		return []line{{"  ⚙ " + m.Name, ui.Dim}}
	case session.User:
		for i, l := range ui.Wrap(strings.TrimSpace(m.Text), w-2) {
			p := "  "
			if i == 0 {
				p = "› "
			}
			out = append(out, line{p + l, term.Style{Fg: ui.Accent, Bold: i == 0}})
		}
	default:
		for _, l := range ui.Wrap(strings.TrimSpace(m.Text), w-2) {
			out = append(out, line{"  " + l, ui.Plain})
		}
	}
	return append(out, line{"", ui.Plain})
}

// ------------------------------------------------------------------ transcript view

func (a *App) buildView() {
	ss := a.current()
	if ss == nil {
		return
	}
	w := a.scr.W - 4
	if w > 110 {
		w = 110
	}
	a.view = nil
	msgs, ok := a.messages(ss)
	if !ok {
		a.view = []line{{"loading…", ui.Dim}}
		return
	}
	for _, m := range msgs {
		if m.Role != session.Tool && !m.Time.IsZero() {
			who := "you"
			if m.Role == session.Assistant {
				who = ss.Tool
			}
			a.view = append(a.view, line{who + " · " + m.Time.Local().Format("Jan 2 15:04"), ui.Dim})
		}
		a.view = append(a.view, msgLines(m, w)...)
	}
}

func (a *App) drawView() {
	s := a.scr
	ss := a.current()
	if ss == nil {
		return
	}
	if len(a.view) == 1 && a.view[0].text == "loading…" {
		a.buildView()
	}
	x, y, w, ih := ui.Box(s, 0, 0, s.W, s.H-2, ss.Tool+" · "+ss.Title, true)
	cw := w - 2
	if cw > 110 {
		cw = 110
	}
	q := strings.ToLower(a.vSearch)
	for i := 0; i < ih && a.vOff+i < len(a.view); i++ {
		l := a.view[a.vOff+i]
		st := l.st
		if q != "" && strings.Contains(strings.ToLower(l.text), q) {
			st.Bg = ui.Celeste
			st.Fg = ui.Ink
		}
		s.Text(x+1, y+i, l.text, st, cw)
	}
	if n := len(a.view); n > ih {
		pct := 100 * (a.vOff + ih) / n
		if pct > 100 {
			pct = 100
		}
		lbl := fmt.Sprintf(" %d%% ", pct)
		s.Text(x+w-len(lbl)-1, y+ih, lbl, ui.Dim, len(lbl))
	}
}

// ------------------------------------------------------------------ memory

func (a *App) drawMemory() {
	s := a.scr
	h := s.H - 2
	lw := s.W * 38 / 100
	if lw < 30 {
		lw = 30
	}
	title := "memory · global"
	if a.mProj != "" {
		title = "memory · " + filepath.Base(a.mProj)
	}
	x, y, w, ih := ui.Box(s, 0, 0, lw, h, title, true)
	row := y
	lastScope := ""
	for i, f := range a.mem {
		if f.Scope != lastScope {
			if row >= y+ih {
				break
			}
			s.Text(x+1, row, f.Scope, ui.Dim, w-1)
			row++
			lastScope = f.Scope
		}
		if row >= y+ih {
			break
		}
		st := ui.Plain
		if !f.Exists {
			st = ui.Dim
		}
		if i == a.mSel {
			st = ui.Selected
			s.Fill(x, row, w, 1, ' ', st)
		}
		mark := "  "
		if i == a.mMark {
			mark = "◆ "
		}
		ts := st
		ts.Fg = ui.ToolColor(f.Tool)
		s.Text(x+1, row, mark, st, 2)
		s.Text(x+3, row, ui.Pad(f.Tool, 12), ts, 12)
		name := relTo(f.Path, a.mProj)
		if !f.Exists {
			name += "  +"
		}
		s.Text(x+15, row, name, st, w-16)
		row++
	}
	s.Text(x+1, y+ih-1, "e edit · d mark/diff · S sync marked → here", ui.Dim, w-2)

	if len(a.mem) == 0 {
		return
	}
	cur := a.mem[a.mSel]
	rt := session.ShortPath(cur.Path)
	var lines []line
	if a.mDiff && a.mMark >= 0 {
		mk := a.mem[a.mMark]
		rt = "diff  " + filepath.Base(mk.Path) + " → " + filepath.Base(cur.Path)
		for _, d := range memory.Diff(memory.Read(mk), memory.Read(cur)) {
			st := ui.Plain
			switch d.Op {
			case '-':
				st = term.Style{Fg: ui.Red}
			case '+':
				st = term.Style{Fg: ui.Green}
			default:
				st = ui.Dim
			}
			lines = append(lines, line{string(d.Op) + " " + d.Text, st})
		}
	} else if !cur.Exists {
		lines = []line{{"doesn't exist yet. press e to create it,", ui.Dim}, {"or mark another file with d and press S here to copy it over.", ui.Dim}}
	} else {
		for _, l := range strings.Split(memory.Read(cur), "\n") {
			st := ui.Plain
			if strings.HasPrefix(l, "#") {
				st = term.Style{Bold: true, Fg: ui.Accent}
			}
			lines = append(lines, line{l, st})
		}
	}
	rx, ry, rw, rh := ui.Box(s, lw, 0, s.W-lw, h, rt, false)
	a.mOff = clamp(a.mOff, 0, len(lines)-1)
	for i := 0; i < rh && a.mOff+i < len(lines); i++ {
		s.Text(rx+1, ry+i, lines[a.mOff+i].text, lines[a.mOff+i].st, rw-2)
	}
}

func relTo(p, base string) string {
	if base != "" {
		if r, err := filepath.Rel(base, p); err == nil && !strings.HasPrefix(r, "..") {
			return r
		}
	}
	return session.ShortPath(p)
}

// ------------------------------------------------------------------ overlays

func (a *App) drawPick() {
	s := a.scr
	w, h := 64, len(handoff.Targets)+7
	x, y := (s.W-w)/2, (s.H-h)/2
	s.Fill(x, y, w, h, ' ', ui.Plain)
	ix, iy, iw, _ := ui.Box(s, x, y, w, h, "hand off to", true)
	ss := a.current()
	if ss != nil {
		s.Text(ix+1, iy, "from "+ss.Tool+": "+ss.Title, ui.Dim, iw-2)
	}
	for i, t := range handoff.Targets {
		row := iy + 2 + i
		st := ui.Plain
		if i == a.pick {
			st = ui.Selected
			s.Fill(ix, row, iw, 1, ' ', st)
		}
		avail, ast := "✓ installed", term.Style{Fg: ui.Green}
		if !t.Available() {
			avail, ast = "✗ not found, copies prompt", ui.Dim
		}
		if i == a.pick {
			ast.Bg, ast.Fg = ui.Celeste, ui.Ink
		}
		ts := st
		ts.Fg = ui.ToolColor(strings.TrimSuffix(t.Name, "-agent"))
		s.Text(ix+2, row, t.Name, ts, 14)
		s.Text(ix+18, row, avail, ast, iw-19)
	}
	m := "compact (first ask + last 16 msgs)"
	if a.pickFull {
		m = "full transcript"
	}
	s.Text(ix+1, iy+3+len(handoff.Targets), "context: "+m+"   tab to switch", ui.Dim, iw-2)
}

var helpText = []string{
	"aictl: every AI coding session, one place",
	"",
	"move      j k ↑ ↓  ·  h l tab switch pane  ·  g G top/bottom  ·  C-d C-u page",
	"find      / text   filter as you type (tool:codex  p:zapo)",
	"          :grep x  search inside every transcript  ·  esc clears",
	"read      o space  open transcript (/ search, n N next)",
	"",
	"resume    ↵        reopen in its own tool; you return here when it exits",
	"          in tmux: each session opens in its own window, aictl stays put",
	"handoff   s        start another tool with this session as context",
	"attach    a / A    brief (compact / full) + copy a line for a running session",
	"copy      y        copy the resume command",
	"",
	"memory    m        CLAUDE.md, AGENTS.md, GEMINI.md, Cursor rules",
	"          e edit · d mark, d again to diff · S copy marked file here",
	"",
	"misc      r reindex · :checkhealth · :q quit",
}

func (a *App) drawHelp() {
	s := a.scr
	w, h := 86, len(helpText)+2
	if w > s.W {
		w = s.W
	}
	x, y := (s.W-w)/2, (s.H-h)/2
	s.Fill(x, y, w, h, ' ', ui.Plain)
	ix, iy, iw, _ := ui.Box(s, x, y, w, h, "help", true)
	for i, l := range helpText {
		st := ui.Plain
		if i == 0 {
			st = term.Style{Bold: true, Fg: ui.Accent}
		}
		s.Text(ix+1, iy+i, l, st, iw-2)
	}
}

// ------------------------------------------------------------------ statusline

func (a *App) drawStatus() {
	s := a.scr
	y := s.H - 2
	bar := term.Style{Bg: term.Hex("#eaeef2"), Fg: ui.Ink}
	s.Fill(0, y, s.W, 1, ' ', bar)
	name := map[mode]string{modeBrowse: "NORMAL", modeView: "VIEW", modeMemory: "MEMORY", modePick: "HANDOFF", modeHelp: "HELP", modeConfirm: "CONFIRM"}[a.mode]
	if a.cmdOn {
		name = "COMMAND"
	}
	ms := term.Style{Bg: ui.AccentInk, Fg: term.Hex("#ffffff"), Bold: true}
	if a.cmdOn || a.mode == modeConfirm {
		ms.Bg = ui.Purple
	}
	x := s.Text(0, y, " "+name+" ", ms, 12)
	x += s.Text(x, y, " aictl ", term.Style{Bg: bar.Bg, Fg: ui.Purple}, 8)
	if ss := a.current(); ss != nil {
		info := fmt.Sprintf(" %s · %s · %d msgs ", ss.Tool, session.ShortPath(ss.Project), ss.Messages)
		x += s.Text(x, y, info, bar, s.W-x-20)
	}
	pos := fmt.Sprintf(" %d/%d ", a.sel+1, len(a.list))
	s.Text(s.W-len(pos), y, pos, term.Style{Bg: ui.Celeste, Fg: ui.AccentInk}, len(pos))
	hint := " ? help "
	s.Text(s.W-len(pos)-len(hint), y, hint, term.Style{Bg: bar.Bg, Fg: ui.Muted}, len(hint))

	// command / message line
	y++
	switch {
	case a.cmdOn:
		k := string(a.cmdKind)
		if a.cmdKind == '?' {
			k = "/"
		}
		n := s.Text(0, y, k+string(a.cmdBuf), ui.Plain, s.W-1)
		s.Set(n, y, ' ', term.Style{Reverse: true})
	case a.mode == modeConfirm:
		s.Text(0, y, a.cPrompt, term.Style{Fg: ui.Purple, Bold: true}, s.W)
	case a.msg != "" && time.Since(a.msgAt) < 6*time.Second:
		st := ui.Dim
		if a.msgErr {
			st = term.Style{Fg: ui.Red}
		}
		s.Text(0, y, a.msg, st, s.W)
	}
}
