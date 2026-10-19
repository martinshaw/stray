// Package resolve decides whether an import specifier lands on a real file.
package resolve

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/martinshaw/stray/internal/glob"
	"github.com/martinshaw/stray/internal/model"
	"github.com/martinshaw/stray/internal/parse"
	"github.com/martinshaw/stray/internal/project"
)

// Resolver caches package and directory lookups for one scan.
type Resolver struct {
	P         *project.Project
	CaseCheck bool
	pkgs      map[string]*pkgInfo
	dirs      map[string][]string
	suffixes  []string
	exts      []string
	ignored   int
}

type pkgInfo struct {
	Main    string
	Module  string
	Exports json.RawMessage
	Missing bool
}

type outcome struct {
	found        bool
	path         string
	code         string
	detail       string
	attempts     []string
	drift        bool
	driftDetail  string
	caseMismatch bool
	caseDetail   string
	escapes      bool
	escapeDetail string
	undeclared   bool
	undeclaredOn string
	builtin      bool
}

// New builds a resolver for a loaded project.
func New(p *project.Project, caseCheck bool) *Resolver {
	return &Resolver{
		P:         p,
		CaseCheck: caseCheck,
		pkgs:      map[string]*pkgInfo{},
		dirs:      map[string][]string{},
	}
}

// File reports problems in one source file. ignored counts framework virtual imports.
func (r *Resolver) File(file, src string) ([]model.Problem, int) {
	before := r.ignored
	rel := r.rel(file)
	var problems []model.Problem
	for _, imp := range parse.FileImports(file, src) {
		problems = append(problems, r.problemsFor(file, rel, imp)...)
	}
	return problems, r.ignored - before
}

func (r *Resolver) problemsFor(file, rel string, imp parse.Import) []model.Problem {
	base := model.Problem{
		File:      rel,
		Line:      imp.Line,
		Column:    imp.Column,
		Specifier: imp.Specifier,
		Kind:      imp.Kind,
	}
	if !imp.Literal {
		p := base
		p.Code = "dynamic"
		p.Severity = model.Warning
		p.Kind = parse.KindDynamic
		p.Detail = "non-literal import is not checked"
		return []model.Problem{p}
	}
	spec := imp.Specifier
	if isExternal(spec) {
		return nil
	}
	path, _, virtual := splitSpec(spec)
	if virtual {
		if r.allowVirtual(path) {
			r.ignored++
			return nil
		}
		p := base
		p.Code = "virtual"
		p.Severity = model.Warning
		p.Detail = "virtual module was not verified because no Vite or Nuxt config was found"
		return []model.Problem{p}
	}
	if imp.Kind == parse.KindGlob {
		out := r.resolveGlob(file, path)
		if out.code == "" && !out.found {
			return nil
		}
		if out.code == "" {
			return nil
		}
		p := base
		p.Code = out.code
		p.Severity = model.Warning
		p.Detail = out.detail
		p.Attempts = out.attempts
		return []model.Problem{p}
	}
	out := r.resolve(file, path)
	return r.outcomes(base, out)
}

func (r *Resolver) outcomes(base model.Problem, out outcome) []model.Problem {
	var problems []model.Problem
	if !out.found {
		p := base
		p.Code = out.code
		if p.Code == "" {
			p.Code = "unresolved"
		}
		p.Severity = model.Error
		p.Detail = out.detail
		p.Attempts = out.attempts
		if out.drift {
			p.Detail = joinDetail(out.driftDetail, p.Detail)
		}
		problems = append(problems, p)
		return problems
	}
	if out.drift {
		p := base
		p.Code = "alias-drift"
		p.Severity = model.Warning
		p.Detail = out.driftDetail
		problems = append(problems, p)
	}
	if out.caseMismatch {
		p := base
		p.Code = "case-mismatch"
		p.Severity = caseSeverity()
		p.Detail = out.caseDetail
		problems = append(problems, p)
	}
	if out.escapes {
		p := base
		p.Code = "escapes-root"
		p.Severity = model.Warning
		p.Detail = out.escapeDetail
		problems = append(problems, p)
	}
	if out.undeclared {
		p := base
		p.Code = "undeclared-package"
		p.Severity = model.Warning
		p.Detail = out.undeclaredOn
		problems = append(problems, p)
	}
	return problems
}

func joinDetail(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	return a + "; " + b
}

func caseSeverity() model.Severity {
	switch runtime.GOOS {
	case "linux", "freebsd", "openbsd", "netbsd":
		return model.Error
	default:
		return model.Warning
	}
}

