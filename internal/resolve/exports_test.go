package resolve

import (
	"encoding/json"
	"testing"
)

func TestExports(t *testing.T) {
	cases := []struct {
		name, sub, raw, want, status string
		conds                        []string
	}{
		{"string root", ".", `"./index.js"`, "./index.js", "ok", []string{"import"}},
		{"string denies deep", "./secret", `"./index.js"`, "", "denied", []string{"import"}},
		{"conditions import", ".", `{"types":"./index.d.ts","import":"./index.js","default":"./index.cjs"}`, "./index.js", "ok", []string{"import", "default"}},
		{"conditions require", ".", `{"import":"./index.js","require":"./index.cjs"}`, "./index.cjs", "ok", []string{"require", "default"}},
		{"exact subpath", "./util", `{"./util":"./util.js",".":"./index.js"}`, "./util.js", "ok", []string{"import"}},
		{"star", "./util/a", `{"./*":"./*"}`, "./util/a", "ok", []string{"import"}},
		{"missing subpath", "./secret", `{"./util":"./util.js"}`, "", "denied", []string{"import"}},
		{"null target", "./hidden", `{"./hidden":null}`, "", "denied", []string{"default"}},
		{"imports hash", "#util", `{"#util":"./src/util.js"}`, "./src/util.js", "ok", []string{"import"}},
		{"imports pattern", "#util/a", `{"#util/*":"./src/util/*"}`, "./src/util/a", "ok", []string{"import"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, status := resolveExports(json.RawMessage(tc.raw), tc.sub, tc.conds)
			if status != tc.status || got != tc.want {
				t.Fatalf("got %q %s, want %q %s", got, status, tc.want, tc.status)
			}
		})
	}
}
