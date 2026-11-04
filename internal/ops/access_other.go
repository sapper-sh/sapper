//go:build !unix

package ops

import "os"

// canWrite is approximate off unix: the directory exists and is not read-only.
func canWrite(dir string) bool {
	st, err := os.Stat(dir)
	return err == nil && st.Mode().Perm()&0o200 != 0
}
