package project

import (
	"path/filepath"
	"strings"

	"github.com/martinshaw/stray/internal/glob"
)

// For returns the nearest package scope and the TypeScript project that contains file.
func (p *Project) For(file string) (Scope, *TSConfig) {
	file = filepath.Clean(file)
	var nearest Scope
	found := false
	var best *TSConfig
	bestScore := -1
	bestDir := -1
	for i := range p.Scopes {
		s := &p.Scopes[i]
		if !DirPrefix(s.Dir, file) {
			continue
		}
		if !found {
			nearest = *s
			found = true
		}
		for j := range s.TS {
			t := &s.TS[j]
			if !t.Matches(file) {
				continue
			}
			score := t.specificity(file)
			if score > bestScore || (score == bestScore && len(t.Dir) > bestDir) {
				best = t
				bestScore = score
				bestDir = len(t.Dir)
			}
		}
	}
	return nearest, best
}

// DirPrefix reports whether file is dir or a path inside dir.
func DirPrefix(dir, file string) bool {
	dir = filepath.Clean(dir)
	file = filepath.Clean(file)
	if dir == file {
		return true
	}
	sep := string(filepath.Separator)
	if !strings.HasSuffix(dir, sep) {
		dir += sep
	}
	return strings.HasPrefix(file, dir)
}

// Matches reports whether file belongs to this TypeScript project.
func (t TSConfig) Matches(file string) bool {
	if t.Solution {
		return false
	}
	rel, err := filepath.Rel(t.Dir, file)
	if err != nil {
		return false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	rel = filepath.ToSlash(rel)
	for _, ex := range t.Exclude {
		if globAny(ex, rel) {
			return false
		}
	}
	if len(t.Include) == 0 {
		return true
	}
	for _, in := range t.Include {
		if globAny(in, rel) {
			return true
		}
	}
	return false
}

func (t TSConfig) specificity(file string) int {
	rel, err := filepath.Rel(t.Dir, file)
	if err != nil {
		return 0
	}
	rel = filepath.ToSlash(rel)
	if len(t.Include) == 0 {
		return 1
	}
	best := 0
	for _, in := range t.Include {
		in = strings.TrimPrefix(filepath.ToSlash(in), "./")
		if globAny(in, rel) && len(in) > best {
			best = len(in)
		}
	}
	return 100 + best
}

func globAny(pattern, path string) bool {
	pattern = strings.TrimPrefix(filepath.ToSlash(pattern), "./")
	if glob.Match(pattern, path) {
		return true
	}
	if !glob.Meta(pattern) {
		return glob.Match(strings.TrimSuffix(pattern, "/")+"/**", path)
	}
	return false
}
