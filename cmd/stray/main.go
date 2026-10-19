// Command stray reports JavaScript, TypeScript, and Vue imports that do not resolve.
package main

import (
	"os"

	"github.com/martinshaw/stray/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr, os.Stdin))
}
