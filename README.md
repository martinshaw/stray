# stray

`stray` reports JavaScript, TypeScript, Vue, and Svelte imports that do not land on a real file. It is a single static binary with no runtime dependencies. Point it at a repository and it reads the config that is actually there: `package.json`, `tsconfig` / `jsconfig`, Vite, webpack, Nuxt, pnpm workspaces, and Node's `exports` / `imports` maps.

```text
stray 0.1.1
6 scanned · 4 errors · 4 warnings · 2 files
resolution bundler
bundler vite

└── src
    ├── App.vue
    │   ├── error unresolved:6  ./nope.vue
    │   │   no file at src/nope.vue
    │   └── error unresolved:11  ./missing.css
    │       no file at src/missing.css
    └── main.ts
        ├── warning case-mismatch:2  ./widget
        │   import uses src/widget.ts but the file on disk is src/Widget.ts
        ├── error unresolved:3  @/missing
        │   no file at src/missing
        ├── warning undeclared-package:4  left-pad
        │   resolved node_modules/left-pad/index.js; left-pad is not listed in package.json
        ├── error exports-denied:6  boxed/secret
        │   package boxed does not export ./secret
        ├── warning empty-glob:12  ./no-pages/*.ts
        │   glob matched 0 files
        └── warning dynamic:14  ./pages/${name}.ts
            non-literal import is not checked
```

That sample is `testdata/demo`. `fs`, `node:path`, `boxed`, `@/Widget`, and `virtual:icons` stay quiet: the first three resolve, and Vite virtual modules are expected once a Vite config is present.

## Install

