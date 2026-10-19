// Package config reads tsconfig, package.json, Vite, webpack, and Nuxt without
// importing those tools. Node is used only when a bundler alias cannot be
// evaluated from source and evaluation is enabled.
package config

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/martinshaw/stray/internal/glob"
	"github.com/martinshaw/stray/internal/jsonc"
	"github.com/martinshaw/stray/internal/project"
)

// Options controls how a project is loaded.
type Options struct {
	Eval       bool
	Probe      bool
	Resolution string
}

// Load reads project configuration from root.
func Load(root string, opt Options) (*project.Project, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	root = abs
	st, err := os.Stat(root)
	if err != nil || !st.IsDir() {
		return nil, err
	}
	p := &project.Project{
		Root:           root,
		WorkspaceNames: map[string]string{},
		Mode:           "classic",
	}
	rootPkg := readPkgFile(filepath.Join(root, "package.json"))
	workspaces := workspaceDirs(root, rootPkg)
	dirs := append([]string{root}, workspaces...)
	seenDir := map[string]bool{}
	var scopes []project.Scope
	for _, dir := range dirs {
		dir = filepath.Clean(dir)
		if seenDir[dir] {
			continue
		}
		seenDir[dir] = true
		scopes = append(scopes, loadScope(p, dir, opt))
	}
	inherit(scopes)
	sort.Slice(scopes, func(i, j int) bool {
		return len(scopes[i].Dir) > len(scopes[j].Dir)
	})
	p.Scopes = scopes
	if p.Vite || p.Webpack || p.Nuxt {
		p.Bundler = true
		p.Mode = "bundler"
	} else if mode := rootMode(scopes, root); mode != "" {
		p.Mode = mode
	}
	switch opt.Resolution {
	case "bundler", "node16", "classic":
		p.Mode = opt.Resolution
	}
	if opt.Probe {
		p.Tools = probeTools()
	}
	sort.Strings(p.Configs)
	return p, nil
}

func rootMode(scopes []project.Scope, root string) string {
	var pkgType string
	var mode string
	for _, s := range scopes {
		if s.Dir == root {
			pkgType = s.Type
		}
		for _, ts := range s.TS {
			if ts.Dir == root && !ts.Solution && ts.Mode != "" {
				mode = ts.Mode
			}
		}
	}
	if mode != "" {
		return mode
	}
	if pkgType == "module" {
		return "node16"
	}
	return "classic"
}

func loadScope(p *project.Project, dir string, opt Options) project.Scope {
	scope := project.Scope{Dir: dir, Deps: map[string]bool{}}
	pkgPath := filepath.Join(dir, "package.json")
	if st, err := os.Stat(pkgPath); err == nil && !st.IsDir() {
		raw := readPkgFile(pkgPath)
		scope.Name = raw.Name
		scope.Type = raw.Type
		scope.Imports = raw.Imports
		if raw.Name != "" {
			p.WorkspaceNames[raw.Name] = dir
		}
		addDeps(scope.Deps, raw.Dependencies)
		addDeps(scope.Deps, raw.DevDependencies)
		addDeps(scope.Deps, raw.PeerDependencies)
		addDeps(scope.Deps, raw.OptionalDependencies)
		p.Configs = append(p.Configs, rel(p.Root, pkgPath))
	}
	scope.TS, p.Notes = appendTS(p, dir, p.Notes)
	if vite := findFile(dir, "vite.config.ts", "vite.config.mts", "vite.config.js", "vite.config.mjs", "vite.config.cjs"); vite != "" {
		p.Vite = true
		p.BundlerName = "vite"
		aliases, notes := bundlerAliases(vite, dir, p.Root, "vite", opt)
		scope.Aliases = append(aliases, scope.Aliases...)
		p.Notes = append(p.Notes, notes...)
		scope.Exts = mergeExts(scope.Exts, parseStringArray(mustRead(vite), "extensions"))
		p.Configs = append(p.Configs, rel(p.Root, vite))
	}
	if wp := findFile(dir, "webpack.config.js", "webpack.config.ts", "webpack.config.mjs", "webpack.config.cjs"); wp != "" {
		p.Webpack = true
		if p.BundlerName == "" {
			p.BundlerName = "webpack"
		}
		aliases, notes := bundlerAliases(wp, dir, p.Root, "webpack", opt)
		scope.Aliases = append(aliases, scope.Aliases...)
		p.Notes = append(p.Notes, notes...)
		scope.Exts = mergeExts(scope.Exts, parseStringArray(mustRead(wp), "extensions"))
		for _, m := range parseStringArray(mustRead(wp), "modules") {
			if m == "node_modules" {
				continue
			}
			if filepath.IsAbs(m) {
				scope.ExtraMod = append(scope.ExtraMod, m)
			} else {
				scope.ExtraMod = append(scope.ExtraMod, filepath.Join(dir, m))
			}
		}
		p.Configs = append(p.Configs, rel(p.Root, wp))
	}
	if nuxt := findFile(dir, "nuxt.config.ts", "nuxt.config.js", "nuxt.config.mjs"); nuxt != "" {
		p.Nuxt = true
		if p.BundlerName == "" {
			p.BundlerName = "nuxt"
		}
		src := mustRead(nuxt)
		aliases, notes := bundlerAliases(nuxt, dir, p.Root, "nuxt", opt)
		aliases = addNuxtAliases(src, dir, p.Root, nuxt, aliases)
		scope.Aliases = append(aliases, scope.Aliases...)
		p.Notes = append(p.Notes, notes...)
		p.Configs = append(p.Configs, rel(p.Root, nuxt))
	}
	return scope
}

