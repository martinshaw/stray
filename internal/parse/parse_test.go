package parse

import "testing"

func specifiers(t *testing.T, src string) []Import {
	t.Helper()
	return FileImports("file.ts", src)
}

func has(imports []Import, spec string) bool {
	for _, imp := range imports {
		if imp.Specifier == spec {
			return true
		}
	}
	return false
}

func TestStaticAndFalsePositives(t *testing.T) {
	src := `
		const label = "import './not-real'";
		// import "./also-not-real";
		/* import "./nope" */
		const re = /\//g;
		import { widget } from "./Widget";
		import "./side";
		export { a } from "./reexport";
		export type { T } from "./types";
		export const local = 1;
	`
	got := specifiers(t, src)
	for _, want := range []string{"./Widget", "./side", "./reexport", "./types"} {
		if !has(got, want) {
			t.Fatalf("missing %s in %#v", want, got)
		}
	}
	for _, bad := range []string{"./not-real", "./also-not-real", "./nope"} {
		if has(got, bad) {
			t.Fatalf("unexpected %s in %#v", bad, got)
		}
	}
}

func TestDynamicRequireGlobAndURL(t *testing.T) {
	src := `
		import("./literal");
		const name = "home";
		import(` + "`./pages/${name}.ts`" + `);
		require("./cjs");
		require.resolve("./resolved");
		import.meta.glob("./pages/*.ts");
		import.meta.glob(["./a/*.ts", "./b/*.ts"]);
		const url = new URL("./asset.png", import.meta.url);
		return /\//g;
		import "./after-regex";
	`
	got := specifiers(t, src)
	want := map[string]string{
		"./literal":          KindStatic,
		"./pages/${name}.ts": KindDynamic,
		"./cjs":              KindRequire,
		"./resolved":         KindRequire,
		"./pages/*.ts":       KindGlob,
		"./a/*.ts":           KindGlob,
		"./b/*.ts":           KindGlob,
		"./asset.png":        KindURL,
		"./after-regex":      KindStatic,
	}
	if len(got) != len(want) {
		t.Fatalf("got %#v", got)
	}
	for _, imp := range got {
		kind, ok := want[imp.Specifier]
		if !ok {
			t.Fatalf("unexpected %#v", imp)
		}
		if imp.Kind != kind {
			t.Fatalf("%s kind %s want %s", imp.Specifier, imp.Kind, kind)
		}
	}
	for _, imp := range got {
		if imp.Specifier == "./pages/${name}.ts" && imp.Literal {
			t.Fatal("template import should be non-literal")
		}
	}
}

func TestVueScriptStyleAndSrc(t *testing.T) {
	src := `<template><div>import "./nope"</div></template>
<script src="./external.ts"></script>
<script setup lang="ts">
import Widget from "@/Widget";
import Missing from "./nope.vue";
</script>
<style scoped>
@import "./missing.css";
@import url("./other.css");
url("./bg.png")
</style>`
	got := FileImports("App.vue", src)
	for _, want := range []string{"./external.ts", "@/Widget", "./nope.vue", "./missing.css", "./other.css", "./bg.png"} {
		if !has(got, want) {
			t.Fatalf("missing %s in %#v", want, got)
		}
	}
	if has(got, "./nope") {
		t.Fatalf("template text was parsed: %#v", got)
	}
	for _, imp := range got {
		if imp.Specifier == "@/Widget" && imp.Line < 3 {
			t.Fatalf("line %d, want inside script", imp.Line)
		}
	}
}

func TestFromInsideBraces(t *testing.T) {
	got := specifiers(t, "import { from } from \"./mod\";\n")
	if len(got) != 1 || got[0].Specifier != "./mod" {
		t.Fatalf("%#v", got)
	}
}
