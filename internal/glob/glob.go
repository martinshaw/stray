// Package glob matches slash-separated paths, including **.
package glob

import "strings"

// Meta reports whether pattern contains a glob metacharacter.
func Meta(pattern string) bool {
	return strings.ContainsAny(pattern, "*?[")
}

// Match reports whether path matches pattern.
// Paths and patterns use forward slashes.
// A pattern with no slash and no ** also matches a file's base name,
// so "*.vue" selects every Vue file in the tree.
func Match(pattern, path string) bool {
	pattern = strings.TrimPrefix(pattern, "./")
	path = strings.TrimPrefix(path, "./")
	if matchParts(split(pattern), split(path)) {
		return true
	}
	if !strings.Contains(pattern, "/") && !strings.Contains(pattern, "**") {
		base := path
		if i := strings.LastIndex(path, "/"); i >= 0 {
			base = path[i+1:]
		}
		return matchParts(split(pattern), []string{base})
	}
	return false
}

func split(p string) []string {
	p = strings.Trim(p, "/")
	if p == "" {
		return nil
	}
	return strings.Split(p, "/")
}

func matchParts(pat, path []string) bool {
	for {
		if len(pat) == 0 {
			return len(path) == 0
		}
		if pat[0] == "**" {
			if matchParts(pat[1:], path) {
				return true
			}
			if len(path) == 0 {
				return false
			}
			path = path[1:]
			continue
		}
		if len(path) == 0 || !matchSeg(pat[0], path[0]) {
			return false
		}
		pat = pat[1:]
		path = path[1:]
	}
}

func matchSeg(pat, seg string) bool {
	for {
		if pat == "" {
			return seg == ""
		}
		if pat[0] == '*' {
			pat = pat[1:]
			for i := 0; i <= len(seg); i++ {
				if matchSeg(pat, seg[i:]) {
					return true
				}
			}
			return false
		}
		if seg == "" {
			return false
		}
		if pat[0] == '?' || pat[0] == seg[0] {
			pat = pat[1:]
			seg = seg[1:]
			continue
		}
		if pat[0] == '[' {
			end := strings.IndexByte(pat, ']')
			if end < 0 {
				return false
			}
			if !class(pat[1:end], seg[0]) {
				return false
			}
			pat = pat[end+1:]
			seg = seg[1:]
			continue
		}
		return false
	}
}

func class(body string, c byte) bool {
	neg := false
	if strings.HasPrefix(body, "!") || strings.HasPrefix(body, "^") {
		neg = true
		body = body[1:]
	}
	ok := false
	for i := 0; i < len(body); i++ {
		if i+2 < len(body) && body[i+1] == '-' {
			if c >= body[i] && c <= body[i+2] {
				ok = true
				break
			}
			i += 2
			continue
		}
		if body[i] == c {
			ok = true
			break
		}
	}
	if neg {
		return !ok
	}
	return ok
}
