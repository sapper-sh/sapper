//go:build !unix

package ops

import "errors"

func freeBytes(string) (uint64, error) { return 0, errors.New("free space unknown on this platform") }
