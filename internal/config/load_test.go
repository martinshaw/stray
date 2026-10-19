package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBrokenTSConfigIsANote(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"name":"app"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "tsconfig.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	proj, err := Load(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(proj.Notes, " "), "not valid JSONC") {
		t.Fatalf("notes = %#v", proj.Notes)
	}
}

func TestNodeEvaluatesComputedAlias(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not installed")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "vite.config.js")
	body := "const path = require('path');\nconst root = path.join(__dirname, 'src');\nmodule.exports = { resolve: { alias: { '@': root } } };\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	aliases, _, err := evalWithNode(path)
	if err != nil {
		t.Fatal(err)
	}
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(aliases) != 1 || aliases[0].Prefix != "@" || aliases[0].Targets[0] != filepath.Join(realDir, "src") {
		t.Fatalf("got %#v want %s", aliases, filepath.Join(realDir, "src"))
	}
}

func TestWorkspacesDiscoverNestedPackages(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"package.json":              `{"name":"root","private":true,"workspaces":["packages/*"]}`,
		"packages/web/package.json": `{"name":"@app/web","dependencies":{"vue":"^3.0.0"}}`,
		"pnpm-workspace.yaml":       "packages:\n  - \"packages/*\"\n",
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
	proj, err := Load(root, Options{Probe: false})
	if err != nil {
		t.Fatal(err)
	}
	if proj.WorkspaceNames["@app/web"] == "" || proj.WorkspaceNames["root"] == "" {
		t.Fatalf("names %#v", proj.WorkspaceNames)
	}
}
