package app

import (
	"strconv"
	"strings"

	"github.com/Panchal-Dakshesh/aictl/internal/handoff"
	"github.com/Panchal-Dakshesh/aictl/internal/term"
)

func (a *App) key(k term.Key) {
	if a.cmdOn {
		a.cmdKey(k)
		return
	}
	switch a.mode {
	case modeHelp:
		a.mode = modeBrowse
		return
	case modeConfirm:
		if k.Code == term.KeyRune && (k.Rune == 'y' || k.Rune == 'Y') && a.confirm != nil {
			a.confirm()
		} else {
			a.flash("cancelled", false)
		}
		a.confirm = nil
		a.mode = modeMemory
		return
	case modePick:
		a.pickKey(k)
		return
	case modeView:
		a.viewKey(k)
		return
	case modeMemory:
		a.memKey(k)
		return
	}
	a.browseKey(k)
}

func (a *App) browseKey(k term.Key) {
	h := a.listHeight()
	switch k.String() {
	case "q", "C-c":
		a.quit = true
	case "j", "down":
		a.move(1)
	case "k", "up":
		a.move(-1)
	case "C-d", "pgdn":
		a.move(h / 2)
	case "C-u", "pgup":
		a.move(-h / 2)
	case "g", "home":
		a.move(-1 << 30)
	case "G", "end":
		a.move(1 << 30)
	case "h", "left":
		if a.focus > 0 {
			a.focus--
		}
	case "l", "right", "tab":
		if a.focus < 1 {
			a.focus++
		} else if a.current() != nil {
			a.openView()
		}
	case "enter":
		if a.focus == 0 {
			a.focus = 1
			return
		}
		a.resume()
	case "o", " ":
		a.openView()
	case "y":
		a.copyResume()
	case "s":
		if a.current() != nil {
			a.mode, a.pick = modePick, 0
		}
	case "a":
		a.attach(false)
	case "A":
		a.attach(true)
	case "m":
		a.openMemory()
	case "r":
		a.flash("re-indexing…", false)
		go a.reindex()
	case "/":
		a.openCmd('/')
	case ":":
		a.openCmd(':')
	case "?":
		a.mode = modeHelp
	case "esc":
		if a.query != "" || a.grep != nil {
			a.query, a.grep, a.grepQ = "", nil, ""
			a.refilter()
			a.flash("filters cleared", false)
		}
	}
}

func (a *App) move(d int) {
	if a.focus == 0 {
		a.fsel = clamp(a.fsel+d, 0, len(a.facets)-1)
		a.sel, a.off = 0, 0
		a.refilter()
		return
	}
	a.sel = clamp(a.sel+d, 0, len(a.list)-1)
}