func (r *Resolver) allowVirtual(spec string) bool {
	if r.P.Vite || r.P.Bundler {
		if strings.HasPrefix(spec, "virtual:") || strings.HasPrefix(spec, "\x00") {
			return true
		}
	}
	if r.P.Nuxt {
		switch spec {
		case "#app", "#imports", "#components", "#build", "#vue-router", "#mdc", "#unhead", "#content":
			return true
		}
		if strings.HasPrefix(spec, "#build/") || strings.HasPrefix(spec, "#components/") {
			return true
		}
	}
	return false
}

func (r *Resolver) resolve(fromFile, spec string) outcome {
	spec = filepath.ToSlash(strings.TrimSpace(spec))
	if spec == "" {
		return outcome{code: "unresolved", detail: "empty specifier"}
	}
	scope, ts := r.P.For(fromFile)
	r.suffixes = nil
	r.exts = scope.Exts
	if ts != nil {
		r.suffixes = ts.Suffixes
	}
	if r.allowVirtual(spec) {
		r.ignored++
		return outcome{found: true, builtin: true, path: spec}
	}
	if strings.HasPrefix(spec, ".") {
		return r.resolveRelative(fromFile, spec)
	}
	if strings.HasPrefix(spec, "/") && (r.P.Mode == "bundler" || r.P.Bundler) {
		abs := filepath.Join(r.P.Root, filepath.FromSlash(strings.TrimPrefix(spec, "/")))
		out := r.finishCandidates([]string{abs})
		if !out.found {
			out.code = "unresolved"
			if out.detail == "" {
				out.detail = "no file at " + r.rel(abs)
			}
		} else if outside(r.P.Root, out.path) {
			out.escapes = true
			out.escapeDetail = "resolved path is outside the project (" + r.rel(out.path) + ")"
		}
		return out
	}
	if strings.HasPrefix(spec, "#") && !r.allowVirtual(spec) {
		return r.resolveImportMap(fromFile, spec, scope)
	}

	bHit, bOK := bestAlias(scope.Aliases, spec, isBundlerSource)
	var tHit aliasHit
	tOK := false
	if ts != nil {
		tHit, tOK = bestAlias(ts.Aliases, spec, func(src string) bool { return src == "tsconfig" })
	}
	drift := bOK && tOK && primary(bHit) != primary(tHit)
	if bOK || tOK {
		use := tHit
		fromBundler := false
		if bOK {
			use = bHit
			fromBundler = true
		}
		out := r.finishCandidates(use.paths)
		if drift {
			out.drift = true
			out.driftDetail = "tsconfig points at " + r.rel(primary(tHit)) + ", " + bHit.alias.Source + " points at " + r.rel(primary(bHit))
		}
		if out.found {
			if outside(r.P.Root, out.path) {
				out.escapes = true
				out.escapeDetail = "resolved path is outside the project (" + r.rel(out.path) + ")"
			}
			return out
		}
		if fromBundler {
			out.code = "unresolved"
			if out.detail == "" {
				out.detail = "no file for " + spec + " (alias " + use.alias.Prefix + " from " + filepath.Base(use.alias.Origin) + ")"
			}
			return out
		}
		bare := r.resolveBare(fromFile, spec, scope)
		if bare.found || bare.code == "exports-denied" {
			if drift {
				bare.drift = true
				bare.driftDetail = out.driftDetail
			}
			return bare
		}
		out.code = "unresolved"
		if out.detail == "" {
			out.detail = "no file for " + spec + " (alias " + use.alias.Prefix + " from " + filepath.Base(use.alias.Origin) + ")"
		}
		return out
	}
	if ts != nil && ts.BaseURL != "" && !strings.Contains(spec, ":") {
		cand := filepath.Join(ts.BaseURL, filepath.FromSlash(spec))
		out := r.finishCandidates([]string{cand})
		if out.found {
			return out
		}
	}
	for _, dir := range scope.ExtraMod {
		cand := filepath.Join(dir, filepath.FromSlash(spec))
		out := r.finishCandidates([]string{cand})
		if out.found {
			return out
		}
	}
	return r.resolveBare(fromFile, spec, scope)
}

func (r *Resolver) resolveRelative(fromFile, spec string) outcome {
	abs := filepath.Join(filepath.Dir(fromFile), filepath.FromSlash(spec))
	out := r.finishCandidates([]string{abs})
	if !out.found {
		out.code = "unresolved"
		if out.detail == "" {
			out.detail = "no file at " + r.rel(abs)
		}
		return out
	}
	if outside(r.P.Root, out.path) {
		out.escapes = true
		out.escapeDetail = "resolved path is outside the project (" + r.rel(out.path) + ")"
	}
	return out
}

