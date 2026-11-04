// Command sapper builds a self-hosted fortress and drills its backups.
package main

import (
	"os"

	"github.com/sapper-unbuilt/sapper/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