func clamp(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func (a *App) openView() {
	if a.current() == nil {
		return
	}
	a.mode, a.vOff = modeView, 0
	a.buildView()
}

func (a *App) viewKey(k term.Key) {
	h := a.scr.H - 3
	max := len(a.view) - h
	switch k.String() {
	case "q", "esc", "h", "left":
		a.mode = modeBrowse
	case "j", "down":
		a.vOff++
	case "k", "up":
		a.vOff--
	case "C-d", "pgdn", " ":
		a.vOff += h / 2
	case "C-u", "pgup":
		a.vOff -= h / 2
	case "g", "home":
		a.vOff = 0
	case "G", "end":
		a.vOff = max
	case "/":
		a.openCmd('?') // in-transcript search
	case "n":
		a.findNext(1)
	case "N":
		a.findNext(-1)
	case "enter":
		a.resume()
	case "y":
		a.copyResume()
	case "s":
		a.mode, a.pick = modePick, 0
	case "a":
		a.attach(false)
	}
	a.vOff = clamp(a.vOff, 0, max)
}

func (a *App) findNext(dir int) {
	if a.vSearch == "" {
		return
	}
	q := strings.ToLower(a.vSearch)
	for i := 1; i <= len(a.view); i++ {
		j := (a.vOff + i*dir + len(a.view)*2) % len(a.view)
		if strings.Contains(strings.ToLower(a.view[j].text), q) {
			a.vOff = j
			return
		}
	}
	a.flash("not found: "+a.vSearch, true)
}

func (a *App) pickKey(k term.Key) {
	switch k.String() {
	case "q", "esc":
		a.mode = modeBrowse
	case "j", "down":
		a.pick = clamp(a.pick+1, 0, len(handoff.Targets)-1)
	case "k", "up":
		a.pick = clamp(a.pick-1, 0, len(handoff.Targets)-1)
	case "f", "c", "tab":
		a.pickFull = !a.pickFull
	case "enter", "l":
		a.mode = modeBrowse
		a.handoffTo(handoff.Targets[a.pick])
	}
}

func (a *App) memKey(k term.Key) {
	switch k.String() {
	case "q", "esc":
		if a.mDiff {
			a.mDiff = false
			return
		}
		a.mode = modeBrowse
	case "j", "down":
		a.mSel = clamp(a.mSel+1, 0, len(a.mem)-1)
		a.mOff = 0
	case "k", "up":
		a.mSel = clamp(a.mSel-1, 0, len(a.mem)-1)
		a.mOff = 0
	case "C-d", "pgdn", " ":
		a.mOff += 10
	case "C-u", "pgup":
		a.mOff = clamp(a.mOff-10, 0, 1<<30)
	case "e", "enter":
		a.memEdit()
	case "d":
		if a.mMark == a.mSel {
			a.mMark, a.mDiff = -1, false
			a.flash("mark cleared", false)
		} else if a.mMark < 0 {
			a.mMark = a.mSel
			a.flash("marked. move to another file and press d to diff, S to sync", false)
		} else {
			a.mDiff, a.mOff = true, 0
		}
	case "S":
		a.memSync()
	}
}

// ------------------------------------------------------------------ command line

func (a *App) openCmd(kind byte) {
	a.cmdOn, a.cmdKind, a.cmdBuf = true, kind, nil
	if kind == '/' {
		a.cmdBuf = []rune(a.query)
	}
}

func (a *App) cmdKey(k term.Key) {
	switch k.Code {
	case term.KeyEsc:
		a.cmdOn = false
		if a.cmdKind == '/' {
			a.query = ""
			a.refilter()
		}
		return
	case term.KeyEnter:
		a.cmdOn = false
		a.runCmd(a.cmdKind, string(a.cmdBuf))
		return
	case term.KeyBackspace:
		if len(a.cmdBuf) > 0 {
			a.cmdBuf = a.cmdBuf[:len(a.cmdBuf)-1]
		} else {
			a.cmdOn = false
		}
	case term.KeyRune:
		a.cmdBuf = append(a.cmdBuf, k.Rune)
	case term.KeyPaste:
		a.cmdBuf = append(a.cmdBuf, []rune(strings.ReplaceAll(k.Paste, "\n", " "))...)
	case term.KeyCtrl:
		if k.Rune == 'u' {
			a.cmdBuf = nil
		}
		if k.Rune == 'c' {
			a.cmdOn = false
			return
		}
	}
	if a.cmdKind == '/' { // live filter
		a.query = string(a.cmdBuf)
		a.sel, a.off = 0, 0
		a.refilter()
	}
}

func (a *App) runCmd(kind byte, s string) {
	switch kind {
	case '/':
		a.query = s
		a.refilter()
		return
	case '?':
		a.vSearch = s
		a.findNext(1)
		return
	}
	f := strings.Fields(s)
	if len(f) == 0 {
		return
	}
	arg := strings.TrimSpace(strings.TrimPrefix(s, f[0]))
	switch f[0] {
	case "q", "q!", "qa", "quit", "wq", "x":
		a.quit = true
	case "grep", "g", "find":
		a.grepAll(arg)
	case "memory", "mem":
		a.openMemory()
	case "send", "handoff":
		if t, ok := handoff.FindTarget(arg); ok {
			a.handoffTo(t)
		} else {
			a.mode, a.pick = modePick, 0
		}
	case "attach":
		a.attach(arg == "full")
	case "resume":
		a.resume()
	case "reindex":
		go a.reindex()
	case "tool":
		a.query = "tool:" + arg
		a.refilter()
	case "help", "h":
		a.mode = modeHelp
	case "checkhealth", "doctor":
		a.flash(a.health(), false)
	case "set":
		if strings.Contains(arg, "background=dark") {
			a.flash("it follows your terminal. go set it there.", false)
			return
		}
		a.flash("nothing to set", true)
	default:
		a.flash("E492: Not an editor command: "+s, true)
	}
}

func (a *App) health() string {
	var parts []string
	for _, f := range a.facets {
		if f.tool != "" {
			parts = append(parts, f.tool+" "+itoa(f.count))
		}
	}
	return "ok: " + strings.Join(parts, ", ") + "  ·  " + itoa(a.stats.Files) + " files, " + itoa(len(a.stats.Errors)) + " errors"
}

func itoa(n int) string { return strconv.Itoa(n) }