func (r *Resolver) resolveGlob(fromFile, pattern string) outcome {
	dir, rest, ok := r.globDir(fromFile, pattern)
	if !ok {
		return outcome{code: "empty-glob", detail: "glob matched 0 files"}
	}
	matches := 0
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == "node_modules" || d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return nil
		}
		if glob.Match(rest, filepath.ToSlash(rel)) {
			matches++
			if matches > 5000 {
				return fs.SkipAll
			}
		}
		return nil
	})
	if matches == 0 {
		return outcome{code: "empty-glob", detail: "glob matched 0 files"}
	}
	return outcome{found: true, path: dir}
}

func (r *Resolver) globDir(fromFile, pattern string) (string, string, bool) {
	pattern = filepath.ToSlash(pattern)
	parts := strings.Split(pattern, "/")
	var static, globby []string
	for i, part := range parts {
		if strings.ContainsAny(part, "*?[") {
			globby = parts[i:]
			break
		}
		static = append(static, part)
	}
	if len(globby) == 0 {
		globby = []string{"*"}
	}
	rest := strings.Join(globby, "/")
	staticSpec := strings.Join(static, "/")
	if staticSpec == "" || staticSpec == "." {
		return filepath.Dir(fromFile), rest, true
	}
	if strings.HasPrefix(staticSpec, ".") {
		abs := filepath.Join(filepath.Dir(fromFile), filepath.FromSlash(staticSpec))
		st, err := os.Stat(abs)
		if err == nil && st.IsDir() {
			return abs, rest, true
		}
		return abs, rest, false
	}
	if strings.HasPrefix(pattern, "/") {
		abs := filepath.Join(r.P.Root, filepath.FromSlash(strings.TrimPrefix(staticSpec, "/")))
		st, err := os.Stat(abs)
		if err == nil && st.IsDir() {
			return abs, rest, true
		}
		return abs, rest, false
	}
	if dir, ok := r.aliasDir(staticSpec); ok {
		return dir, rest, true
	}
	return "", rest, false
}

func (r *Resolver) aliasDir(spec string) (string, bool) {
	// A glob prefix has no importing file; use the root scope.
	scope, ts := r.P.For(filepath.Join(r.P.Root, "_.js"))
	if h, ok := bestAlias(scope.Aliases, spec, isBundlerSource); ok {
		for _, p := range h.paths {
			if st, err := os.Stat(p); err == nil && st.IsDir() {
				return p, true
			}
		}
	}
	if ts != nil {
		if h, ok := bestAlias(ts.Aliases, spec, func(s string) bool { return s == "tsconfig" }); ok {
			for _, p := range h.paths {
				if st, err := os.Stat(p); err == nil && st.IsDir() {
					return p, true
				}
			}
		}
	}
	return "", false
}

type aliasHit struct {
	alias   project.Alias
	paths   []string
	specLen int
}

func bestAlias(aliases []project.Alias, spec string, keep func(string) bool) (aliasHit, bool) {
	var hit aliasHit
	found := false
	for _, alias := range aliases {
		if keep != nil && !keep(alias.Source) {
			continue
		}
		h, ok := matchAlias(alias, spec)
		if !ok {
			continue
		}
		if !found || h.specLen > hit.specLen {
			hit = h
			found = true
		}
	}
	return hit, found
}

func matchAlias(alias project.Alias, spec string) (aliasHit, bool) {
	if alias.Exact {
		if spec != alias.Prefix {
			return aliasHit{}, false
		}
		return aliasHit{alias: alias, paths: append([]string{}, alias.Targets...), specLen: len(alias.Prefix)}, true
	}
	if alias.Wildcard {
		capture, ok := matchStar(alias.Prefix+"*"+alias.Suffix, spec)
		if !ok {
			return aliasHit{}, false
		}
		var paths []string
		for _, target := range alias.Targets {
			paths = append(paths, substitute(target, capture))
		}
		return aliasHit{alias: alias, paths: paths, specLen: len(alias.Prefix)}, true
	}
	if spec == alias.Prefix || strings.HasPrefix(spec, alias.Prefix+"/") || (strings.HasSuffix(alias.Prefix, "/") && strings.HasPrefix(spec, alias.Prefix)) {
		rest := strings.TrimPrefix(spec, alias.Prefix)
		var paths []string
		for _, target := range alias.Targets {
			paths = append(paths, joinAlias(target, rest))
		}
		return aliasHit{alias: alias, paths: paths, specLen: len(alias.Prefix)}, true
	}
	return aliasHit{}, false
}

