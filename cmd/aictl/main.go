// aictl: a control plane for your AI coding sessions.
//
//	aictl                       open the TUI
//	aictl list [--tool t] [--json] [-n N]
//	aictl show <id>             print a transcript
//	aictl resume <id>           reopen a session in its own tool
//	aictl send <id> --to <tool> [--full]   hand a session to another tool
//	aictl attach <id> [--full]  write a context brief, print a line to paste
//	aictl memory [dir]          list memory/instruction files
//	aictl doctor                where aictl looked and what it found
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"text/tabwriter"

	"github.com/Panchal-Dakshesh/aictl/internal/app"
	"github.com/Panchal-Dakshesh/aictl/internal/handoff"
	"github.com/Panchal-Dakshesh/aictl/internal/memory"
	"github.com/Panchal-Dakshesh/aictl/internal/session"
	"github.com/Panchal-Dakshesh/aictl/internal/sources"
	"github.com/Panchal-Dakshesh/aictl/internal/term"
)

var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "aictl:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	srcs := sources.All()
	if len(args) == 0 {
		if !term.IsTerminal() {
			return fmt.Errorf("not a terminal. try `aictl list`")
		}
		return app.New(srcs).Run()
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "list", "ls":
		return cmdList(srcs, rest)
	case "show", "cat":
		return withSession(srcs, rest, func(s *session.Session, src session.Source, _ *flag.FlagSet) error {
			msgs, err := src.Load(s)
			if err != nil {
				return err
			}
			fmt.Print(handoff.Context(s, msgs, handoff.Full))
			return nil
		})
	case "resume":
		return withSession(srcs, rest, func(s *session.Session, src session.Source, _ *flag.FlagSet) error {
			dir, argv, ok := src.Resume(s)
			if !ok {
				return fmt.Errorf("%s can't reopen a chat from outside its app; try `aictl send %s --to claude`", s.Tool, short(s.ID))
			}
			return execIn(dir, argv)
		})
	case "send", "handoff":
		return cmdSend(srcs, rest)
	case "attach":
		return withSession(srcs, rest, func(s *session.Session, src session.Source, fs *flag.FlagSet) error {
			msgs, err := src.Load(s)
			if err != nil {
				return err
			}
			mode := handoff.Compact
			if fs.Lookup("full").Value.String() == "true" {
				mode = handoff.Full
			}
			p, err := handoff.Write(s, handoff.Context(s, msgs, mode))
			if err != nil {
				return err
			}
			line := fmt.Sprintf("Read %s for context from my earlier %s session (%q).", p, s.Tool, s.Title)
			handoff.Copy(line)
			fmt.Println(line)
			return nil
		})
	case "memory", "mem":
		dir := ""
		if len(rest) > 0 {
			dir, _ = filepath.Abs(rest[0])
		} else {
			dir, _ = os.Getwd()
		}
		for _, f := range memory.Find(dir) {
			state := "missing"
			if f.Exists {
				state = fmt.Sprintf("%d bytes", f.Size)
			}
			fmt.Printf("%-8s %-12s %-12s %s\n", f.Scope, f.Tool, state, session.ShortPath(f.Path))
		}
		return nil
	case "doctor", "checkhealth":
		return cmdDoctor(srcs)
	case "version", "--version", "-v":
		fmt.Println("aictl", version)
		return nil
	case "help", "-h", "--help":
		fmt.Print(usage)
		return nil
	}
	return fmt.Errorf("unknown command %q\n\n%s", cmd, usage)
}

const usage = `aictl: one control plane for Claude Code, Codex, Gemini, Antigravity and Cursor sessions

  aictl                                  open the TUI
  aictl list [--tool t] [-n 20] [--json]  list sessions, newest first
  aictl show <id>                        print a transcript as Markdown
  aictl resume <id>                      reopen a session in its own tool
  aictl send <id> --to <tool> [--full]   start another tool with this session as context
  aictl attach <id> [--full]             write a brief + copy a line to paste into a running session
  aictl memory [dir]                     list CLAUDE.md / AGENTS.md / GEMINI.md / Cursor rules
  aictl doctor                           show where aictl looked and what it found

<id> can be any unique prefix.
`

func build(srcs []session.Source) ([]*session.Session, map[string]session.Source, session.Stats) {
	ix := session.OpenIndex()
	all, owner, st := ix.Build(srcs)
	ix.Save()
	return all, owner, st
}

