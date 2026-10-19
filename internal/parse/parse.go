// Package parse extracts static import specifiers from JS, TS, Vue, and style blocks.
package parse

import (
	"path/filepath"
	"strings"
)

const (
	// KindStatic is an import or re-export with a literal specifier.
	KindStatic = "static"
	// KindDynamic is an import() or a non-literal specifier.
	KindDynamic = "dynamic"
	// KindRequire is a require() or require.resolve() call.
	KindRequire = "require"
	// KindGlob is import.meta.glob.
	KindGlob = "glob"
	// KindStyle is a CSS @import or url().
	KindStyle = "style"
	// KindURL is new URL("./file", import.meta.url).
	KindURL = "url"
)

// Import is one specifier found in a source file.
type Import struct {
	Specifier string
	Line      int
	Column    int
	Kind      string
	Literal   bool
}

// FileImports scans src using the filename's extension to choose a strategy.
func FileImports(path, src string) []Import {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".vue", ".svelte":
		return vueImports(src)
	case ".css", ".scss", ".sass", ".less":
		return styleImports(src, 0)
	default:
		return scanJS(src, 0)
	}
}

func vueImports(src string) []Import {
	var out []Import
	lower := src
	i := 0
	for i < len(src) {
		rel, kind, tag, ok := nextTag(lower, i)
		if !ok {
			break
		}
		abs := rel
		tagLine := 1 + strings.Count(src[:abs], "\n")
		attrs, end, selfClose := readTag(src, abs)
		if end < 0 {
			break
		}
		if srcAttr, ok := attr(attrs, "src"); ok && (kind == "script" || kind == "style") {
			out = append(out, Import{
				Specifier: srcAttr,
				Line:      tagLine,
				Column:    1,
				Kind:      kindOf(kind),
				Literal:   true,
			})
		}
		if selfClose {
			i = end
			continue
		}
		closeAt := indexFold(src[end:], "</"+tag)
		if closeAt < 0 {
			break
		}
		bodyStart := end
		bodyEnd := end + closeAt
		body := src[bodyStart:bodyEnd]
		base := strings.Count(src[:bodyStart], "\n")
		if kind == "script" {
			out = append(out, scanJS(body, base)...)
		} else if kind == "style" {
			out = append(out, styleImports(body, base)...)
		}
		i = bodyEnd
		if endTag := indexFold(src[i:], ">"); endTag >= 0 {
			i += endTag + 1
		}
	}
	return out
}

func kindOf(tag string) string {
	if tag == "style" {
		return KindStyle
	}
	return KindStatic
}

func nextTag(src string, from int) (at int, kind, tag string, ok bool) {
	for i := from; i < len(src); i++ {
		if src[i] != '<' {
			continue
		}
		rest := src[i+1:]
		switch {
		case hasPrefixFold(rest, "script"):
			n := len("script")
			if n < len(rest) && isIdentByte(rest[n]) {
				continue
			}
			return i, "script", "script", true
		case hasPrefixFold(rest, "style"):
			n := len("style")
			if n < len(rest) && isIdentByte(rest[n]) {
				continue
			}
			return i, "style", "style", true
		}
	}
	return 0, "", "", false
}

func readTag(src string, at int) (attrs string, end int, selfClose bool) {
	i := at
	quote := byte(0)
	for i < len(src) {
		c := src[i]
		if quote != 0 {
			if c == '\\' && i+1 < len(src) {
				i += 2
				continue
			}
			if c == quote {
				quote = 0
			}
			i++
			continue
		}
		if c == '"' || c == '\'' {
			quote = c
			i++
			continue
		}
		if c == '>' {
			body := src[at+1 : i]
			self := strings.HasSuffix(strings.TrimRight(body, " \t\r\n"), "/")
			return body, i + 1, self
		}
		i++
	}
	return "", -1, false
}

