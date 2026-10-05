// Package app is the aictl TUI: a session browser across every AI tool, with
// resume, handoff, attach and a memory-file manager. Vim keys throughout.
package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/Panchal-Dakshesh/aictl/internal/handoff"
	"github.com/Panchal-Dakshesh/aictl/internal/memory"
	"github.com/Panchal-Dakshesh/aictl/internal/session"
	"github.com/Panchal-Dakshesh/aictl/internal/term"
)

type mode int

const (
	modeBrowse mode = iota
	modeView
	modeMemory
	modePick
	modeHelp
	modeConfirm
)

type facet struct {
	label string
	tool  string
	proj  string
	count int
}

type App struct {
	t      *term.Terminal
	in     *term.Input
	scr    *term.Screen
	tasks  chan func()
	quit   bool
	execAt *execReq

	sources []session.Source
	all     []*session.Session
	owner   map[string]session.Source
	stats   session.Stats

	facets []facet
	fsel   int
	list   []*session.Session
	sel    int
	off    int
	focus  int // 0 facets, 1 list, 2 preview
	query  string
	grep   map[string]bool // content-search hits, nil when inactive
	grepQ  string

	msgs    map[string][]session.Message
	loading map[string]bool

	mode    mode
	prevOff int
	view    []line
	vOff    int
	vSearch string

	mem     []memory.File
	mSel    int
	mMark   int
	mDiff   bool
	mOff    int
	mProj   string
	confirm func()
	cPrompt string

	pick     int
	pickFull bool

	cmdOn   bool
	cmdKind byte
	cmdBuf  []rune
	msg     string
	msgErr  bool
	msgAt   time.Time
}

type execReq struct {
	dir  string
	argv []string
	name string
	note string
}

// launch runs a session without leaving aictl. Inside tmux it opens a new
// window per session, so aictl stays up as the dashboard and several
// sessions can run side by side. Outside tmux the tool takes over this
// terminal and you land back in aictl when it exits.
func (a *App) launch(r *execReq) {
	dir := r.dir
	if dir != "" {
		if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
			dir = ""
		}
	}
	if InTmux() {
		args := []string{"new-window", "-n", session.Truncate(r.name, 24)}
		if dir != "" {
			args = append(args, "-c", dir)
		}
		args = append(args, r.argv...)
		if out, err := exec.Command("tmux", args...).CombinedOutput(); err != nil {
			a.flash("tmux: "+strings.TrimSpace(string(out)), true)
			return
		}
		a.flash("opened in a new tmux window. prefix + w to switch, aictl stays here", false)
		return
	}
	if err := a.suspend(dir, r.argv...); err != nil {
		a.flash(r.argv[0]+": "+err.Error(), true)
	} else {
		a.flash("back in aictl. "+r.argv[0]+" exited", false)
	}
	go a.reindex() // the session probably has new messages now
}

// InTmux reports whether aictl is running inside a tmux session.
func InTmux() bool { return os.Getenv("TMUX") != "" }

type line struct {
	text string
	st   term.Style
}

func New(sources []session.Source) *App {
	return &App{sources: sources, tasks: make(chan func(), 64), msgs: map[string][]session.Message{}, loading: map[string]bool{}, focus: 1, mMark: -1}
}

// Run takes over the terminal until the user quits or resumes a session.
func (a *App) Run() error {
	t, err := term.Open()
	if err != nil {
		return err
	}
	a.t = t
	if err := t.EnterRaw(); err != nil {
		return err
	}
	w, h := t.Size()
	a.scr = term.NewScreen(w, h)
	a.in = term.NewInput(os.Stdin)
	a.flash("indexing sessions…", false)
	a.draw()
	go a.reindex()

	for !a.quit && a.execAt == nil {
		select {
		case k := <-a.in.Keys:
			a.key(k)
		case <-t.Resize:
			w, h := t.Size()
			a.scr.Resize(w, h)
		case f := <-a.tasks:
			f()
		case <-time.After(time.Second):
		}
		a.draw()
	}
	a.in.Pause()
	t.Restore()
	if a.execAt != nil {
		return a.doExec(a.execAt)
	}
	return nil
}

