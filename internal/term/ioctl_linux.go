package term

import "syscall"

const (
	ioctlGet = uintptr(syscall.TCGETS)
	ioctlSet = uintptr(syscall.TCSETS)
)