Download a binary from the [releases](https://github.com/martinshaw/stray/releases), or build it yourself:

```sh
go install github.com/martinshaw/stray/cmd/stray@v0.1.1
```

`go install` still produces one binary. The module has no third-party dependencies.

Release archives:

| File | System |
| --- | --- |
| `stray-darwin-arm64` | macOS Apple silicon |
| `stray-darwin-amd64` | macOS Intel |
| `stray-linux-amd64` | Linux x86_64 |
| `stray-linux-arm64` | Linux arm64 |
| `stray-windows-amd64.exe` | Windows x86_64 |

## Usage

Run it from the repository root. Paths after the flags are files, directories, or globs relative to `-root`. Quote globs so the shell does not expand them first.

```sh
stray
stray -format markdown 'src/**/*.vue' 'src/**/*.ts'
stray -format github -strict
stray -format interactive
stray -ignore '**/*.test.ts' -ext .vue,.ts
```

### Flags

| Flag | Default | Meaning |
| --- | --- | --- |
| `-format` | `tree` | `tree`, `markdown`, `json`, `compact`, `github`, or `interactive` |
| `-root` | `.` | Project root. Config is discovered here, and in workspace packages. |
| `-resolution` | `auto` | `auto`, `bundler`, `node16`, or `classic` |
| `-ignore` | | Glob to skip, relative to the root. Repeatable. |
| `-ext` | Vue, Svelte, JS, TS | Comma-separated extensions. |
| `-fail-on` | `error` | `error`, `warning`, or `none` |
| `-strict` | off | Same as `-fail-on warning` |
| `-explain` | off | Print the candidate paths that were tried |
| `-quiet` | off | Summary only |
| `-no-color` | off | Disable ANSI color. `NO_COLOR` is also honored. |
| `-no-case` | off | Skip filename case checks |
| `-no-ignore` | off | Also walk `node_modules`, `dist`, and other build directories |
| `-no-eval` | off | Do not run Node to evaluate a bundler config |
| `-version` | | Print `0.1.1` and exit |

### Formats

- **tree** is the default. Files are grouped as a directory tree and each problem hangs off its file.
- **markdown** is a report you can paste into a pull request.
- **json** is the same report for another program. `-explain` keeps the candidate list.
- **compact** is one line per problem: `error unresolved src/App.vue:6:21 ./nope.vue no file at src/nope.vue`
- **github** prints workflow commands (`::error`, `::warning`) plus the summary.
- **interactive** lists the files, then reads `1` to open one, `a` for the full tree, or `q` to quit. Commands can be piped.

### Exit codes

| Code | When |
| --- | --- |
| 0 | No problems at the chosen severity. Warnings alone do not fail. |
| 1 | At least one error, or a warning when `-strict` / `-fail-on warning` is set |
| 2 | Bad flags, unreadable root, or a glob that matched nothing |

## What it understands

stray does not shell out to `tsc` or Vite to decide whether a file exists. It reads the config itself so the check matches the rules those tools use.

**TypeScript.** `tsconfig.json`, `jsconfig.json`, and `tsconfig.app.json`, including JSONC comments, trailing commas, `extends` (relative or from `node_modules`), and project `references`. `paths` use the longest prefix. A solution-style config (`references`, no `include`) does not claim every file; the referenced project that matches the file does. `moduleSuffixes` are probed. `baseUrl` is tried after aliases.

**Vite, webpack, Nuxt.** Aliases in `resolve.alias`, including object and array forms, `path.resolve(__dirname, ...)`, `path.join`, `fileURLToPath(new URL(..., import.meta.url))`, and `__dirname + "/src"`. Simple prefix regexes such as `/^@\//` are translated. webpack `resolve.extensions` and `resolve.modules` are picked up. A string alias matches on a slash boundary, so `@` rewrites `@/Button` and does not swallow `@vue/runtime-core`. Nuxt adds `~`, `@`, `~~`, and `@@`, and `srcDir` when it is a string. Known virtual ids (`virtual:…`, Nuxt `#imports`, `#components`, `#app`, …) are counted and omitted.

**Node packages.** Nearest `package.json`, npm/yarn `workspaces`, and `pnpm-workspace.yaml`. Bare imports walk `node_modules` from the importing file up to the root. `exports` is honored in bundler and `node16` mode, including conditional exports and one `*` pattern. The package `imports` field resolves `#internal` ids. A package may import its own name. `main` / `module` are the fallback when `exports` is absent. Node builtins, with or without the `node:` prefix, are fine.

**Source.** `import`, `export … from`, `import()`, `require()`, `require.resolve()`, `import.meta.glob`, `new URL("./file", import.meta.url)`, Vue/Svelte `<script src>`, and CSS `@import` / relative `url()` in `<style>`. Webpack loader prefixes (`!!raw-loader!./file`) and asset queries (`./icon.svg?raw`) are stripped before the lookup. Comments, strings, and regular expressions are not imports.

**Resolution mode.** `auto` uses bundler rules when a Vite, webpack, or Nuxt config exists, otherwise the `moduleResolution` from the root tsconfig (`bundler`, `node16` / `nodenext`, or classic `node`). Bundler and `node16` map a `.js` specifier onto a sibling `.ts` file. Classic mode does not, which is how a Node project that never rewrites extensions gets a real error. `-resolution` forces one mode.

When a tsconfig path and a bundler alias both match and point at different files, the bundler wins (that is what the dev server loads) and stray reports `alias-drift`.

### Problem codes

| Code | Severity | Meaning |
| --- | --- | --- |
| `unresolved` | error | No file, or a package that is not installed |
| `exports-denied` | error | The package exists, and `exports` blocks that subpath |
| `case-mismatch` | error on Linux, warning on macOS and Windows | The import's casing is not the name on disk |
| `undeclared-package` | warning | The package resolved from `node_modules` but is not in `dependencies` or `devDependencies` |
| `alias-drift` | warning | tsconfig and the bundler map the same specifier to different files |
| `escapes-root` | warning | A relative or alias import resolved outside the project |
| `empty-glob` | warning | `import.meta.glob` matched nothing |
| `dynamic` | warning | The specifier is not a literal, so it was not checked |
| `virtual` | warning | `virtual:` (or a Nuxt id) with no Vite or Nuxt config in the project |
| `unreadable` | error | The file could not be read |

## Trying the demo

```sh
stray -root testdata/demo -no-eval -no-color
```

`testdata/demo/node_modules` is a fixture, not an installed tree. It is there so the package checks have something to resolve.

## License

Copyright (C) 2026 Martin Shaw.

stray is free software: you can redistribute it and/or modify it under the terms of the GNU General Public License as published by the Free Software Foundation, either version 3 of the License, or (at your option) any later version.

This program is distributed in the hope that it will be useful, but WITHOUT ANY WARRANTY; without even the implied warranty of MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the GNU General Public License for more details.

The full text is in [LICENSE](LICENSE). The source repository is public: <https://github.com/martinshaw/stray>.

## Development

```sh
go test ./...
go build -o stray ./cmd/stray
```

The release workflow builds `linux`, `darwin`, and `windows` binaries when a `v*` tag is pushed, then publishes them with a `SHA256SUMS` file. Tests run before the binaries are built.

## Limits

- A fully dynamic import, such as a template string built from a variable, is reported and not expanded.
- An alias whose replacement is an arbitrary expression is evaluated by Node when `node` is on `PATH`. That runs the config file. Pass `-no-eval` to stay on the static parser. TypeScript configs need Node 22 or newer for `--experimental-strip-types`; otherwise the static parser is used.
- Regex aliases that are more than a prefix are skipped and mentioned in the header.
- stray checks that the target file exists. It does not run the type checker, so a file can exist and still fail `tsc` for a type error.
- Package lookup stops at the project root. A `node_modules` directory above the repository is ignored on purpose.
