// Package scan selects the files stray should read.
package scan

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/martinshaw/stray/internal/glob"
)

// DefaultExts are the source extensions scanned when the user does not pass -ext.
var DefaultExts = []string{".vue", ".svelte", ".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx", ".mts", ".cts"}

var ignoredDirs = map[string]bool{
	"node_modules": true,
	"dist":         true,
	"build":        true,
	"coverage":     true,
	".git":         true,
	"vendor":       true,
	".next":        true,
	".nuxt":        true,
	".output":      true,
	".vite":        true,
	".turbo":       true,
	".cache":       true,
	"out":          true,
	"target":       true,
}

// Options controls file selection.
type Options struct {
	Exts     []string
	Ignore   []string
	NoIgnore bool
}

// Files returns source files under root. Patterns are relative to root.
// An argument that exists as a file or directory is used directly; anything else
// is a glob. Missing patterns are returned so the CLI can fail clearly.
func Files(root string, args []string, opt Options) (files []string, missing []string, err error) {
	root, err = filepath.Abs(root)
	if err != nil {
		return nil, nil, err
	}
	exts := map[string]bool{}
	list := opt.Exts
	if len(list) == 0 {
		list = DefaultExts
	}
	for _, ext := range list {
		ext = strings.ToLower(ext)
		if ext != "" && !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}
		exts[ext] = true
	}
	if len(args) == 0 {
		found, err := walk(root, root, exts, opt, true)
		return found, nil, err
	}
	seen := map[string]bool{}
	for _, arg := range args {
		arg = filepath.ToSlash(arg)
		abs := arg
		if !filepath.IsAbs(arg) {
			abs = filepath.Join(root, filepath.FromSlash(arg))
		}
		if st, err := os.Stat(abs); err == nil {
			if st.IsDir() {
				found, err := walk(root, abs, exts, opt, true)
				if err != nil {
					return nil, nil, err
				}
				for _, f := range found {
					if !seen[f] {
						seen[f] = true
						files = append(files, f)
					}
				}
				continue
			}
			if !seen[abs] {
				seen[abs] = true
				files = append(files, abs)
			}
			continue
		}
		matched := false
		found, err := walk(root, root, exts, opt, !opt.NoIgnore)
		if err != nil {
			return nil, nil, err
		}
		for _, f := range found {
			rel, err := filepath.Rel(root, f)
			if err != nil {
				continue
			}
			if glob.Match(arg, filepath.ToSlash(rel)) && !seen[f] {
				seen[f] = true
				files = append(files, f)
				matched = true
			}
		}
		if !matched {
			missing = append(missing, arg)
		}
	}
	return files, missing, nil
}

func walk(root, start string, exts map[string]bool, opt Options, skipDefault bool) ([]string, error) {
	var files []string
	err := filepath.WalkDir(start, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != start && skipDefault && ignoredDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if ignored(rel, opt.Ignore) {
			return nil
		}
		if len(exts) > 0 && !exts[strings.ToLower(filepath.Ext(path))] {
			return nil
		}
		files = append(files, path)
		return nil
	})
	return files, err
}

func ignored(rel string, patterns []string) bool {
	for _, pattern := range patterns {
		if glob.Match(filepath.ToSlash(pattern), rel) {
			return true
		}
	}
	return false
}