func bundlerAliases(path, dir, root, source string, opt Options) ([]project.Alias, []string) {
	src := mustRead(path)
	aliases, notes := extractAliases(src, dir, root, source, path)
	unevaluated := false
	for _, n := range notes {
		if strings.Contains(n, "could not evaluate") || strings.Contains(n, "could not read") || strings.Contains(n, "unsupported regex") {
			unevaluated = true
		}
	}
	if opt.Eval && (len(aliases) == 0 || unevaluated) {
		if evaled, evalNotes, err := evalWithNode(path); err == nil && len(evaled) > 0 {
			for i := range evaled {
				evaled[i].Source = source
				evaled[i].Origin = path
				evaled[i].PrefixMatch = true
			}
			notes = append(evalNotes, "evaluated "+filepath.Base(path)+" with node")
			return evaled, notes
		}
	}
	return aliases, notes
}

func appendTS(p *project.Project, dir string, notes []string) ([]project.TSConfig, []string) {
	main := findFile(dir, "tsconfig.json", "jsconfig.json", "tsconfig.app.json")
	if main == "" {
		return nil, notes
	}
	seen := map[string]bool{}
	cfgs := []project.TSConfig{}
	cfg, refs, n := loadTS(main, dir, p.Root, 0, seen)
	notes = append(notes, n...)
	cfgs = append(cfgs, cfg)
	p.Configs = append(p.Configs, rel(p.Root, main))
	for _, ref := range refs {
		ref = existingTS(ref)
		if ref == "" || seen[ref] {
			continue
		}
		child, _, n := loadTS(ref, dir, p.Root, 0, seen)
		notes = append(notes, n...)
		cfgs = append(cfgs, child)
		p.Configs = append(p.Configs, rel(p.Root, ref))
	}
	if cfg.Solution {
		for _, extra := range []string{"tsconfig.app.json", "tsconfig.node.json"} {
			path := filepath.Join(dir, extra)
			if seen[path] {
				continue
			}
			if st, err := os.Stat(path); err != nil || st.IsDir() {
				continue
			}
			child, _, n := loadTS(path, dir, p.Root, 0, seen)
			notes = append(notes, n...)
			cfgs = append(cfgs, child)
			p.Configs = append(p.Configs, rel(p.Root, path))
		}
	}
	return cfgs, notes
}

type tsLoaded struct {
	cfg  project.TSConfig
	refs []string
}