func cmdList(srcs []session.Source, args []string) error {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	tool := fs.String("tool", "", "only this tool")
	n := fs.Int("n", 30, "how many")
	asJSON := fs.Bool("json", false, "JSON output")
	if err := fs.Parse(args); err != nil {
		return err
	}
	all, _, _ := build(srcs)
	var out []*session.Session
	for _, s := range all {
		if *tool == "" || s.Tool == *tool {
			out = append(out, s)
		}
	}
	if *n > 0 && len(out) > *n {
		out = out[:*n]
	}
	if *asJSON {
		e := json.NewEncoder(os.Stdout)
		e.SetIndent("", "  ")
		return e.Encode(out)
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "AGO\tTOOL\tID\tMSGS\tPROJECT\tTITLE")
	for _, s := range out {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\t%s\n", session.Ago(s.Updated), s.Tool, short(s.ID), s.Messages, session.ShortPath(s.Project), session.Truncate(s.Title, 60))
	}
	return tw.Flush()
}

func short(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// withSession resolves an id prefix (flags may come before or after it).
func withSession(srcs []session.Source, args []string, fn func(*session.Session, session.Source, *flag.FlagSet) error) error {
	fs := flag.NewFlagSet("", flag.ContinueOnError)
	fs.Bool("full", false, "full transcript")
	to := fs.String("to", "", "target tool")
	_ = to
	var pos []string
	for len(args) > 0 {
		if err := fs.Parse(args); err != nil {
			return err
		}
		args = fs.Args()
		if len(args) > 0 {
			pos = append(pos, args[0])
			args = args[1:]
		}
	}
	if len(pos) == 0 {
		return fmt.Errorf("which session? pass an id (see `aictl list`)")
	}
	all, owner, _ := build(srcs)
	var hits []*session.Session
	for _, s := range all {
		if strings.HasPrefix(s.ID, pos[0]) {
			hits = append(hits, s)
		}
	}
	switch len(hits) {
	case 0:
		return fmt.Errorf("no session matches %q", pos[0])
	case 1:
		return fn(hits[0], owner[session.Key(hits[0])], fs)
	}
	return fmt.Errorf("%q matches %d sessions; use a longer prefix", pos[0], len(hits))
}

func cmdSend(srcs []session.Source, args []string) error {
	return withSession(srcs, args, func(s *session.Session, src session.Source, fs *flag.FlagSet) error {
		to := fs.Lookup("to").Value.String()
		t, ok := handoff.FindTarget(to)
		if !ok {
			names := []string{}
			for _, t := range handoff.Targets {
				names = append(names, t.Name)
			}
			return fmt.Errorf("--to must be one of: %s", strings.Join(names, ", "))
		}
		msgs, err := src.Load(s)
		if err != nil {
			return err
		}
		mode := handoff.Compact
		if fs.Lookup("full").Value.String() == "true" {
			mode = handoff.Full
		}
		p, err := handoff.Write(s, handoff.Context(s, msgs, mode))
		if err != nil {
			return err
		}
		prompt := handoff.Prompt(s.Tool, p)
		if !t.Available() {
			handoff.Copy(prompt)
			fmt.Printf("%s isn't installed. Brief saved to %s and the prompt is on your clipboard.\n", t.Bin, p)
			return nil
		}
		return execIn(s.Project, append([]string{t.Bin}, t.Args(prompt)...))
	})
}

func execIn(dir string, argv []string) error {
	if dir != "" {
		if err := os.Chdir(dir); err != nil {
			fmt.Fprintf(os.Stderr, "aictl: %s is gone, staying in the current directory\n", dir)
		}
	}
	bin, err := exec.LookPath(argv[0])
	if err != nil {
		return fmt.Errorf("%s isn't on PATH. run this yourself:\n  %s", argv[0], handoff.Quote(dir, argv))
	}
	return syscall.Exec(bin, argv, os.Environ())
}

func cmdDoctor(srcs []session.Source) error {
	all, _, st := build(srcs)
	count := map[string]int{}
	for _, s := range all {
		count[s.Tool]++
	}
	fmt.Printf("aictl %s\n\n", version)
	for _, src := range srcs {
		files, _ := src.Files()
		mark := "·"
		if count[src.Name()] > 0 {
			mark = "✓"
		}
		fmt.Printf("  %s %-12s %4d sessions  %4d files  %s\n", mark, src.Name(), count[src.Name()], len(files), session.ShortPath(src.Roots()[0]))
	}
	fmt.Printf("\nindex: %d files, %d parsed, %d from cache, %d failed  (%s)\n", st.Files, st.Parsed, st.Cached, st.Failed, session.ShortPath(session.CacheDir()))
	if _, err := exec.LookPath("sqlite3"); err != nil {
		fmt.Println("note: sqlite3 not found; Antigravity and Cursor need it (it ships with macOS)")
	}
	for _, e := range st.Errors {
		fmt.Println("  !", e)
	}
	fmt.Println("\nhandoff targets:")
	for _, t := range handoff.Targets {
		state := "not installed"
		if t.Available() {
			state = "ok"
		}
		fmt.Printf("  %-13s %s\n", t.Name, state)
	}
	return nil
}