func attr(tag, name string) (string, bool) {
	i := 0
	for i < len(tag) {
		for i < len(tag) && !isIdentStart(tag[i]) {
			i++
		}
		if i >= len(tag) {
			break
		}
		start := i
		for i < len(tag) && isIdentByte(tag[i]) {
			i++
		}
		key := tag[start:i]
		for i < len(tag) && (tag[i] == ' ' || tag[i] == '\t' || tag[i] == '\n' || tag[i] == '\r') {
			i++
		}
		if i >= len(tag) || tag[i] != '=' {
			continue
		}
		i++
		for i < len(tag) && (tag[i] == ' ' || tag[i] == '\t') {
			i++
		}
		if i >= len(tag) {
			break
		}
		if tag[i] != '"' && tag[i] != '\'' {
			continue
		}
		q := tag[i]
		i++
		vStart := i
		for i < len(tag) && tag[i] != q {
			i++
		}
		val := tag[vStart:i]
		if i < len(tag) {
			i++
		}
		if strings.EqualFold(key, name) {
			return val, true
		}
	}
	return "", false
}

func styleImports(src string, baseLine int) []Import {
	var out []Import
	s := &scanner{src: src, line: 1, col: 1, baseLine: baseLine}
	for s.i < len(s.src) {
		if s.peek() == '/' && s.peekN(1) == '*' {
			s.skipBlock()
			continue
		}
		if hasHere(s, "@import") {
			line, col := s.line, s.col
			s.i += len("@import")
			s.col += len("@import")
			s.skip()
			if hasHere(s, "url") {
				s.i += 3
				s.col += 3
				s.skip()
				if s.peek() == '(' {
					s.next()
				}
			}
			val, lit, vl, vc, ok := s.readSpec()
			if ok && lit && !externalSpec(val) {
				out = append(out, Import{Specifier: val, Line: s.baseLine + vl, Column: vc, Kind: KindStyle, Literal: true})
			}
			_, _ = line, col
			continue
		}
		if hasHere(s, "url(") {
			line, col := s.line, s.col
			s.i += 4
			s.col += 4
			s.skip()
			val, lit, vl, vc, ok := s.readSpec()
			if ok && lit && isRelativeOrAlias(val) {
				out = append(out, Import{Specifier: val, Line: s.baseLine + vl, Column: vc, Kind: KindStyle, Literal: true})
			}
			_, _ = line, col
			continue
		}
		s.next()
	}
	return out
}

func externalSpec(v string) bool {
	return strings.HasPrefix(v, "http:") || strings.HasPrefix(v, "https:") || strings.HasPrefix(v, "//") || strings.HasPrefix(v, "data:")
}

func isRelativeOrAlias(v string) bool {
	return strings.HasPrefix(v, ".") || strings.HasPrefix(v, "/") || strings.HasPrefix(v, "@") || strings.HasPrefix(v, "~") || strings.HasPrefix(v, "#")
}

func scanJS(src string, baseLine int) []Import {
	s := &scanner{src: src, line: 1, col: 1, baseLine: baseLine}
	s.run()
	return s.out
}

type scanner struct {
	src      string
	i        int
	line     int
	col      int
	prev     byte
	baseLine int
	out      []Import
}

func (s *scanner) run() {
	for s.i < len(s.src) {
		c := s.peek()
		if c == 0 {
			return
		}
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
			s.next()
			continue
		}
		if c == '/' && s.peekN(1) == '/' {
			s.skipLine()
			continue
		}
		if c == '/' && s.peekN(1) == '*' {
			s.skipBlock()
			continue
		}
		if c == '\'' || c == '"' {
			s.skipQuoted(c)
			s.prev = c
			continue
		}
		if c == '`' {
			s.skipTemplate()
			s.prev = '`'
			continue
		}
		if c == '/' && s.canRegex() {
			s.skipRegex()
			s.prev = '/'
			continue
		}
		if isIdentStart(c) {
			word := s.readIdent()
			switch word {
			case "import":
				s.parseImport()
			case "export":
				s.parseExport()
			case "require":
				s.parseRequire()
			case "new":
				s.parseNewURL()
			case "return", "throw", "case", "typeof", "void", "delete", "in", "of", "yield", "await", "else":
				s.prev = '('
			default:
				if word != "" {
					s.prev = word[len(word)-1]
				}
			}
			continue
		}
		s.prev = c
		s.next()
	}
}