func loadTS(path, scopeDir, root string, depth int, seen map[string]bool) (project.TSConfig, []string, []string) {
	path = filepath.Clean(path)
	if depth > 8 || seen[path] {
		return project.TSConfig{File: path, Dir: filepath.Dir(path)}, nil, nil
	}
	seen[path] = true
	buf, err := os.ReadFile(path)
	if err != nil {
		return project.TSConfig{File: path, Dir: filepath.Dir(path)}, nil, []string{"could not read " + filepath.Base(path)}
	}
	var doc struct {
		Extends         json.RawMessage `json:"extends"`
		CompilerOptions json.RawMessage `json:"compilerOptions"`
		Include         []string        `json:"include"`
		Exclude         []string        `json:"exclude"`
		Files           []string        `json:"files"`
		References      []struct {
			Path string `json:"path"`
		} `json:"references"`
	}
	if err := json.Unmarshal([]byte(jsonc.Strip(string(buf))), &doc); err != nil {
		return project.TSConfig{File: path, Dir: filepath.Dir(path)}, nil, []string{filepath.Base(path) + " is not valid JSONC: " + err.Error()}
	}
	cfg := project.TSConfig{File: path, Dir: filepath.Dir(path), Include: doc.Include, Exclude: doc.Exclude}
	var notes []string
	var inheritedPaths bool
	var inheritedMode string
	var inheritedSuffix []string
	var inheritedBase string
	var inheritedAliases []project.Alias
	for _, ext := range parseExtends(doc.Extends) {
		extPath := resolveExtends(path, ext, root)
		if extPath == "" {
			notes = append(notes, "could not resolve tsconfig extends "+ext)
			continue
		}
		parent, _, n := loadTS(extPath, scopeDir, root, depth+1, seen)
		notes = append(notes, n...)
		inheritedAliases = parent.Aliases
		inheritedBase = parent.BaseURL
		inheritedMode = parent.Mode
		inheritedSuffix = parent.Suffixes
		inheritedPaths = len(parent.Aliases) > 0 || parent.BaseURL != ""
		_ = inheritedPaths
	}
	cfg.Aliases = inheritedAliases
	cfg.BaseURL = inheritedBase
	cfg.Mode = inheritedMode
	cfg.Suffixes = inheritedSuffix

	opts := map[string]json.RawMessage{}
	if len(bytes.TrimSpace(doc.CompilerOptions)) > 0 {
		_ = json.Unmarshal(doc.CompilerOptions, &opts)
	}
	if raw, ok := opts["baseUrl"]; ok {
		var base string
		if json.Unmarshal(raw, &base) == nil {
			if filepath.IsAbs(base) {
				cfg.BaseURL = filepath.Clean(base)
			} else {
				cfg.BaseURL = filepath.Join(cfg.Dir, base)
			}
		}
	}
	if raw, ok := opts["paths"]; ok {
		var paths map[string][]string
		if json.Unmarshal(raw, &paths) == nil {
			base := cfg.BaseURL
			if base == "" {
				base = cfg.Dir
			}
			cfg.Aliases = pathsToAliases(paths, base, path)
		}
	}
	if raw, ok := opts["moduleResolution"]; ok {
		var mr string
		if json.Unmarshal(raw, &mr) == nil {
			cfg.Mode = resolutionMode(mr, "")
		}
	} else if raw, ok := opts["module"]; ok && cfg.Mode == "" {
		var m string
		if json.Unmarshal(raw, &m) == nil {
			cfg.Mode = resolutionMode("", m)
		}
	}
	if raw, ok := opts["moduleSuffixes"]; ok {
		var suf []string
		if json.Unmarshal(raw, &suf) == nil {
			cfg.Suffixes = suf
		}
	}
	var refs []string
	for _, ref := range doc.References {
		if ref.Path == "" {
			continue
		}
		refs = append(refs, filepath.Join(cfg.Dir, ref.Path))
	}
	if len(doc.References) > 0 && len(doc.Include) == 0 && len(doc.Files) == 0 {
		cfg.Solution = true
	}
	_ = scopeDir
	return cfg, refs, notes
}

func pathsToAliases(paths map[string][]string, base, origin string) []project.Alias {
	var keys []string
	for k := range paths {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return len(keys[i]) > len(keys[j])
	})
	var out []project.Alias
	for _, pattern := range keys {
		a := project.Alias{Source: "tsconfig", Origin: origin}
		targets := paths[pattern]
		if pre, suf, ok := splitStar(pattern); ok {
			a.Wildcard = true
			a.Prefix = pre
			a.Suffix = suf
		} else {
			a.Exact = true
			a.Prefix = pattern
		}
		for _, target := range targets {
			a.Targets = append(a.Targets, absTarget(target, base))
		}
		out = append(out, a)
	}
	return out
}

