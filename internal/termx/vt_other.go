//go:build !windows

package termx

import "os"

func enableVT(*os.File) bool { return true }