func primary(hit aliasHit) string {
	if len(hit.paths) == 0 {
		return ""
	}
	return filepath.Clean(hit.paths[0])
}

func substitute(target, capture string) string {
	capture = filepath.ToSlash(capture)
	if strings.Contains(target, "*") {
		return filepath.Clean(filepath.FromSlash(strings.Replace(target, "*", capture, 1)))
	}
	return joinAlias(target, capture)
}

func joinAlias(target, remainder string) string {
	remainder = strings.TrimPrefix(filepath.ToSlash(remainder), "/")
	if remainder == "" {
		return filepath.Clean(target)
	}
	return filepath.Clean(filepath.Join(target, filepath.FromSlash(remainder)))
}

func isBundlerSource(source string) bool {
	switch source {
	case "vite", "webpack", "nuxt", "bundler":
		return true
	default:
		return false
	}
}

func (r *Resolver) finishCandidates(paths []string) outcome {
	var attempts []string
	last := ""
	for _, candidate := range paths {
		last = candidate
		found, requested, exact, tried, ok := r.findFile(candidate)
		attempts = append(attempts, tried...)
		if !ok {
			continue
		}
		out := outcome{found: true, path: found, attempts: attempts}
		if !exact {
			out.caseMismatch = true
			out.caseDetail = "import uses " + r.rel(requested) + " but the file on disk is " + r.rel(found)
		}
		return out
	}
	detail := "no file at " + r.rel(last)
	if last == "" {
		detail = "no candidate path"
	}
	return outcome{detail: detail, attempts: attempts}
}

func (r *Resolver) findFile(candidate string) (found, requested string, exact bool, attempts []string, ok bool) {
	candidate = filepath.Clean(candidate)
	exts := r.extList()
	var cands []string
	if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
		cands = append(cands, candidate)
	}
	ext := filepath.Ext(candidate)
	if knownExt(ext) {
		cands = append(cands, candidate)
		if r.P.Mode == "bundler" || r.P.Mode == "node16" {
			for _, alt := range tsAlternates[ext] {
				cands = append(cands, strings.TrimSuffix(candidate, ext)+alt)
			}
		}
	} else {
		cands = append(cands, r.withSuffixes(candidate, exts)...)
		for _, e := range exts {
			cands = append(cands, filepath.Join(candidate, "index"+e))
		}
	}
	seen := map[string]bool{}
	for _, cand := range cands {
		if seen[cand] {
			continue
		}
		seen[cand] = true
		actual, precise, exists := r.lookup(cand)
		if !exists {
			attempts = append(attempts, r.rel(cand))
			continue
		}
		return actual, cand, precise, attempts, true
	}
	return "", "", true, attempts, false
}

func (r *Resolver) withSuffixes(base string, exts []string) []string {
	sufs := r.suffixes
	if len(sufs) == 0 {
		sufs = []string{""}
	}
	var out []string
	for _, suf := range sufs {
		for _, ext := range exts {
			out = append(out, base+suf+ext)
		}
	}
	return out
}

func (r *Resolver) extList() []string {
	if len(r.exts) > 0 {
		return r.exts
	}
	return defaultExts
}

func (r *Resolver) lookup(path string) (actual string, exact bool, exists bool) {
	if !r.CaseCheck {
		st, err := os.Stat(path)
		if err != nil || st.IsDir() {
			return "", true, false
		}
		return path, true, true
	}
	actual, exact, ok := r.actualCase(path)
	if !ok {
		return "", true, false
	}
	st, err := os.Stat(actual)
	if err != nil || st.IsDir() {
		return "", true, false
	}
	return actual, exact, true
}

func (r *Resolver) actualCase(path string) (string, bool, bool) {
	path = filepath.Clean(path)
	vol := filepath.VolumeName(path)
	rest := strings.TrimPrefix(path, vol)
	rest = strings.TrimPrefix(rest, string(filepath.Separator))
	if rest == "" {
		return path, true, true
	}
	parts := strings.Split(rest, string(filepath.Separator))
	cur := vol
	if vol == "" {
		cur = string(filepath.Separator)
	} else if !strings.HasSuffix(cur, string(filepath.Separator)) {
		cur += string(filepath.Separator)
	}
	exact := true
	for _, part := range parts {
		if part == "" {
			continue
		}
		names := r.dirNames(cur)
		chosen := ""
		precise := false
		for _, name := range names {
			if name == part {
				chosen = name
				precise = true
				break
			}
		}
		if chosen == "" {
			for _, name := range names {
				if strings.EqualFold(name, part) {
					chosen = name
					break
				}
			}
		}
		if chosen == "" {
			return "", true, false
		}
		if !precise {
			exact = false
		}
		cur = filepath.Join(cur, chosen)
	}
	return cur, exact, true
}