func absTarget(target, base string) string {
	target = filepath.ToSlash(target)
	star := strings.Index(target, "*")
	if star < 0 {
		if filepath.IsAbs(filepath.FromSlash(target)) {
			return filepath.Clean(filepath.FromSlash(target))
		}
		return filepath.Join(base, filepath.FromSlash(target))
	}
	pre := strings.TrimSuffix(target[:star], "/")
	suf := target[star+1:]
	var abs string
	if pre == "" {
		abs = base
	} else if filepath.IsAbs(filepath.FromSlash(pre)) {
		abs = filepath.Clean(filepath.FromSlash(pre))
	} else {
		abs = filepath.Join(base, filepath.FromSlash(pre))
	}
	return filepath.ToSlash(abs) + "/*" + suf
}

func splitStar(pattern string) (string, string, bool) {
	i := strings.Index(pattern, "*")
	if i < 0 {
		return "", "", false
	}
	return pattern[:i], pattern[i+1:], true
}

func resolutionMode(moduleResolution, moduleKind string) string {
	switch strings.ToLower(moduleResolution) {
	case "bundler":
		return "bundler"
	case "node16", "nodenext":
		return "node16"
	case "node", "node10", "classic":
		return "classic"
	}
	switch strings.ToLower(moduleKind) {
	case "node16", "nodenext":
		return "node16"
	}
	return ""
}

func parseExtends(raw json.RawMessage) []string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	if raw[0] == '"' {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			return []string{s}
		}
		return nil
	}
	var list []string
	if json.Unmarshal(raw, &list) == nil {
		return list
	}
	return nil
}

