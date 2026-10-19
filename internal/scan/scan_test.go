package scan

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFilesGlobsAndIgnores(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{
		"src/App.vue",
		"src/main.ts",
		"src/nested/home.ts",
		"src/main.test.ts",
		"node_modules/pkg/index.js",
		"README.md",
	} {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	files, missing, err := Files(root, nil, Options{})
	if err != nil || len(missing) != 0 {
		t.Fatal(err, missing)
	}
	if len(files) != 4 {
		t.Fatalf("default scan = %v", files)
	}
	files, missing, err = Files(root, []string{"src/**/*.vue"}, Options{})
	if err != nil || len(missing) != 0 || len(files) != 1 {
		t.Fatalf("files=%v missing=%v err=%v", files, missing, err)
	}
	files, _, err = Files(root, []string{"*.test.ts"}, Options{Ignore: []string{"**/*.test.ts"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("ignore failed: %v", files)
	}
	_, missing, err = Files(root, []string{"nope/**/*.ts"}, Options{})
	if err != nil || len(missing) != 1 {
		t.Fatalf("missing=%v err=%v", missing, err)
	}
	files, _, err = Files(root, []string{filepath.Join(root, "README.md")}, Options{})
	if err != nil || len(files) != 1 {
		t.Fatalf("explicit file %v %v", files, err)
	}
}
