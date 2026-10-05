package term

import (
	"os"
	"syscall"
	"time"
	"unicode/utf8"
)

// KeyCode identifies non-printable keys. Printable input arrives as KeyRune.
type KeyCode int

const (
	KeyRune KeyCode = iota
	KeyEnter
	KeyTab
	KeyBacktab
	KeyBackspace
	KeyEsc
	KeyUp
	KeyDown
	KeyLeft
	KeyRight
	KeyHome
	KeyEnd
	KeyPgUp
	KeyPgDn
	KeyDelete
	KeyCtrl // Rune holds the letter, e.g. Ctrl+d => Rune 'd'
	KeyPaste
)

type Key struct {
	Code  KeyCode
	Rune  rune
	Paste string
}

// String gives a compact name, handy for keymaps and tests.
func (k Key) String() string {
	switch k.Code {
	case KeyRune:
		return string(k.Rune)
	case KeyCtrl:
		return "C-" + string(k.Rune)
	}
	return map[KeyCode]string{
		KeyEnter: "enter", KeyTab: "tab", KeyBacktab: "backtab", KeyBackspace: "backspace", KeyEsc: "esc",
		KeyUp: "up", KeyDown: "down", KeyLeft: "left", KeyRight: "right", KeyHome: "home", KeyEnd: "end",
		KeyPgUp: "pgup", KeyPgDn: "pgdn", KeyDelete: "delete", KeyPaste: "paste",
	}[k.Code]
}

// Input decodes keys from the tty. It reads with a short timeout (VTIME) so
// it can be paused cleanly while an editor or another program owns the tty.
type Input struct {
	fd     int
	Keys   chan Key
	pause  chan chan struct{}
	resume chan struct{}
}

func NewInput(f *os.File) *Input {
	in := &Input{fd: int(f.Fd()), Keys: make(chan Key, 64), pause: make(chan chan struct{}), resume: make(chan struct{})}
	go in.loop()
	return in
}

// Pause stops reading and returns once no read is in flight.
func (in *Input) Pause() {
	ack := make(chan struct{})
	in.pause <- ack
	<-ack
}

func (in *Input) Resume() { in.resume <- struct{}{} }

func (in *Input) loop() {
	buf := make([]byte, 4096)
	var pending []byte
	var pendingSince time.Time
	for {
		select {
		case ack := <-in.pause:
			pending = nil
			close(ack)
			<-in.resume
			continue
		default:
		}
		n, err := syscall.Read(in.fd, buf)
		if err != nil && err != syscall.EINTR && err != syscall.EAGAIN {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		if n > 0 {
			pending = append(pending, buf[:n]...)
			pendingSince = time.Now()
		}
		if len(pending) == 0 {
			continue
		}
		final := n == 0 || time.Since(pendingSince) > 30*time.Millisecond
		keys, rest := Decode(pending, final)
		for _, k := range keys {
			in.Keys <- k
		}
		pending = rest
	}
}

// Decode turns raw bytes into keys. If final is false, an incomplete escape
// sequence at the end is returned as rest so more bytes can complete it.
func Decode(b []byte, final bool) ([]Key, []byte) {
	var keys []Key
	for len(b) > 0 {
		c := b[0]
		switch {
		case c == 0x1b:
			if len(b) == 1 {
				if !final {
					return keys, b
				}
				keys = append(keys, Key{Code: KeyEsc})
				b = b[1:]
				continue
			}
			k, n, complete := escape(b)
			if !complete {
				if !final {
					return keys, b
				}
				keys = append(keys, Key{Code: KeyEsc})
				b = b[1:]
				continue
			}
			if n == 0 { // unknown sequence: drop ESC, keep going
				b = b[1:]
				continue
			}
			if k.Code != -1 {
				keys = append(keys, k)
			}
			b = b[n:]
		case c == '\r' || c == '\n':
			keys = append(keys, Key{Code: KeyEnter})
			b = b[1:]
		case c == '\t':
			keys = append(keys, Key{Code: KeyTab})
			b = b[1:]
		case c == 127 || c == 8:
			keys = append(keys, Key{Code: KeyBackspace})
			b = b[1:]
		case c < 32:
			keys = append(keys, Key{Code: KeyCtrl, Rune: rune('a' + c - 1)})
			b = b[1:]
		default:
			if !utf8.FullRune(b) && !final {
				return keys, b
			}
			r, n := utf8.DecodeRune(b)
			keys = append(keys, Key{Code: KeyRune, Rune: r})
			b = b[n:]
		}
	}
	return keys, nil
}

// escape parses one escape sequence starting at b[0]==ESC.
func escape(b []byte) (Key, int, bool) {
	if b[1] == '[' || b[1] == 'O' {
		// bracketed paste
		if len(b) >= 6 && string(b[:6]) == "\x1b[200~" {
			end := indexOf(b, "\x1b[201~")
			if end < 0 {
				return Key{}, 0, false
			}
			return Key{Code: KeyPaste, Paste: string(b[6:end])}, end + 6, true
		}
		// find the final byte (0x40..0x7e)
		i := 2
		for i < len(b) && (b[i] < 0x40 || b[i] > 0x7e) {
			i++
		}
		if i >= len(b) {
			return Key{}, 0, false
		}
		seq := string(b[2:i])
		fin := b[i]
		n := i + 1
		switch fin {
		case 'A':
			return Key{Code: KeyUp}, n, true
		case 'B':
			return Key{Code: KeyDown}, n, true
		case 'C':
			return Key{Code: KeyRight}, n, true
		case 'D':
			return Key{Code: KeyLeft}, n, true
		case 'H':
			return Key{Code: KeyHome}, n, true
		case 'F':
			return Key{Code: KeyEnd}, n, true
		case 'Z':
			return Key{Code: KeyBacktab}, n, true
		case '~':
			switch seq {
			case "1", "7":
				return Key{Code: KeyHome}, n, true
			case "4", "8":
				return Key{Code: KeyEnd}, n, true
			case "3":
				return Key{Code: KeyDelete}, n, true
			case "5":
				return Key{Code: KeyPgUp}, n, true
			case "6":
				return Key{Code: KeyPgDn}, n, true
			}
		}
		return Key{Code: -1}, n, true // recognised shape, unhandled key: swallow it
	}
	// Alt+key arrives as ESC followed by the key; treat as the key.
	return Key{Code: -1}, 1, true
}

func indexOf(b []byte, s string) int {
	for i := 0; i+len(s) <= len(b); i++ {
		if string(b[i:i+len(s)]) == s {
			return i
		}
	}
	return -1
}