func resolveExtends(fromFile, spec, root string) string {
	if strings.HasPrefix(spec, ".") {
		return existingTS(filepath.Join(filepath.Dir(fromFile), spec))
	}
	dir := filepath.Dir(fromFile)
	for {
		cand := filepath.Join(dir, "node_modules", filepath.FromSlash(spec))
		if p := existingTS(cand); p != "" {
			return p
		}
		pkg, sub, ok := splitPkgLoose(spec)
		if ok {
			base := filepath.Join(dir, "node_modules", filepath.FromSlash(pkg))
			if sub != "" {
				if p := existingTS(filepath.Join(base, filepath.FromSlash(sub))); p != "" {
					return p
				}
			}
			if p := existingTS(base); p != "" {
				return p
			}
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

func splitPkgLoose(spec string) (string, string, bool) {
	if strings.HasPrefix(spec, "@") {
		parts := strings.SplitN(spec, "/", 3)
		if len(parts) < 2 {
			return "", "", false
		}
		name := parts[0] + "/" + parts[1]
		sub := ""
		if len(parts) == 3 {
			sub = parts[2]
		}
		return name, sub, true
	}
	parts := strings.SplitN(spec, "/", 2)
	if len(parts) == 1 {
		return parts[0], "", true
	}
	return parts[0], parts[1], true
}

func existingTS(p string) string {
	if st, err := os.Stat(p); err == nil {
		if st.IsDir() {
			p = filepath.Join(p, "tsconfig.json")
		} else {
			return p
		}
	}
	if !strings.HasSuffix(p, ".json") {
		if st, err := os.Stat(p + ".json"); err == nil && !st.IsDir() {
			return p + ".json"
		}
	}
	if st, err := os.Stat(p); err == nil && !st.IsDir() {
		return p
	}
	return ""
}

func inherit(scopes []project.Scope) {
	sort.Slice(scopes, func(i, j int) bool {
		return len(scopes[i].Dir) < len(scopes[j].Dir)
	})
	for i := range scopes {
		var parent *project.Scope
		for j := range scopes[:i] {
			if project.DirPrefix(scopes[j].Dir, scopes[i].Dir) {
				if parent == nil || len(scopes[j].Dir) > len(parent.Dir) {
					parent = &scopes[j]
				}
			}
		}
		if parent == nil {
			continue
		}
		if len(scopes[i].Aliases) == 0 {
			scopes[i].Aliases = parent.Aliases
		} else {
			scopes[i].Aliases = append(append([]project.Alias{}, scopes[i].Aliases...), parent.Aliases...)
		}
		if len(scopes[i].Exts) == 0 {
			scopes[i].Exts = parent.Exts
		}
		if len(scopes[i].ExtraMod) == 0 {
			scopes[i].ExtraMod = parent.ExtraMod
		}
		if scopes[i].Type == "" {
			scopes[i].Type = parent.Type
		}
	}
}

type pkgFile struct {
	Name                 string            `json:"name"`
	Type                 string            `json:"type"`
	Main                 string            `json:"main"`
	Module               string            `json:"module"`
	Imports              json.RawMessage   `json:"imports"`
	Exports              json.RawMessage   `json:"exports"`
	Dependencies         map[string]string `json:"dependencies"`
	DevDependencies      map[string]string `json:"devDependencies"`
	PeerDependencies     map[string]string `json:"peerDependencies"`
	OptionalDependencies map[string]string `json:"optionalDependencies"`
	Workspaces           json.RawMessage   `json:"workspaces"`
}

func readPkgFile(path string) pkgFile {
	buf, err := os.ReadFile(path)
	if err != nil {
		return pkgFile{}
	}
	var pkg pkgFile
	if json.Unmarshal(buf, &pkg) != nil {
		return pkgFile{}
	}
	return pkg
}

func addDeps(dst map[string]bool, src map[string]string) {
	for k := range src {
		dst[k] = true
	}
}

func workspaceDirs(root string, pkg pkgFile) []string {
	patterns := workspacePatterns(pkg.Workspaces)
	patterns = append(patterns, pnpmPatterns(filepath.Join(root, "pnpm-workspace.yaml"))...)
	var out []string
	seen := map[string]bool{}
	for _, pat := range patterns {
		for _, dir := range matchWorkspaces(root, pat) {
			if !seen[dir] {
				seen[dir] = true
				out = append(out, dir)
			}
		}
	}
	return out
}

func workspacePatterns(raw json.RawMessage) []string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil
	}
	if raw[0] == '[' {
		var list []string
		if json.Unmarshal(raw, &list) == nil {
			return list
		}
	}
	var obj struct {
		Packages []string `json:"packages"`
	}
	if json.Unmarshal(raw, &obj) == nil {
		return obj.Packages
	}
	return nil
}

func pnpmPatterns(path string) []string {
	buf, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	lines := strings.Split(string(buf), "\n")
	in := false
	var out []string
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}
		if strings.HasPrefix(trim, "packages:") {
			in = true
			continue
		}
		if !in {
			continue
		}
		if !strings.HasPrefix(trim, "-") {
			if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
				break
			}
			continue
		}
		item := strings.TrimSpace(strings.TrimPrefix(trim, "-"))
		item = strings.Trim(item, `"'`)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

func matchWorkspaces(root, pattern string) []string {
	pattern = strings.TrimPrefix(filepath.ToSlash(pattern), "./")
	var dirs []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		name := d.Name()
		if name == "node_modules" || name == ".git" || name == "dist" {
			return filepath.SkipDir
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if strings.Count(rel, "/") > 3 {
			return filepath.SkipDir
		}
		if glob.Match(pattern, rel) {
			if st, err := os.Stat(filepath.Join(path, "package.json")); err == nil && !st.IsDir() {
				dirs = append(dirs, path)
			}
		}
		return nil
	})
	return dirs
}

func findFile(dir string, names ...string) string {
	for _, name := range names {
		p := filepath.Join(dir, name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

func mustRead(path string) string {
	buf, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(buf)
}

func rel(root, path string) string {
	r, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(r)
}

func mergeExts(base, extra []string) []string {
	if len(extra) == 0 {
		return base
	}
	seen := map[string]bool{}
	var out []string
	for _, e := range extra {
		if !strings.HasPrefix(e, ".") {
			e = "." + e
		}
		if seen[e] {
			continue
		}
		seen[e] = true
		out = append(out, e)
	}
	for _, e := range []string{".ts", ".tsx", ".vue", ".js", ".jsx", ".json"} {
		if !seen[e] {
			out = append(out, e)
		}
	}
	return out
}
