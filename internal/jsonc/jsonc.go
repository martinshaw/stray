// Package jsonc strips comments and trailing commas from JSONC documents.
package jsonc

import "bytes"

// Strip returns JSONC with comments and trailing commas removed.
// Strings are preserved, including comment-like sequences inside them.
func Strip(src string) string {
	var b bytes.Buffer
	b.Grow(len(src))
	inStr := false
	esc := false
	for i := 0; i < len(src); i++ {
		c := src[i]
		if inStr {
			b.WriteByte(c)
			if esc {
				esc = false
				continue
			}
			if c == '\\' {
				esc = true
				continue
			}
			if c == '"' {
				inStr = false
			}
			continue
		}
		if c == '"' {
			inStr = true
			b.WriteByte(c)
			continue
		}
		if c == '/' && i+1 < len(src) && src[i+1] == '/' {
			i += 2
			for i < len(src) && src[i] != '\n' {
				i++
			}
			b.WriteByte('\n')
			continue
		}
		if c == '/' && i+1 < len(src) && src[i+1] == '*' {
			i += 2
			for i+1 < len(src) && !(src[i] == '*' && src[i+1] == '/') {
				if src[i] == '\n' {
					b.WriteByte('\n')
				}
				i++
			}
			if i+1 < len(src) {
				i++
			}
			continue
		}
		b.WriteByte(c)
	}
	return string(stripTrailingCommas(b.Bytes()))
}

func stripTrailingCommas(in []byte) []byte {
	var b bytes.Buffer
	b.Grow(len(in))
	inStr := false
	esc := false
	for i := 0; i < len(in); i++ {
		c := in[i]
		if inStr {
			b.WriteByte(c)
			if esc {
				esc = false
				continue
			}
			if c == '\\' {
				esc = true
				continue
			}
			if c == '"' {
				inStr = false
			}
			continue
		}
		if c == '"' {
			inStr = true
			b.WriteByte(c)
			continue
		}
		if c == ',' {
			j := i + 1
			for j < len(in) && (in[j] == ' ' || in[j] == '\t' || in[j] == '\n' || in[j] == '\r') {
				j++
			}
			if j < len(in) && (in[j] == '}' || in[j] == ']') {
				continue
			}
		}
		b.WriteByte(c)
	}
	return b.Bytes()
}
