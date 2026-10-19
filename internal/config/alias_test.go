package config

import (
	"path/filepath"
	"testing"
)

func TestEvalPathExpressions(t *testing.T) {
	dir := filepath.Join(string(filepath.Separator), "proj")
	cases := []struct {
		expr string
		want string
	}{
		{`path.resolve(__dirname, "src")`, filepath.Join(dir, "src")},
		{`path.join(__dirname, "src", "lib")`, filepath.Join(dir, "src", "lib")},
		{`fileURLToPath(new URL("./src", import.meta.url))`, filepath.Join(dir, "src")},
		{`__dirname + "/src"`, filepath.Join(dir, "src")},
		{`path.resolve(process.cwd(), "packages")`, filepath.Join(dir, "packages")},
	}
	for _, tc := range cases {
		got, ok := evalExpr(tc.expr, dir, dir)
		if !ok {
			t.Fatalf("did not evaluate %s", tc.expr)
		}
		if filepath.Clean(got) != filepath.Clean(tc.want) {
			t.Fatalf("%s -> %s, want %s", tc.expr, got, tc.want)
		}
	}
	if _, ok := evalExpr("computed()", dir, dir); ok {
		t.Fatal("computed() should not evaluate")
	}
}

func TestExtractViteAndWebpackAliases(t *testing.T) {
	dir := t.TempDir()
	vite := `
		const path = require("path");
		export default {
			resolve: {
				alias: {
					"@": path.resolve(__dirname, "src"),
					"@utils/api/client": path.resolve(__dirname, "src/api/client"),
				},
			},
		};
	`
	aliases, notes := extractAliases(vite, dir, dir, "vite", filepath.Join(dir, "vite.config.js"))
	if len(notes) != 0 {
		t.Fatal(notes)
	}
	if len(aliases) != 2 {
		t.Fatalf("%#v", aliases)
	}
	if aliases[0].Prefix != "@" || aliases[0].Targets[0] != filepath.Join(dir, "src") {
		t.Fatalf("alias0 %#v", aliases[0])
	}
	if aliases[1].Prefix != "@utils/api/client" {
		t.Fatalf("alias1 %#v", aliases[1])
	}

	wp := `
		module.exports = {
			resolve: {
				alias: [
					{ find: "@", replacement: path.resolve(__dirname, "lib") },
					{ find: /^~\//, replacement: path.resolve(__dirname, "lib") + "/" },
				],
				extensions: [".ts", ".js"],
			},
		};
	`
	found, notes := extractAliases(wp, dir, dir, "webpack", "webpack.config.js")
	if len(notes) != 0 {
		t.Fatal(notes)
	}
	if len(found) != 2 || found[0].Prefix != "@" || found[1].Prefix != "~/" {
		t.Fatalf("%#v notes=%v", found, notes)
	}
	if got := parseStringArray(wp, "extensions"); len(got) != 2 || got[0] != ".ts" {
		t.Fatalf("extensions %#v", got)
	}
}

func TestNuxtSrcDir(t *testing.T) {
	dir := t.TempDir()
	src := "export default defineNuxtConfig({ srcDir: 'app' })"
	aliases := addNuxtAliases(src, dir, dir, "nuxt.config.ts", nil)
	by := map[string]string{}
	for _, a := range aliases {
		by[a.Prefix] = a.Targets[0]
	}
	if by["@"] != filepath.Join(dir, "app") || by["@@"] != dir || by["~"] != filepath.Join(dir, "app") {
		t.Fatalf("%#v", by)
	}
}