func (s *scanner) parseImport() {
	s.skip()
	if s.peek() == '.' {
		s.parseImportMeta()
		return
	}
	if s.peek() == '(' {
		s.next()
		s.skip()
		val, lit, line, col, ok := s.readSpec()
		if ok {
			kind := KindDynamic
			if lit {
				kind = KindStatic
			}
			s.add(val, line, col, kind, lit)
		}
		return
	}
	if s.peek() == '\'' || s.peek() == '"' || s.peek() == '`' {
		val, lit, line, col, ok := s.readSpec()
		if ok && lit {
			s.add(val, line, col, KindStatic, true)
		}
		return
	}
	if s.consumeFrom() {
		val, lit, line, col, ok := s.readSpec()
		if ok && lit {
			s.add(val, line, col, KindStatic, true)
		} else if ok {
			s.add(val, line, col, KindDynamic, false)
		}
	}
}

func (s *scanner) parseImportMeta() {
	if s.peek() != '.' {
		return
	}
	s.next()
	s.skip()
	if !s.matchIdent("meta") {
		return
	}
	s.skip()
	if s.peek() != '.' {
		return
	}
	s.next()
	s.skip()
	name := ""
	if isIdentStart(s.peek()) {
		name = s.readIdent()
	}
	if name != "glob" && name != "globEager" {
		return
	}
	s.skip()
	if s.peek() != '(' {
		return
	}
	s.next()
	s.skip()
	if s.peek() == '[' {
		s.next()
		for s.i < len(s.src) {
			s.skip()
			if s.peek() == ']' || s.peek() == 0 {
				break
			}
			val, lit, line, col, ok := s.readSpec()
			if ok {
				s.add(val, line, col, KindGlob, lit)
			}
			s.skip()
			if s.peek() == ',' {
				s.next()
			}
		}
		return
	}
	val, lit, line, col, ok := s.readSpec()
	if ok {
		s.add(val, line, col, KindGlob, lit)
	}
}

func (s *scanner) parseExport() {
	s.skip()
	if s.hasIdent("type") {
		// Peek without consuming when this is `export type Name =`.
		save := *s
		s.matchIdent("type")
		s.skip()
		if s.peek() != '*' && s.peek() != '{' {
			*s = save
			return
		}
	}
	if s.peek() != '*' && s.peek() != '{' {
		return
	}
	if s.consumeFrom() {
		val, lit, line, col, ok := s.readSpec()
		if ok && lit {
			s.add(val, line, col, KindStatic, true)
		}
	}
}

func (s *scanner) parseRequire() {
	s.skip()
	if s.peek() == '.' {
		s.next()
		s.skip()
		if s.hasIdent("resolve") {
			s.matchIdent("resolve")
			s.skip()
		}
	}
	if s.peek() != '(' {
		return
	}
	s.next()
	s.skip()
	val, lit, line, col, ok := s.readSpec()
	if !ok {
		return
	}
	kind := KindRequire
	if !lit {
		kind = KindDynamic
	}
	s.add(val, line, col, kind, lit)
}