func (a *App) reindex() {
	ix := session.OpenIndex()
	all, owner, st := ix.Build(a.sources)
	ix.Save()
	a.tasks <- func() {
		var cur string
		if s := a.current(); s != nil {
			cur = session.Key(s)
		}
		a.all, a.owner, a.stats = all, owner, st
		a.buildFacets()
		a.refilter()
		for i, s := range a.list {
			if session.Key(s) == cur {
				a.sel = i
			}
		}
		a.flash(fmt.Sprintf("%d sessions from %d tools  (%d parsed, %d cached)", len(all), a.toolCount(), st.Parsed, st.Cached), false)
	}
}

func (a *App) toolCount() int {
	n := 0
	for _, f := range a.facets {
		if f.tool != "" {
			n++
		}
	}
	return n
}

func (a *App) buildFacets() {
	tools := map[string]int{}
	projs := map[string]int{}
	for _, s := range a.all {
		tools[s.Tool]++
		if s.Project != "" {
			projs[s.Project]++
		}
	}
	a.facets = []facet{{label: "all", count: len(a.all)}}
	for _, src := range a.sources {
		if n := tools[src.Name()]; n > 0 {
			a.facets = append(a.facets, facet{label: src.Name(), tool: src.Name(), count: n})
		}
	}
	type kv struct {
		k string
		v int
	}
	var ps []kv
	for k, v := range projs {
		ps = append(ps, kv{k, v})
	}
	sort.Slice(ps, func(i, j int) bool { return ps[i].v > ps[j].v || ps[i].v == ps[j].v && ps[i].k < ps[j].k })
	for i, p := range ps {
		if i >= 12 {
			break
		}
		a.facets = append(a.facets, facet{label: filepath.Base(p.k), proj: p.k, count: p.v})
	}
	if a.fsel >= len(a.facets) {
		a.fsel = 0
	}
}

// refilter applies the facet, the / query and any :grep result.
func (a *App) refilter() {
	f := facet{}
	if a.fsel < len(a.facets) {
		f = a.facets[a.fsel]
	}
	tool, proj, words := parseQuery(a.query)
	a.list = a.list[:0]
	for _, s := range a.all {
		if f.tool != "" && s.Tool != f.tool || f.proj != "" && s.Project != f.proj {
			continue
		}
		if tool != "" && !strings.HasPrefix(s.Tool, tool) {
			continue
		}
		if proj != "" && !strings.Contains(strings.ToLower(s.Project), proj) {
			continue
		}
		if a.grep != nil && !a.grep[session.Key(s)] {
			continue
		}
		hay := strings.ToLower(s.Title + " " + s.Project + " " + s.Tool + " " + s.Branch)
		ok := true
		for _, w := range words {
			if !strings.Contains(hay, w) {
				ok = false
				break
			}
		}
		if ok {
			a.list = append(a.list, s)
		}
	}
	if a.sel >= len(a.list) {
		a.sel = len(a.list) - 1
	}
	if a.sel < 0 {
		a.sel = 0
	}
}

func parseQuery(q string) (tool, proj string, words []string) {
	for _, w := range strings.Fields(strings.ToLower(q)) {
		switch {
		case strings.HasPrefix(w, "tool:"):
			tool = w[5:]
		case strings.HasPrefix(w, "project:"):
			proj = w[8:]
		case strings.HasPrefix(w, "p:"):
			proj = w[2:]
		default:
			words = append(words, w)
		}
	}
	return
}

func (a *App) current() *session.Session {
	if a.sel >= 0 && a.sel < len(a.list) {
		return a.list[a.sel]
	}
	return nil
}

func (a *App) src(s *session.Session) session.Source { return a.owner[session.Key(s)] }

