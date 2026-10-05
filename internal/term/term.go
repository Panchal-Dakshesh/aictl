// Package term is aictl's terminal layer: raw mode, screen size, the
// alternate screen, and resize notifications. No third-party libraries.
package term

import (
	"os"
	"os/signal"
	"sync"
	"syscall"
	"unsafe"
)

// Terminal owns the tty for the lifetime of the TUI.
type Terminal struct {
	in, out *os.File
	saved   syscall.Termios
	raw     bool
	Resize  chan struct{}
	once    sync.Once
}

func Open() (*Terminal, error) {
	t := &Terminal{in: os.Stdin, out: os.Stdout, Resize: make(chan struct{}, 1)}
	return t, nil
}

// EnterRaw switches the tty to raw mode and the alternate screen.
func (t *Terminal) EnterRaw() error {
	fd := t.in.Fd()
	if err := tcget(fd, &t.saved); err != nil {
		return err
	}
	raw := t.saved
	// cfmakeraw, by hand
	raw.Iflag &^= syscall.IGNBRK | syscall.BRKINT | syscall.PARMRK | syscall.ISTRIP | syscall.INLCR | syscall.IGNCR | syscall.ICRNL | syscall.IXON
	raw.Oflag &^= syscall.OPOST
	raw.Lflag &^= syscall.ECHO | syscall.ECHONL | syscall.ICANON | syscall.ISIG | syscall.IEXTEN
	raw.Cflag &^= syscall.CSIZE | syscall.PARENB
	raw.Cflag |= syscall.CS8
	raw.Cc[syscall.VMIN] = 0
	raw.Cc[syscall.VTIME] = 1 // reads return every 100ms so input can be paused
	if err := tcset(fd, &raw); err != nil {
		return err
	}
	t.raw = true
	// alt screen, hide cursor, enable bracketed paste, clear
	t.out.WriteString("\x1b[?1049h\x1b[?25l\x1b[?2004h\x1b[2J")

	t.once.Do(func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGWINCH)
		go func() {
			for range sig {
				select {
				case t.Resize <- struct{}{}:
				default:
				}
			}
		}()
	})
	return nil
}

// Restore puts the terminal back exactly as we found it.
func (t *Terminal) Restore() {
	if !t.raw {
		return
	}
	t.out.WriteString("\x1b[0m\x1b[?2004l\x1b[?25h\x1b[?1049l")
	tcset(t.in.Fd(), &t.saved)
	t.raw = false
}

func (t *Terminal) Write(b []byte) (int, error) { return t.out.Write(b) }

type winsize struct{ Row, Col, X, Y uint16 }

// Size returns columns, rows.
func (t *Terminal) Size() (int, int) {
	var ws winsize
	_, _, e := syscall.Syscall(syscall.SYS_IOCTL, t.out.Fd(), uintptr(syscall.TIOCGWINSZ), uintptr(unsafe.Pointer(&ws)))
	if e != 0 || ws.Col == 0 {
		return 100, 30
	}
	return int(ws.Col), int(ws.Row)
}

func tcget(fd uintptr, t *syscall.Termios) error {
	_, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, ioctlGet, uintptr(unsafe.Pointer(t)))
	if e != 0 {
		return e
	}
	return nil
}

func tcset(fd uintptr, t *syscall.Termios) error {
	_, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, ioctlSet, uintptr(unsafe.Pointer(t)))
	if e != 0 {
		return e
	}
	return nil
}

// IsTerminal reports whether stdout is a tty.
func IsTerminal() bool {
	var t syscall.Termios
	return tcget(os.Stdout.Fd(), &t) == nil
}
