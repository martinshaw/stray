package glob

import "testing"

func TestMatch(t *testing.T) {
	cases := []struct {
		pat, path string
		want      bool
	}{
		{"*.vue", "src/App.vue", true},
		{"*.vue", "src/App.ts", false},
		{"src/**/*.vue", "src/App.vue", true},
		{"src/**/*.vue", "src/components/App.vue", true},
		{"src/**/*.vue", "lib/App.vue", false},
		{"src/*.ts", "src/main.ts", true},
		{"src/*.ts", "src/pages/home.ts", false},
		{"**/home.ts", "src/pages/home.ts", true},
		{"src/Widget.ts", "src/Widget.ts", true},
		{"src/?", "src/a", true},
		{"src/[abc].ts", "src/b.ts", true},
		{"src/[!a].ts", "src/b.ts", true},
		{"src/[!a].ts", "src/a.ts", false},
	}
	for _, tc := range cases {
		if got := Match(tc.pat, tc.path); got != tc.want {
			t.Errorf("Match(%q, %q) = %v, want %v", tc.pat, tc.path, got, tc.want)
		}
	}
}