func (s *scanner) parseNewURL() {
	s.skip()
	if !s.matchIdent("URL") {
		return
	}
	s.skip()
	if s.peek() != '(' {
		return
	}
	s.next()
	s.skip()
	val, lit, line, col, ok := s.readSpec()
	if !ok || !lit {
		return
	}
	s.skip()
	if s.peek() != ',' {
		return
	}
	s.next()
	s.skip()
	if !s.matchIdent("import") {
		return
	}
	s.skip()
	if s.peek() != '.' {
		return
	}
	s.next()
	s.skip()
	if !s.matchIdent("meta") {
		return
	}
	s.skip()
	if s.peek() != '.' {
		return
	}
	s.next()
	s.skip()
	if !s.matchIdent("url") {
		return
	}
	s.add(val, line, col, KindURL, true)
}

func (s *scanner) consumeFrom() bool {
	start := s.i
	depth := 0
	for s.i < len(s.src) && s.i-start < 8000 {
		s.skip()
		if s.i >= len(s.src) {
			return false
		}
		c := s.peek()
		if c == ';' && depth == 0 {
			return false
		}
		if c == '{' || c == '(' || c == '[' {
			depth++
			s.next()
			continue
		}
		if c == '}' || c == ')' || c == ']' {
			if depth > 0 {
				depth--
			}
			s.next()
			continue
		}
		if c == '\'' || c == '"' {
			s.skipQuoted(c)
			continue
		}
		if c == '`' {
			s.skipTemplate()
			continue
		}
		if c == '/' && s.peekN(1) == '/' {
			s.skipLine()
			continue
		}
		if c == '/' && s.peekN(1) == '*' {
			s.skipBlock()
			continue
		}
		if isIdentStart(c) {
			w := s.readIdent()
			if w == "from" && depth == 0 {
				return true
			}
			continue
		}
		s.next()
	}
	return false
}

func (s *scanner) add(val string, line, col int, kind string, lit bool) {
	val = strings.TrimSpace(val)
	if val == "" {
		return
	}
	s.out = append(s.out, Import{
		Specifier: val,
		Line:      s.baseLine + line,
		Column:    col,
		Kind:      kind,
		Literal:   lit,
	})
}

func (s *scanner) readSpec() (val string, lit bool, line, col int, ok bool) {
	s.skip()
	if s.i >= len(s.src) {
		return "", false, 0, 0, false
	}
	line, col = s.line, s.col
	c := s.peek()
	switch c {
	case '\'', '"':
		v, good := s.readQuoted(c)
		return v, true, line, col, good
	case '`':
		v, literal, good := s.readTemplate()
		return v, literal, line, col, good
	default:
		start := s.i
		for s.i < len(s.src) {
			ch := s.peek()
			if ch == ')' || ch == ',' || ch == ']' || ch == '\n' || ch == ';' {
				break
			}
			s.next()
		}
		raw := strings.TrimSpace(s.src[start:s.i])
		if raw == "" {
			return "", false, line, col, false
		}
		return raw, false, line, col, true
	}
}

func (s *scanner) readQuoted(q byte) (string, bool) {
	if s.peek() != q {
		return "", false
	}
	s.next()
	var b strings.Builder
	for s.i < len(s.src) {
		c := s.next()
		if c == '\\' {
			if s.i >= len(s.src) {
				return "", false
			}
			n := s.next()
			switch n {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case '\\', '\'', '"', '`':
				b.WriteByte(n)
			default:
				b.WriteByte(n)
			}
			continue
		}
		if c == q {
			return b.String(), true
		}
		b.WriteByte(c)
	}
	return "", false
}

func (s *scanner) skipQuoted(q byte) {
	_, _ = s.readQuoted(q)
}

func (s *scanner) readTemplate() (string, bool, bool) {
	if s.peek() != '`' {
		return "", false, false
	}
	s.next()
	start := s.i
	lit := true
	for s.i < len(s.src) {
		c := s.peek()
		if c == '\\' {
			s.next()
			if s.i < len(s.src) {
				s.next()
			}
			continue
		}
		if c == '`' {
			raw := s.src[start:s.i]
			s.next()
			return raw, lit, true
		}
		if c == '$' && s.peekN(1) == '{' {
			lit = false
			s.next()
			s.next()
			depth := 1
			for s.i < len(s.src) && depth > 0 {
				ch := s.next()
				if ch == '{' {
					depth++
				} else if ch == '}' {
					depth--
				}
			}
			continue
		}
		s.next()
	}
	return "", false, false
}

