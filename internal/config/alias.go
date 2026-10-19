package config

import (
	"path/filepath"
	"strings"

	"github.com/martinshaw/stray/internal/project"
)

func extractAliases(src, configDir, root, source, origin string) ([]project.Alias, []string) {
	clean := stripJSComments(src)
	var aliases []project.Alias
	var notes []string
	for _, block := range aliasBlocks(clean) {
		var found []project.Alias
		var more []string
		if strings.HasPrefix(strings.TrimSpace(block), "[") {
			found, more = parseAliasArray(block, configDir, root, source, origin)
		} else {
			found, more = parseAliasObject(block, configDir, root, source, origin)
		}
		aliases = append(aliases, found...)
		notes = append(notes, more...)
	}
	return aliases, notes
}

func aliasBlocks(src string) []string {
	var blocks []string
	for i := 0; i < len(src); {
		j := indexIdent(src, i, "alias")
		if j < 0 {
			break
		}
		k := skipSpaceStr(src, j+len("alias"))
		if k < len(src) && src[k] == ':' {
			k = skipSpaceStr(src, k+1)
			if k < len(src) && (src[k] == '{' || src[k] == '[') {
				block, end, ok := readBalanced(src, k)
				if ok {
					blocks = append(blocks, block)
					i = end
					continue
				}
			}
		}
		i = j + len("alias")
	}
	return blocks
}

func parseAliasObject(block, configDir, root, source, origin string) ([]project.Alias, []string) {
	inner := strings.TrimSpace(block)
	if len(inner) >= 2 && inner[0] == '{' {
		inner = inner[1 : len(inner)-1]
	}
	return parseEntries(inner, configDir, root, source, origin, false)
}

func parseAliasArray(block, configDir, root, source, origin string) ([]project.Alias, []string) {
	inner := strings.TrimSpace(block)
	if len(inner) >= 2 && inner[0] == '[' {
		inner = inner[1 : len(inner)-1]
	}
	var aliases []project.Alias
	var notes []string
	i := 0
	for i < len(inner) {
		i = skipSpaceStr(inner, i)
		if i >= len(inner) {
			break
		}
		if inner[i] != '{' {
			i++
			continue
		}
		obj, end, ok := readBalanced(inner, i)
		if !ok {
			break
		}
		i = end
		body := strings.TrimSpace(obj)
		body = body[1 : len(body)-1]
		find, repl, ok := objectFields(body, "find", "replacement")
		if !ok {
			continue
		}
		a, note := aliasFrom(find, repl, configDir, root, source, origin)
		if note != "" {
			notes = append(notes, note)
		}
		if a != nil {
			aliases = append(aliases, *a)
		}
	}
	return aliases, notes
}

func parseEntries(inner, configDir, root, source, origin string, _ bool) ([]project.Alias, []string) {
	var aliases []project.Alias
	var notes []string
	i := 0
	for i < len(inner) {
		i = skipSpaceStr(inner, i)
		if i >= len(inner) {
			break
		}
		if inner[i] == ',' {
			i++
			continue
		}
		findTok, next, ok := readFind(inner, i)
		if !ok {
			i++
			continue
		}
		i = skipSpaceStr(inner, next)
		if i >= len(inner) || inner[i] != ':' {
			continue
		}
		i++
		expr, next := readExpr(inner, i)
		i = next
		a, note := aliasFrom(findTok, expr, configDir, root, source, origin)
		if note != "" {
			notes = append(notes, note)
		}
		if a != nil {
			aliases = append(aliases, *a)
		}
	}
	return aliases, notes
}

func objectFields(body, wantFind, wantRepl string) (string, string, bool) {
	find, repl := "", ""
	gotF, gotR := false, false
	i := 0
	for i < len(body) {
		i = skipSpaceStr(body, i)
		if i >= len(body) {
			break
		}
		key, next, ok := readFind(body, i)
		if !ok {
			i++
			continue
		}
		key = strings.Trim(key, `"'`)
		i = skipSpaceStr(body, next)
		if i >= len(body) || body[i] != ':' {
			continue
		}
		i++
		expr, next := readExpr(body, i)
		i = next
		switch key {
		case wantFind:
			find, gotF = expr, true
		case wantRepl:
			repl, gotR = expr, true
		}
	}
	return find, repl, gotF && gotR
}

