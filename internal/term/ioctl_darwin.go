package term

import "syscall"

const (
	ioctlGet = uintptr(syscall.TIOCGETA)
	ioctlSet = uintptr(syscall.TIOCSETA)
)