func (r *Resolver) dirNames(dir string) []string {
	if names, ok := r.dirs[dir]; ok {
		return names
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		r.dirs[dir] = nil
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	r.dirs[dir] = names
	return names
}

func (r *Resolver) resolveBare(fromFile, spec string, scope project.Scope) outcome {
	if isBuiltin(spec) {
		return outcome{found: true, builtin: true, path: spec}
	}
	name, sub, ok := splitPkg(spec)
	if !ok {
		return outcome{code: "unresolved", detail: "invalid specifier " + spec}
	}
	if dir, ok := r.P.WorkspaceNames[name]; ok {
		return r.finishPackage(dir, name, sub, scope, fromFile, true)
	}
	if scope.Name != "" && scope.Name == name && scope.Dir != "" {
		return r.finishPackage(scope.Dir, name, sub, scope, fromFile, true)
	}
	dir := findPkgDir(filepath.Dir(fromFile), r.P.Root, name)
	if dir == "" {
		detail := "package " + name + " is not installed"
		if !r.declared(name, fromFile) {
			detail += " and is not listed in package.json"
		}
		return outcome{code: "unresolved", detail: detail}
	}
	return r.finishPackage(dir, name, sub, scope, fromFile, false)
}

func (r *Resolver) resolveImportMap(fromFile, spec string, scope project.Scope) outcome {
	if len(strings.TrimSpace(string(scope.Imports))) == 0 {
		return outcome{code: "unresolved", detail: "package imports do not define " + spec}
	}
	target, status := resolveExports(scope.Imports, spec, r.conditions(scope))
	if status != "ok" {
		return outcome{code: "unresolved", detail: "package imports do not define " + spec}
	}
	abs := target
	if !filepath.IsAbs(target) {
		abs = filepath.Join(scope.Dir, filepath.FromSlash(target))
	}
	out := r.finishCandidates([]string{abs})
	if !out.found {
		out.code = "unresolved"
		out.detail = "import " + spec + " points at " + r.rel(abs) + ", which is missing"
	}
	return out
}

func (r *Resolver) finishPackage(dir, name, sub string, scope project.Scope, fromFile string, self bool) outcome {
	info := r.readPkg(dir)
	if r.P.Mode != "classic" && len(strings.TrimSpace(string(info.Exports))) > 0 {
		target, status := resolveExports(info.Exports, sub, r.conditions(scope))
		if status != "ok" {
			return outcome{code: "exports-denied", detail: "package " + name + " does not export " + sub}
		}
		abs := filepath.Join(dir, filepath.FromSlash(strings.TrimPrefix(target, "./")))
		out := r.finishCandidates([]string{abs})
		if !out.found {
			out.code = "unresolved"
			out.detail = "package " + name + " exports " + sub + " but " + r.rel(abs) + " is missing"
			return out
		}
		r.noteUndeclared(&out, name, fromFile, self)
		return out
	}
	var base string
	if sub == "." {
		if (r.P.Mode == "bundler") && info.Module != "" {
			base = filepath.Join(dir, filepath.FromSlash(info.Module))
		} else if info.Main != "" {
			base = filepath.Join(dir, filepath.FromSlash(info.Main))
		} else {
			base = filepath.Join(dir, "index")
		}
	} else {
		base = filepath.Join(dir, filepath.FromSlash(strings.TrimPrefix(sub, "./")))
	}
	out := r.finishCandidates([]string{base})
	if !out.found {
		out.code = "unresolved"
		if out.detail == "" {
			out.detail = "no file for " + r.rel(base)
		}
		return out
	}
	r.noteUndeclared(&out, name, fromFile, self)
	return out
}

func (r *Resolver) noteUndeclared(out *outcome, name, fromFile string, self bool) {
	if self || out.builtin || !out.found {
		return
	}
	if r.declared(name, fromFile) {
		return
	}
	out.undeclared = true
	out.undeclaredOn = "resolved " + r.rel(out.path) + "; " + name + " is not listed in package.json"
}

func (r *Resolver) declared(name, fromFile string) bool {
	if _, ok := r.P.WorkspaceNames[name]; ok {
		return true
	}
	for _, scope := range r.P.Scopes {
		if !project.DirPrefix(scope.Dir, fromFile) {
			continue
		}
		if scope.Deps[name] {
			return true
		}
	}
	return false
}

func (r *Resolver) conditions(scope project.Scope) []string {
	if r.P.Mode == "bundler" {
		return []string{"import", "module", "browser", "development", "default"}
	}
	if scope.Type == "module" || r.P.Mode == "node16" && scope.Type != "commonjs" {
		if scope.Type == "commonjs" {
			return []string{"require", "node", "default"}
		}
		return []string{"import", "node", "default"}
	}
	return []string{"require", "node", "default"}
}

func (r *Resolver) readPkg(dir string) *pkgInfo {
	if info, ok := r.pkgs[dir]; ok {
		return info
	}
	info := &pkgInfo{}
	buf, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		info.Missing = true
		r.pkgs[dir] = info
		return info
	}
	var doc struct {
		Main    string          `json:"main"`
		Module  string          `json:"module"`
		Exports json.RawMessage `json:"exports"`
	}
	if json.Unmarshal(buf, &doc) == nil {
		info.Main = doc.Main
		info.Module = doc.Module
		info.Exports = doc.Exports
	}
	r.pkgs[dir] = info
	return info
}