func aliasFrom(findTok, expr, configDir, root, source, origin string) (*project.Alias, string) {
	prefix, ok, note := evalFind(findTok)
	if !ok {
		if note == "" {
			note = "could not read alias " + trimNote(findTok)
		}
		return nil, originBase(origin) + ": " + note
	}
	target, ok := evalExpr(expr, configDir, root)
	if !ok {
		return nil, originBase(origin) + ": could not evaluate alias " + prefix
	}
	a := &project.Alias{
		Prefix:      prefix,
		PrefixMatch: true,
		Targets:     []string{target},
		Source:      source,
		Origin:      origin,
	}
	if source == "webpack" && strings.HasSuffix(prefix, "$") {
		a.Prefix = strings.TrimSuffix(prefix, "$")
		a.Exact = true
		a.PrefixMatch = false
	}
	return a, ""
}

func originBase(origin string) string {
	if origin == "" {
		return "config"
	}
	return filepath.Base(origin)
}

func trimNote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 80 {
		return s[:80]
	}
	return s
}

func evalFind(tok string) (string, bool, string) {
	tok = strings.TrimSpace(tok)
	if s, ok := unquoteJS(tok); ok {
		return s, true, ""
	}
	if tok != "" && isIdentStart(tok[0]) && !strings.ContainsAny(tok, " \t\n()[]{}") {
		return tok, true, ""
	}
	if strings.HasPrefix(tok, "/") {
		body, ok := regexBody(tok)
		if !ok {
			return "", false, "unsupported regex alias " + trimNote(tok)
		}
		body = strings.TrimPrefix(body, "^")
		body = strings.TrimSuffix(body, "$")
		body = strings.ReplaceAll(body, `\/`, "/")
		body = strings.ReplaceAll(body, "(.*)", "")
		body = strings.ReplaceAll(body, "(.*?)", "")
		if strings.ContainsAny(body, "()[]{}|+?^$\\") {
			return "", false, "unsupported regex alias " + trimNote(tok)
		}
		return body, true, ""
	}
	return "", false, "could not read alias " + trimNote(tok)
}

func regexBody(tok string) (string, bool) {
	if len(tok) < 2 || tok[0] != '/' {
		return "", false
	}
	for i := 1; i < len(tok); i++ {
		if tok[i] == '\\' && i+1 < len(tok) {
			i++
			continue
		}
		if tok[i] == '/' {
			return tok[1:i], true
		}
	}
	return "", false
}

func evalExpr(expr, configDir, root string) (string, bool) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return "", false
	}
	for strings.HasPrefix(expr, "(") && strings.HasSuffix(expr, ")") && balancedParens(expr[1:len(expr)-1]) {
		expr = strings.TrimSpace(expr[1 : len(expr)-1])
	}
	if parts := splitPlus(expr); len(parts) > 1 {
		var b strings.Builder
		for _, part := range parts {
			s, ok := evalExpr(part, configDir, root)
			if !ok {
				return "", false
			}
			b.WriteString(s)
		}
		return filepath.Clean(b.String()), true
	}
	if s, ok := unquoteJS(expr); ok {
		if filepath.IsAbs(s) {
			return filepath.Clean(s), true
		}
		return filepath.Join(configDir, filepath.FromSlash(s)), true
	}
	if strings.Contains(expr, "new URL(") || strings.HasPrefix(expr, "fileURLToPath(") {
		if u, ok := firstJSString(expr); ok {
			if filepath.IsAbs(u) {
				return filepath.Clean(u), true
			}
			return filepath.Clean(filepath.Join(configDir, filepath.FromSlash(u))), true
		}
	}
	for _, name := range []string{"path.resolve", "path.join", "resolve", "join"} {
		if isCall(expr, name) {
			return evalJoin(expr, configDir, root)
		}
	}
	switch expr {
	case "__dirname", "import.meta.url":
		return configDir, true
	case "process.cwd()":
		return root, true
	}
	return "", false
}

func evalJoin(expr, configDir, root string) (string, bool) {
	args := callArgs(expr)
	if len(args) == 0 {
		return "", false
	}
	var segs []string
	for _, arg := range args {
		arg = strings.TrimSpace(arg)
		switch arg {
		case "__dirname", "import.meta.url":
			segs = append(segs, configDir)
		case "process.cwd()":
			segs = append(segs, root)
		default:
			if s, ok := unquoteJS(arg); ok {
				segs = append(segs, s)
				continue
			}
			s, ok := evalExpr(arg, configDir, root)
			if !ok {
				return "", false
			}
			segs = append(segs, s)
		}
	}
	if len(segs) == 0 {
		return "", false
	}
	return filepath.Clean(filepath.Join(segs...)), true
}

func isCall(expr, name string) bool {
	return strings.HasPrefix(expr, name+"(") && strings.HasSuffix(strings.TrimSpace(expr), ")")
}

