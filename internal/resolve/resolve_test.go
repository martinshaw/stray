package resolve

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/martinshaw/stray/internal/config"
	"github.com/martinshaw/stray/internal/model"
)

func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func codes(problems []model.Problem) map[string]model.Problem {
	out := map[string]model.Problem{}
	for _, p := range problems {
		out[p.Specifier+"|"+p.Code] = p
	}
	return out
}

func load(t *testing.T, root string) *Resolver {
	t.Helper()
	proj, err := config.Load(root, config.Options{Resolution: "auto"})
	if err != nil {
		t.Fatal(err)
	}
	return New(proj, true)
}

func TestRelativeAliasesPackagesAndCase(t *testing.T) {
	root := writeTree(t, map[string]string{
		"package.json": `{
			"name": "app",
			"type": "module",
			"imports": { "#util": "./src/util.ts" },
			"dependencies": { "boxed": "1.0.0" }
		}`,
		"tsconfig.json": `{
			"compilerOptions": {
				"baseUrl": ".",
				"paths": { "@/*": ["src/*"] },
				"moduleResolution": "bundler"
			}
		}`,
		"vite.config.js": `module.exports = { resolve: { alias: { "@": path.resolve(__dirname, "src") } } };`,
		"src/util.ts":    "export const util = 1;\n",
		"src/Widget.ts":  "export const widget = 1;\n",
		"src/main.ts": `import "./util";
import "./missing";
import "@/Widget";
import "@/nope";
import "#util";
import "boxed";
import "boxed/secret";
import "left-pad";
import "fs";
import "./widget";
`,
		"node_modules/boxed/package.json":    `{"name":"boxed","exports":{".":"./index.js"}}`,
		"node_modules/boxed/index.js":        "export {}",
		"node_modules/boxed/secret.js":       "export {}",
		"node_modules/left-pad/package.json": `{"name":"left-pad","main":"index.js"}`,
		"node_modules/left-pad/index.js":     "module.exports = {}",
	})
	// vite alias uses path.resolve; static parser needs the path identifier only when the
	// call is path.resolve. The file above references path without requiring it, but the
	// expression still matches path.resolve(__dirname, "src") if written that way.
	// Rewrite the config to the supported call form.
	vite := "const path = require('path');\nmodule.exports = { resolve: { alias: { '@': path.resolve(__dirname, 'src') } } };\n"
	if err := os.WriteFile(filepath.Join(root, "vite.config.js"), []byte(vite), 0o644); err != nil {
		t.Fatal(err)
	}
	r := load(t, root)
	problems, ignored := r.File(filepath.Join(root, "src", "main.ts"), mustRead(t, filepath.Join(root, "src", "main.ts")))
	got := codes(problems)
	if _, ok := got["./missing|unresolved"]; !ok {
		t.Fatalf("missing unresolved: %#v", problems)
	}
	if _, ok := got["@/nope|unresolved"]; !ok {
		t.Fatalf("alias unresolved: %#v", problems)
	}
	if _, ok := got["boxed/secret|exports-denied"]; !ok {
		t.Fatalf("exports: %#v", problems)
	}
	if _, ok := got["left-pad|undeclared-package"]; !ok {
		t.Fatalf("undeclared: %#v", problems)
	}
	if _, ok := got["./widget|case-mismatch"]; !ok {
		t.Fatalf("case: %#v", problems)
	}
	if p := got["./widget|case-mismatch"]; p.Severity != caseSeverity() {
		t.Fatalf("case severity %s", p.Severity)
	}
	for _, spec := range []string{"./util", "@/Widget", "#util", "boxed", "fs"} {
		for _, p := range problems {
			if p.Specifier == spec {
				t.Fatalf("%s should be clean, got %#v", spec, p)
			}
		}
	}
	if ignored != 0 {
		t.Fatalf("ignored %d", ignored)
	}
}

func TestJSSpecifierMapsToTSOnlyInBundlerMode(t *testing.T) {
	root := writeTree(t, map[string]string{
		"package.json":  `{"name":"app"}`,
		"tsconfig.json": `{"compilerOptions":{"moduleResolution":"node"}}`,
		"src/main.ts":   `import "./a.js";`,
		"src/a.ts":      "export const a = 1;\n",
	})
	r := load(t, root)
	problems, _ := r.File(filepath.Join(root, "src", "main.ts"), `import "./a.js";`)
	if len(problems) == 0 || problems[0].Code != "unresolved" {
		t.Fatalf("classic mode should not map .js to .ts: %#v mode=%s", problems, r.P.Mode)
	}

	root = writeTree(t, map[string]string{
		"package.json":  `{"name":"app"}`,
		"tsconfig.json": `{"compilerOptions":{"moduleResolution":"bundler"}}`,
		"src/a.ts":      "export const a = 1;\n",
	})
	r = load(t, root)
	problems, _ = r.File(filepath.Join(root, "src", "main.ts"), `import "./a.js";`)
	if len(problems) != 0 {
		t.Fatalf("bundler mode should map .js to .ts: %#v", problems)
	}
}