// messages returns the transcript if loaded, starting an async load if not.
func (a *App) messages(s *session.Session) ([]session.Message, bool) {
	k := session.Key(s)
	if m, ok := a.msgs[k]; ok {
		return m, true
	}
	if !a.loading[k] {
		a.loading[k] = true
		src := a.src(s)
		go func() {
			m, err := src.Load(s)
			a.tasks <- func() {
				delete(a.loading, k)
				if err != nil {
					a.flash("load failed: "+err.Error(), true)
				}
				a.msgs[k] = m
				if a.mode == modeView && a.current() == s {
					a.buildView()
				}
			}
		}()
	}
	return nil, false
}

// loadSync is used by actions that need the whole transcript right now.
func (a *App) loadSync(s *session.Session) []session.Message {
	if m, ok := a.msgs[session.Key(s)]; ok {
		return m
	}
	m, _ := a.src(s).Load(s)
	a.msgs[session.Key(s)] = m
	return m
}

func (a *App) flash(m string, isErr bool) { a.msg, a.msgErr, a.msgAt = m, isErr, time.Now() }

// ------------------------------------------------------------------ actions

func (a *App) resume() {
	s := a.current()
	if s == nil {
		return
	}
	dir, argv, ok := a.src(s).Resume(s)
	if !ok {
		a.flash(s.Tool+" can't reopen a chat from outside its app. press s to hand it off to another tool", true)
		return
	}
	if _, err := exec.LookPath(argv[0]); err != nil {
		handoff.Copy(handoff.Quote(dir, argv))
		a.flash(argv[0]+" isn't on PATH. resume command copied to clipboard", true)
		return
	}
	a.launch(&execReq{dir: dir, argv: argv, name: s.Tool + ":" + s.Title})
}

func (a *App) copyResume() {
	s := a.current()
	if s == nil {
		return
	}
	dir, argv, ok := a.src(s).Resume(s)
	if !ok {
		a.flash(s.Tool+" has no resume command. try s (handoff) or a (attach)", true)
		return
	}
	cmd := handoff.Quote(dir, argv)
	if err := handoff.Copy(cmd); err != nil {
		a.flash("copy failed: "+err.Error(), true)
		return
	}
	a.flash("copied: "+cmd, false)
}

// attach writes a compact brief and copies a one-liner pointing at it, for
// pasting into a session that's already running.
func (a *App) attach(full bool) {
	s := a.current()
	if s == nil {
		return
	}
	msgs := a.loadSync(s)
	mode := handoff.Compact
	if full {
		mode = handoff.Full
	}
	md := handoff.Context(s, msgs, mode)
	p, err := handoff.Write(s, md)
	if err != nil {
		a.flash(err.Error(), true)
		return
	}
	line := fmt.Sprintf("Read %s for context from my earlier %s session (%q).", p, s.Tool, s.Title)
	handoff.Copy(line)
	a.flash("attach line copied → paste it into your running session", false)
}

func (a *App) handoffTo(t handoff.Target) {
	s := a.current()
	if s == nil {
		return
	}
	msgs := a.loadSync(s)
	mode := handoff.Compact
	if a.pickFull {
		mode = handoff.Full
	}
	p, err := handoff.Write(s, handoff.Context(s, msgs, mode))
	if err != nil {
		a.flash(err.Error(), true)
		return
	}
	prompt := handoff.Prompt(s.Tool, p)
	if !t.Available() {
		handoff.Copy(prompt)
		a.flash(t.Bin+" isn't installed. brief saved and prompt copied to clipboard", true)
		return
	}
	a.launch(&execReq{dir: s.Project, argv: append([]string{t.Bin}, t.Args(prompt)...), name: t.Name + "←" + s.Tool, note: "handoff " + s.Tool + " → " + t.Name})
}