func callArgs(expr string) []string {
	open := strings.Index(expr, "(")
	if open < 0 {
		return nil
	}
	inner := expr[open+1 : len(expr)-1]
	return splitDepth(inner, ',')
}

func splitPlus(expr string) []string {
	parts := splitDepth(expr, '+')
	if len(parts) < 2 {
		return nil
	}
	return parts
}

func splitDepth(s string, sep byte) []string {
	var parts []string
	start := 0
	depth := 0
	in := byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if in != 0 {
			if c == '\\' && i+1 < len(s) {
				i++
				continue
			}
			if c == in {
				in = 0
			}
			continue
		}
		if c == '\'' || c == '"' || c == '`' {
			in = c
			continue
		}
		if c == '(' || c == '[' || c == '{' {
			depth++
			continue
		}
		if c == ')' || c == ']' || c == '}' {
			if depth > 0 {
				depth--
			}
			continue
		}
		if c == sep && depth == 0 {
			parts = append(parts, strings.TrimSpace(s[start:i]))
			start = i + 1
		}
	}
	parts = append(parts, strings.TrimSpace(s[start:]))
	return parts
}

func balancedParens(s string) bool {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth < 0 {
				return false
			}
		}
	}
	return depth == 0
}

func firstJSString(expr string) (string, bool) {
	for i := 0; i < len(expr); i++ {
		if expr[i] == '\'' || expr[i] == '"' {
			s, next, ok := takeQuoted(expr, i)
			if ok {
				return s, true
			}
			i = next
		}
	}
	return "", false
}

func unquoteJS(tok string) (string, bool) {
	tok = strings.TrimSpace(tok)
	if len(tok) < 2 {
		return "", false
	}
	if tok[0] != '\'' && tok[0] != '"' && tok[0] != '`' {
		return "", false
	}
	s, _, ok := takeQuoted(tok, 0)
	return s, ok
}

func takeQuoted(s string, i int) (string, int, bool) {
	if i >= len(s) {
		return "", i, false
	}
	q := s[i]
	i++
	var b strings.Builder
	for i < len(s) {
		c := s[i]
		if c == '\\' && i+1 < len(s) {
			n := s[i+1]
			switch n {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			default:
				b.WriteByte(n)
			}
			i += 2
			continue
		}
		if c == q {
			return b.String(), i + 1, true
		}
		b.WriteByte(c)
		i++
	}
	return "", i, false
}

func readFind(s string, i int) (string, int, bool) {
	i = skipSpaceStr(s, i)
	if i >= len(s) {
		return "", i, false
	}
	if s[i] == '\'' || s[i] == '"' {
		_, next, ok := takeQuoted(s, i)
		if !ok {
			return "", i, false
		}
		return s[i:next], next, true
	}
	if s[i] == '/' {
		bodyEnd := i + 1
		for bodyEnd < len(s) {
			if s[bodyEnd] == '\\' && bodyEnd+1 < len(s) {
				bodyEnd += 2
				continue
			}
			if s[bodyEnd] == '/' {
				bodyEnd++
				for bodyEnd < len(s) && isIdentByte(s[bodyEnd]) {
					bodyEnd++
				}
				return s[i:bodyEnd], bodyEnd, true
			}
			bodyEnd++
		}
		return "", i, false
	}
	if isIdentStart(s[i]) {
		j := i + 1
		for j < len(s) && isIdentByte(s[j]) {
			j++
		}
		return s[i:j], j, true
	}
	return "", i, false
}

func readExpr(s string, i int) (string, int) {
	i = skipSpaceStr(s, i)
	start := i
	depth := 0
	in := byte(0)
	for i < len(s) {
		c := s[i]
		if in != 0 {
			if c == '\\' && i+1 < len(s) {
				i += 2
				continue
			}
			if c == in {
				in = 0
			}
			i++
			continue
		}
		if c == '\'' || c == '"' || c == '`' {
			in = c
			i++
			continue
		}
		if c == '(' || c == '[' || c == '{' {
			depth++
			i++
			continue
		}
		if c == ')' || c == ']' || c == '}' {
			if depth == 0 {
				break
			}
			depth--
			i++
			continue
		}
		if c == ',' && depth == 0 {
			return strings.TrimSpace(s[start:i]), i + 1
		}
		i++
	}
	return strings.TrimSpace(s[start:i]), i
}

