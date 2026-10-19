package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/martinshaw/stray/internal/model"
)

func sample() model.Report {
	return model.Report{
		Version:         "0.1.0",
		Root:            "/proj",
		Scanned:         4,
		FilesWithIssues: 1,
		Errors:          1,
		Warnings:        1,
		IgnoredVirtual:  2,
		Detected: model.Detected{
			Resolution: "bundler",
			Bundler:    "vite",
			Configs:    []string{"package.json", "tsconfig.json", "vite.config.js"},
			Tools:      []model.Tool{{Name: "node", Version: "v22.0.0"}},
			Notes:      []string{"tsconfig.app.json is not valid JSONC: nope"},
		},
		Problems: []model.Problem{
			{File: "src/App.vue", Line: 4, Column: 8, Specifier: "@/missing", Kind: "static", Code: "unresolved", Severity: model.Error, Detail: "no file at src/missing", Attempts: []string{"src/missing.ts"}},
			{File: "src/App.vue", Line: 9, Column: 8, Specifier: "left-pad", Kind: "static", Code: "undeclared-package", Severity: model.Warning, Detail: "not listed in package.json"},
		},
	}
}

func TestFormats(t *testing.T) {
	rep := sample()
	var buf bytes.Buffer
	if err := Write(&buf, rep, Options{Format: "tree"}); err != nil {
		t.Fatal(err)
	}
	tree := buf.String()
	for _, part := range []string{"stray 0.1.0", "src", "App.vue", "unresolved:4", "@/missing", "undeclared-package:9", "resolution bundler", "ignored 2"} {
		if !strings.Contains(tree, part) {
			t.Fatalf("tree missing %q\n%s", part, tree)
		}
	}
	if strings.Contains(tree, "src/missing.ts") {
		t.Fatal("attempts should be hidden without explain")
	}

	buf.Reset()
	if err := Write(&buf, rep, Options{Format: "tree", Explain: true}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "tried src/missing.ts") {
		t.Fatal(buf.String())
	}

	buf.Reset()
	if err := Write(&buf, rep, Options{Format: "markdown"}); err != nil {
		t.Fatal(err)
	}
	md := buf.String()
	if !strings.Contains(md, "# stray report") || !strings.Contains(md, "## `src/App.vue`") || !strings.Contains(md, "unresolved") {
		t.Fatal(md)
	}

	buf.Reset()
	if err := Write(&buf, rep, Options{Format: "compact"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "error unresolved src/App.vue:4:8 @/missing") {
		t.Fatal(buf.String())
	}

	buf.Reset()
	if err := Write(&buf, rep, Options{Format: "github"}); err != nil {
		t.Fatal(err)
	}
	gh := buf.String()
	if !strings.Contains(gh, "::error file=src/App.vue,line=4,col=8,title=unresolved::") || !strings.Contains(gh, "::warning file=src/App.vue") {
		t.Fatal(gh)
	}

	buf.Reset()
	if err := Write(&buf, rep, Options{Format: "json", Explain: true}); err != nil {
		t.Fatal(err)
	}
	var decoded model.Report
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Errors != 1 || decoded.Problems[0].Attempts[0] != "src/missing.ts" {
		t.Fatalf("%#v", decoded)
	}

	buf.Reset()
	in := strings.NewReader("1\na\nq\n")
	if err := Write(&buf, rep, Options{Format: "interactive", In: in}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "1  src/App.vue") || !strings.Contains(buf.String(), "@/missing") {
		t.Fatal(buf.String())
	}
}

func TestQuietTree(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, sample(), Options{Format: "tree", Quiet: true}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "@/missing") || !strings.Contains(buf.String(), "1 errors") {
		t.Fatal(buf.String())
	}
}