func (a *App) doExec(r *execReq) error {
	if r.dir != "" {
		if err := os.Chdir(r.dir); err != nil {
			fmt.Fprintf(os.Stderr, "aictl: %s no longer exists, starting in %s\n", r.dir, mustWd())
		}
	}
	bin, err := exec.LookPath(r.argv[0])
	if err != nil {
		return err
	}
	if r.note != "" {
		fmt.Fprintln(os.Stderr, "aictl: "+r.note)
	}
	return syscall.Exec(bin, r.argv, os.Environ())
}

func mustWd() string { d, _ := os.Getwd(); return d }

// suspend hands the terminal to another program (an editor) and takes it back.
func (a *App) suspend(dir string, argv ...string) error {
	a.in.Pause()
	a.t.Restore()
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	err := cmd.Run()
	a.t.EnterRaw()
	a.in.Resume()
	w, h := a.t.Size()
	a.scr.Resize(w, h)
	return err
}

func editor() []string {
	for _, v := range []string{os.Getenv("VISUAL"), os.Getenv("EDITOR")} {
		if v != "" {
			return strings.Fields(v)
		}
	}
	for _, e := range []string{"nvim", "vim", "vi", "nano"} {
		if _, err := exec.LookPath(e); err == nil {
			return []string{e}
		}
	}
	return []string{"vi"}
}

// grepAll searches every transcript in the background.
func (a *App) grepAll(q string) {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		a.grep, a.grepQ = nil, ""
		a.refilter()
		return
	}
	a.flash("searching every transcript for "+q+"…", false)
	todo := append([]*session.Session(nil), a.all...)
	cached := map[string][]session.Message{}
	for k, v := range a.msgs {
		cached[k] = v
	}
	go func() {
		hits := map[string]bool{}
		loaded := map[string][]session.Message{}
		for _, s := range todo {
			k := session.Key(s)
			m, ok := cached[k]
			if !ok {
				m, _ = a.owner[k].Load(s)
				loaded[k] = m
			}
			for _, x := range m {
				if strings.Contains(strings.ToLower(x.Text), q) {
					hits[k] = true
					break
				}
			}
		}
		a.tasks <- func() {
			for k, v := range loaded {
				a.msgs[k] = v
			}
			a.grep, a.grepQ = hits, q
			a.sel, a.off = 0, 0
			a.refilter()
			a.flash(fmt.Sprintf("%d sessions mention %q  (:grep with no text clears)", len(hits), q), false)
		}
	}()
}

// ------------------------------------------------------------------ memory

func (a *App) openMemory() {
	proj := ""
	if s := a.current(); s != nil {
		proj = s.Project
	}
	a.mProj = proj
	a.mem = memory.Find(proj)
	a.mSel, a.mMark, a.mDiff, a.mOff = 0, -1, false, 0
	a.mode = modeMemory
}

func (a *App) memEdit() {
	if len(a.mem) == 0 {
		return
	}
	f := a.mem[a.mSel]
	os.MkdirAll(filepath.Dir(f.Path), 0o755)
	ed := editor()
	if err := a.suspend(filepath.Dir(f.Path), append(ed, f.Path)...); err != nil {
		a.flash("editor: "+err.Error(), true)
	}
	a.mem = memory.Find(a.mProj)
}

func (a *App) memSync() {
	if a.mMark < 0 || a.mMark == a.mSel {
		a.flash("mark a source file with d, move to the target, then press S", true)
		return
	}
	src, dst := a.mem[a.mMark], a.mem[a.mSel]
	if !src.Exists {
		a.flash("source file doesn't exist", true)
		return
	}
	a.cPrompt = fmt.Sprintf("overwrite %s with %s? (backup kept)  y/n", session.ShortPath(dst.Path), filepath.Base(src.Path))
	a.confirm = func() {
		bak, err := memory.Sync(src, dst)
		if err != nil {
			a.flash(err.Error(), true)
			return
		}
		a.mem = memory.Find(a.mProj)
		if bak != "" {
			a.flash("synced. previous version saved to "+session.ShortPath(bak), false)
		} else {
			a.flash("created "+session.ShortPath(dst.Path), false)
		}
	}
	a.mode = modeConfirm
}