func readBalanced(s string, i int) (string, int, bool) {
	if i >= len(s) {
		return "", i, false
	}
	open := s[i]
	var close byte
	switch open {
	case '{':
		close = '}'
	case '[':
		close = ']'
	case '(':
		close = ')'
	default:
		return "", i, false
	}
	depth := 0
	in := byte(0)
	for j := i; j < len(s); j++ {
		c := s[j]
		if in != 0 {
			if c == '\\' && j+1 < len(s) {
				j++
				continue
			}
			if c == in {
				in = 0
			}
			continue
		}
		if c == '\'' || c == '"' || c == '`' {
			in = c
			continue
		}
		if c == open {
			depth++
		} else if c == close {
			depth--
			if depth == 0 {
				return s[i : j+1], j + 1, true
			}
		}
	}
	return "", i, false
}

func stripJSComments(src string) string {
	var b strings.Builder
	b.Grow(len(src))
	in := byte(0)
	for i := 0; i < len(src); i++ {
		c := src[i]
		if in != 0 {
			b.WriteByte(c)
			if c == '\\' && i+1 < len(src) {
				b.WriteByte(src[i+1])
				i++
				continue
			}
			if c == in {
				in = 0
			}
			continue
		}
		if c == '\'' || c == '"' || c == '`' {
			in = c
			b.WriteByte(c)
			continue
		}
		if c == '/' && i+1 < len(src) && src[i+1] == '/' && (i == 0 || src[i-1] != '\\') {
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
				} else {
					b.WriteByte(' ')
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
	return b.String()
}

func indexIdent(s string, from int, word string) int {
	for i := from; i+len(word) <= len(s); i++ {
		if s[i:i+len(word)] != word {
			continue
		}
		if i > 0 && isIdentByte(s[i-1]) {
			continue
		}
		if i+len(word) < len(s) && isIdentByte(s[i+len(word)]) {
			continue
		}
		return i
	}
	return -1
}

func skipSpaceStr(s string, i int) int {
	for i < len(s) {
		if s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r' {
			i++
			continue
		}
		break
	}
	return i
}

func isIdentStart(c byte) bool {
	return c == '_' || c == '$' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isIdentByte(c byte) bool {
	return c == '_' || c == '$' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

func nuxtSourceDir(src, configDir string) string {
	i := indexIdent(src, 0, "srcDir")
	if i < 0 {
		return configDir
	}
	rest := strings.TrimLeft(src[i+len("srcDir"):], " \t\r\n")
	if !strings.HasPrefix(rest, ":") && !strings.HasPrefix(rest, "=") {
		return configDir
	}
	rest = strings.TrimLeft(rest[1:], " \t\r\n")
	if s, ok := firstJSString(rest); ok {
		if filepath.IsAbs(s) {
			return filepath.Clean(s)
		}
		return filepath.Join(configDir, filepath.FromSlash(s))
	}
	return configDir
}

func addNuxtAliases(src, configDir, root, origin string, existing []project.Alias) []project.Alias {
	srcDir := nuxtSourceDir(src, configDir)
	defaults := []project.Alias{
		{Prefix: "@@", PrefixMatch: true, Targets: []string{root}, Source: "nuxt", Origin: origin},
		{Prefix: "~~", PrefixMatch: true, Targets: []string{root}, Source: "nuxt", Origin: origin},
		{Prefix: "@", PrefixMatch: true, Targets: []string{srcDir}, Source: "nuxt", Origin: origin},
		{Prefix: "~", PrefixMatch: true, Targets: []string{srcDir}, Source: "nuxt", Origin: origin},
	}
	have := map[string]bool{}
	for _, a := range existing {
		have[a.Prefix] = true
	}
	for _, d := range defaults {
		if have[d.Prefix] {
			continue
		}
		existing = append(existing, d)
	}
	return existing
}

func parseStringArray(src, key string) []string {
	clean := stripJSComments(src)
	j := indexIdent(clean, 0, key)
	if j < 0 {
		return nil
	}
	// Prefer the array that sits inside a resolve block when one exists.
	k := skipSpaceStr(clean, j+len(key))
	if k >= len(clean) || clean[k] != ':' {
		return nil
	}
	k = skipSpaceStr(clean, k+1)
	if k >= len(clean) || clean[k] != '[' {
		return nil
	}
	block, _, ok := readBalanced(clean, k)
	if !ok {
		return nil
	}
	var out []string
	for i := 0; i < len(block); i++ {
		if block[i] != '\'' && block[i] != '"' {
			continue
		}
		s, next, ok := takeQuoted(block, i)
		if !ok {
			break
		}
		out = append(out, s)
		i = next - 1
	}
	return out
}