func (s *scanner) skipTemplate() {
	_, _, _ = s.readTemplate()
}

func (s *scanner) skipLine() {
	for s.i < len(s.src) && s.peek() != '\n' {
		s.next()
	}
}

func (s *scanner) skipBlock() {
	s.next()
	s.next()
	for s.i+1 < len(s.src) && !(s.peek() == '*' && s.peekN(1) == '/') {
		s.next()
	}
	if s.i+1 < len(s.src) {
		s.next()
		s.next()
	}
}

func (s *scanner) skipRegex() {
	s.next()
	for s.i < len(s.src) {
		c := s.next()
		if c == '\\' {
			if s.i < len(s.src) {
				s.next()
			}
			continue
		}
		if c == '/' {
			break
		}
		if c == '\n' {
			break
		}
		if c == '[' {
			for s.i < len(s.src) && s.peek() != ']' && s.peek() != '\n' {
				if s.peek() == '\\' {
					s.next()
				}
				if s.i < len(s.src) {
					s.next()
				}
			}
		}
	}
	for s.i < len(s.src) && isIdentByte(s.peek()) {
		s.next()
	}
}

func (s *scanner) canRegex() bool {
	switch s.prev {
	case 0, '(', '[', '{', ',', ';', '=', ':', '!', '&', '|', '?', '+', '-', '~', '*', '%', '^', '<', '>', '\n':
		return true
	default:
		return false
	}
}

func (s *scanner) skip() {
	for s.i < len(s.src) {
		c := s.peek()
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
			s.next()
			continue
		}
		if c == '/' && s.peekN(1) == '/' {
			s.skipLine()
			continue
		}
		if c == '/' && s.peekN(1) == '*' {
			s.skipBlock()
			continue
		}
		return
	}
}

func (s *scanner) readIdent() string {
	start := s.i
	for s.i < len(s.src) && isIdentByte(s.peek()) {
		s.next()
	}
	return s.src[start:s.i]
}

func (s *scanner) hasIdent(want string) bool {
	if s.i+len(want) > len(s.src) || s.src[s.i:s.i+len(want)] != want {
		return false
	}
	if s.i+len(want) < len(s.src) && isIdentByte(s.src[s.i+len(want)]) {
		return false
	}
	return true
}

func (s *scanner) matchIdent(want string) bool {
	if !s.hasIdent(want) {
		return false
	}
	for range len(want) {
		s.next()
	}
	return true
}

func (s *scanner) peek() byte {
	if s.i >= len(s.src) {
		return 0
	}
	return s.src[s.i]
}

func (s *scanner) peekN(n int) byte {
	if s.i+n >= len(s.src) {
		return 0
	}
	return s.src[s.i+n]
}

func (s *scanner) next() byte {
	if s.i >= len(s.src) {
		return 0
	}
	c := s.src[s.i]
	s.i++
	if c == '\n' {
		s.line++
		s.col = 1
		return c
	}
	s.col++
	return c
}

func hasHere(s *scanner, lit string) bool {
	if s.i+len(lit) > len(s.src) {
		return false
	}
	return s.src[s.i:s.i+len(lit)] == lit
}

func isIdentStart(c byte) bool {
	return c == '_' || c == '$' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func isIdentByte(c byte) bool {
	return isIdentStart(c) || c >= '0' && c <= '9'
}

func hasPrefixFold(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	return strings.EqualFold(s[:len(prefix)], prefix)
}

func indexFold(s, sub string) int {
	if sub == "" {
		return 0
	}
	return strings.Index(strings.ToLower(s), strings.ToLower(sub))
}
