package cli

import (
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }

func (s *stringList) Set(v string) error {
	*s = append(*s, v)
	return nil
}

func flagSet(stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet("stray", flag.ContinueOnError)
	fs.SetOutput(stderr)
	return fs
}

func wantColor(w io.Writer, noColor bool) bool {
	if noColor || os.Getenv("NO_COLOR") != "" {
		return false
	}
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func relFile(root, path string) (string, error) {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path), err
	}
	return filepath.ToSlash(rel), nil
}
