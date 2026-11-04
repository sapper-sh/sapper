//go:build unix

package ops

import "golang.org/x/sys/unix"

func canWrite(dir string) bool { return unix.Access(dir, unix.W_OK) == nil }