func findPkgDir(start, root, name string) string {
	dir := filepath.Clean(start)
	root = filepath.Clean(root)
	for {
		cand := filepath.Join(dir, "node_modules", filepath.FromSlash(name))
		if st, err := os.Stat(cand); err == nil && st.IsDir() {
			return cand
		}
		if dir == root {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

func splitPkg(spec string) (string, string, bool) {
	if spec == "" || strings.HasPrefix(spec, ".") || strings.HasPrefix(spec, "/") {
		return "", "", false
	}
	if strings.HasPrefix(spec, "@") {
		parts := strings.SplitN(spec, "/", 3)
		if len(parts) < 2 || parts[0] == "@" || parts[1] == "" {
			return "", "", false
		}
		name := parts[0] + "/" + parts[1]
		sub := "."
		if len(parts) == 3 {
			sub = "./" + parts[2]
		}
		return name, sub, true
	}
	parts := strings.SplitN(spec, "/", 2)
	if parts[0] == "" {
		return "", "", false
	}
	if len(parts) == 1 {
		return parts[0], ".", true
	}
	return parts[0], "./" + parts[1], true
}

func splitSpec(spec string) (string, string, bool) {
	if strings.HasPrefix(spec, "virtual:") || strings.HasPrefix(spec, "\x00") {
		return spec, "", true
	}
	if i := strings.LastIndex(spec, "!"); i >= 0 {
		spec = spec[i+1:]
	}
	// #util is a package import, not a URL fragment.
	if strings.HasPrefix(spec, "#") {
		return spec, "", false
	}
	if i := strings.IndexAny(spec, "?#"); i >= 0 {
		return spec[:i], spec[i:], strings.HasPrefix(spec[:i], "virtual:")
	}
	return spec, "", false
}

func isExternal(spec string) bool {
	return strings.HasPrefix(spec, "data:") || strings.HasPrefix(spec, "http:") || strings.HasPrefix(spec, "https:") || strings.HasPrefix(spec, "//")
}

func outside(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return true
	}
	return rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func (r *Resolver) rel(path string) string {
	if path == "" {
		return ""
	}
	if !filepath.IsAbs(path) {
		return filepath.ToSlash(path)
	}
	rel, err := filepath.Rel(r.P.Root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

var defaultExts = []string{".ts", ".tsx", ".mts", ".cts", ".js", ".jsx", ".mjs", ".cjs", ".vue", ".svelte", ".json"}

var tsAlternates = map[string][]string{
	".js":  {".ts", ".tsx"},
	".jsx": {".tsx"},
	".mjs": {".mts"},
	".cjs": {".cts"},
}

func knownExt(ext string) bool {
	switch strings.ToLower(ext) {
	case ".ts", ".tsx", ".mts", ".cts", ".js", ".jsx", ".mjs", ".cjs", ".vue", ".svelte",
		".json", ".css", ".scss", ".sass", ".less", ".svg", ".png", ".webp", ".gif", ".jpg", ".jpeg",
		".woff", ".woff2", ".ttf", ".html", ".md":
		return true
	default:
		return false
	}
}
