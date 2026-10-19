package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/martinshaw/stray/internal/model"
	"github.com/martinshaw/stray/internal/version"
)

func demoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "testdata", "demo")
}

func run(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(args, &stdout, &stderr, strings.NewReader(""))
	return code, stdout.String(), stderr.String()
}

func TestVersionAndUsage(t *testing.T) {
	code, stdout, _ := run(t, "-version")
	if code != 0 || !strings.Contains(stdout, version.Version) {
		t.Fatalf("code %d out %s", code, stdout)
	}
	code, _, stderr := run(t, "-format", "nope")
	if code != 2 || !strings.Contains(stderr, "unknown format") {
		t.Fatalf("code %d err %s", code, stderr)
	}
	code, _, stderr = run(t, "-root", filepath.Join(t.TempDir(), "missing"))
	if code != 2 {
		t.Fatalf("code %d err %s", code, stderr)
	}
}

func TestDemoProject(t *testing.T) {
	root := demoRoot(t)
	code, stdout, stderr := run(t, "-root", root, "-no-eval", "-no-color", "-format", "json")
	if stderr != "" && !strings.Contains(stderr, "stray:") {
		t.Fatal(stderr)
	}
	if code != 1 {
		t.Fatalf("code %d\n%s\n%s", code, stdout, stderr)
	}
	var rep model.Report
	if err := json.Unmarshal([]byte(stdout), &rep); err != nil {
		t.Fatal(err, stdout)
	}
	if rep.Version != "0.1.0" || rep.Detected.Resolution != "bundler" || rep.Detected.Bundler != "vite" {
		t.Fatalf("%+v", rep.Detected)
	}
	found := map[string]bool{}
	for _, p := range rep.Problems {
		found[p.Specifier+"|"+p.Code] = true
		if p.Specifier == "virtual:icons" || p.Specifier == "fs" || p.Specifier == "node:path" || p.Specifier == "boxed" || p.Specifier == "./Widget" || p.Specifier == "@/Widget" || p.Specifier == "./pages/*.ts" {
			t.Fatalf("clean import reported: %#v", p)
		}
	}
	for _, key := range []string{
		"./widget|case-mismatch",
		"@/missing|unresolved",
		"left-pad|undeclared-package",
		"boxed/secret|exports-denied",
		"./no-pages/*.ts|empty-glob",
		"./pages/${name}.ts|dynamic",
		"./nope.vue|unresolved",
		"./missing.css|unresolved",
	} {
		if !found[key] {
			t.Fatalf("missing %s in %#v", key, rep.Problems)
		}
	}
	if rep.IgnoredVirtual < 1 {
		t.Fatalf("ignored %d", rep.IgnoredVirtual)
	}
	if rep.Scanned < 4 {
		t.Fatalf("scanned %d", rep.Scanned)
	}

	code, stdout, stderr = run(t, "-root", root, "-no-eval", "-no-color", "-format", "tree")
	if code != 1 {
		t.Fatal(code, stderr)
	}
	for _, part := range []string{"stray 0.1.0", "├──", "└──", "case-mismatch", "exports-denied", "resolution bundler"} {
		if !strings.Contains(stdout, part) {
			t.Fatalf("tree missing %q\n%s", part, stdout)
		}
	}

	code, stdout, _ = run(t, "-root", root, "-no-eval", "-format", "compact", "-quiet")
	if code != 1 || strings.Contains(stdout, "@/missing") {
		t.Fatalf("quiet %d %s", code, stdout)
	}

	code, _, _ = run(t, "-root", root, "-no-eval", "-format", "json", "-strict")
	if code != 1 {
		t.Fatal(code)
	}

	only := filepath.Join(root, "src", "ok.ts")
	code, stdout, stderr = run(t, "-root", root, "-no-eval", "-format", "compact", only)
	if code != 0 || stderr != "" {
		t.Fatalf("ok.ts code %d\n%s\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "0 errors, 0 warnings, 0 files, 1 scanned") {
		t.Fatal(stdout)
	}

	code, _, stderr = run(t, "-root", root, "-no-eval", "src/does-not-exist/**/*.ts")
	if code != 2 || !strings.Contains(stderr, "no files matched") {
		t.Fatalf("code %d %s", code, stderr)
	}
}

func TestUndeclaredIsWarningUntilStrict(t *testing.T) {
	root := t.TempDir()
	for name, body := range map[string]string{
		"package.json":                       `{"name":"app"}`,
		"src/main.ts":                        "import pad from \"left-pad\";\nvoid pad;\n",
		"node_modules/left-pad/package.json": `{"name":"left-pad","main":"index.js"}`,
		"node_modules/left-pad/index.js":     "module.exports = {}\n",
	} {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	code, _, _ := run(t, "-root", root, "-no-eval", "-format", "json")
	if code != 0 {
		t.Fatalf("warnings should exit 0, got %d", code)
	}
	code, _, _ = run(t, "-root", root, "-no-eval", "-format", "json", "-fail-on", "warning")
	if code != 1 {
		t.Fatalf("fail-on warning = %d", code)
	}
	code, _, _ = run(t, "-root", root, "-no-eval", "-format", "json", "-fail-on", "none")
	if code != 0 {
		t.Fatal(code)
	}
}

func TestInteractiveAndGitHub(t *testing.T) {
	root := demoRoot(t)
	var stdout, stderr bytes.Buffer
	code := Run([]string{"-root", root, "-no-eval", "-no-color", "-format", "interactive"}, &stdout, &stderr, strings.NewReader("1\nq\n"))
	if code != 1 || !strings.Contains(stdout.String(), "Enter a number") {
		t.Fatalf("code %d\n%s\n%s", code, stdout.String(), stderr.String())
	}
	code, stdoutText, _ := run(t, "-root", root, "-no-eval", "-format", "github")
	if code != 1 || !strings.Contains(stdoutText, "::error ") || !strings.Contains(stdoutText, "::warning ") {
		t.Fatalf("%s", stdoutText)
	}
}

func TestReleaseWorkflowMentionsBinaries(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	path := filepath.Join(filepath.Dir(file), "..", "..", ".github", "workflows", "release.yml")
	buf, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(buf)
	for _, part := range []string{"go build", "GOOS", "GOARCH", "contents: write", "gh release create", "GH_REPO", "v*", "go test ./..."} {
		if !strings.Contains(text, part) {
			t.Fatalf("workflow missing %s", part)
		}
	}
}
