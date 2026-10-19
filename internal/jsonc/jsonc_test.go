package jsonc

import (
	"encoding/json"
	"testing"
)

func TestStripCommentsAndTrailingCommas(t *testing.T) {
	in := `{
		// line
		"compilerOptions": {
			"paths": {
				"@/*": ["src/*"],
			},
			"note": "keep // inside",
		},
		/* block
		   comma, */
	}`
	var doc struct {
		CompilerOptions struct {
			Paths map[string][]string `json:"paths"`
			Note  string              `json:"note"`
		} `json:"compilerOptions"`
	}
	if err := json.Unmarshal([]byte(Strip(in)), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.CompilerOptions.Note != "keep // inside" {
		t.Fatalf("note = %q", doc.CompilerOptions.Note)
	}
	if got := doc.CompilerOptions.Paths["@/*"]; len(got) != 1 || got[0] != "src/*" {
		t.Fatalf("paths = %#v", doc.CompilerOptions.Paths)
	}
}

func TestObjectKeyOrder(t *testing.T) {
	raw := json.RawMessage(`{"import":"./a.js","require":"./b.cjs","default":"./c.js"}`)
	kvs, ok, err := Object(raw)
	if err != nil || !ok {
		t.Fatalf("object: %v %v", ok, err)
	}
	if len(kvs) != 3 || kvs[0].Key != "import" || kvs[1].Key != "require" || kvs[2].Key != "default" {
		t.Fatalf("keys = %#v", kvs)
	}
	if _, ok, err := Object(json.RawMessage(`"nope"`)); err != nil || ok {
		t.Fatalf("non-object ok=%v err=%v", ok, err)
	}
}
