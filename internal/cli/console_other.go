//go:build !unix

package cli

import "context"

// runConsole is the Unix terminal session. Other platforms keep the boot screen only.
func (a *app) runConsole(context.Context) error { return nil }