func TestAliasDriftVirtualGlobAndEscape(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "app")
	files := map[string]string{
		"package.json": `{"name":"app"}`,
		"tsconfig.json": `{
			"compilerOptions": {
				"baseUrl": ".",
				"paths": { "@/*": ["ts/*"] },
				"moduleResolution": "bundler"
			}
		}`,
		"vite.config.js": "const path = require('path');\nmodule.exports = { resolve: { alias: { '@': path.resolve(__dirname, 'vite') } } };\n",
		"vite/there.ts":  "export const there = 1;\n",
		"src/main.ts": `import "@/there";
import "virtual:pwa";
import.meta.glob("./pages/*.ts");
import.meta.glob("./missing/*.ts");
import "../../outside";
`,
		"src/pages/home.ts": "export const home = 1;\n",
	}
	if err := os.MkdirAll(filepath.Join(root, "src", "pages"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(parent, "outside.ts"), []byte("export {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := load(t, root)
	problems, ignored := r.File(filepath.Join(root, "src", "main.ts"), files["src/main.ts"])
	got := codes(problems)
	if _, ok := got["@/there|alias-drift"]; !ok {
		t.Fatalf("drift: %#v", problems)
	}
	if _, ok := got["./missing/*.ts|empty-glob"]; !ok {
		t.Fatalf("glob: %#v", problems)
	}
	if _, ok := got["../../outside|escapes-root"]; !ok {
		t.Fatalf("escape: %#v", problems)
	}
	if ignored != 1 {
		t.Fatalf("ignored virtual = %d, problems %#v", ignored, problems)
	}
	for _, p := range problems {
		if p.Specifier == "./pages/*.ts" {
			t.Fatalf("matching glob should be quiet: %#v", p)
		}
	}
}

func TestExtendsAndSelfPackage(t *testing.T) {
	root := writeTree(t, map[string]string{
		"package.json": `{
			"name": "@app/web",
			"type": "module",
			"exports": { ".": "./index.ts", "./util": "./src/util.ts" }
		}`,
		"base.json": `{
			"compilerOptions": {
				"baseUrl": ".",
				"paths": { "@/*": ["src/*"] }
			}
		}`,
		"tsconfig.json": `{ "extends": "./base.json", "compilerOptions": { "moduleResolution": "bundler" } }`,
		"index.ts":      "export const app = 1;\n",
		"src/util.ts":   "export const util = 1;\n",
		"src/main.ts":   "import \"@app/web/util\";\nimport \"@app/web/nope\";\nimport \"@/util\";\n",
	})
	r := load(t, root)
	problems, _ := r.File(filepath.Join(root, "src", "main.ts"), mustRead(t, filepath.Join(root, "src", "main.ts")))
	got := codes(problems)
	if _, ok := got["@app/web/nope|exports-denied"]; !ok {
		t.Fatalf("%#v", problems)
	}
	for _, spec := range []string{"@app/web/util", "@/util"} {
		for _, p := range problems {
			if p.Specifier == spec {
				t.Fatalf("%s should resolve via extends/self: %#v", spec, p)
			}
		}
	}
}

func TestWebpackModulesAndQuery(t *testing.T) {
	root := writeTree(t, map[string]string{
		"package.json":      `{"name":"app"}`,
		"webpack.config.js": "const path = require('path');\nmodule.exports = { resolve: { alias: { '@': path.resolve(__dirname, 'src') }, modules: ['src', 'node_modules'], extensions: ['.js', '.ts'] } };\n",
		"src/icon.svg":      "<svg/>",
		"src/main.js":       "import icon from './icon.svg?raw';\nimport thing from 'thing';\n",
		"src/thing.ts":      "export const thing = 1;\n",
	})
	r := load(t, root)
	if r.P.Mode != "bundler" || r.P.BundlerName != "webpack" {
		t.Fatalf("mode %s bundler %s", r.P.Mode, r.P.BundlerName)
	}
	problems, _ := r.File(filepath.Join(root, "src", "main.js"), mustRead(t, filepath.Join(root, "src", "main.js")))
	for _, p := range problems {
		if p.Specifier == "./icon.svg?raw" || p.Specifier == "thing" {
			t.Fatalf("unexpected %#v", p)
		}
	}
}

func TestCaseSeverityFollowsOS(t *testing.T) {
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		if caseSeverity() != model.Warning {
			t.Fatal(caseSeverity())
		}
		return
	}
	if caseSeverity() != model.Error {
		t.Fatal(caseSeverity())
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	buf, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(buf)
}
