// Package cli is the stray command.
package cli

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/martinshaw/stray/internal/config"
	"github.com/martinshaw/stray/internal/model"
	"github.com/martinshaw/stray/internal/report"
	"github.com/martinshaw/stray/internal/resolve"
	"github.com/martinshaw/stray/internal/scan"
	"github.com/martinshaw/stray/internal/version"
)

// Run executes stray and returns a process status code.
func Run(args []string, stdout, stderr io.Writer, stdin io.Reader) int {
	fs := flagSet(stderr)
	var (
		format     = fs.String("format", "tree", "output format: tree, markdown, json, compact, github, interactive")
		root       = fs.String("root", ".", "project root")
		ext        = fs.String("ext", "", "comma-separated extensions to scan")
		failOn     = fs.String("fail-on", "error", "exit 1 when this severity is present: none, warning, or error")
		resolution = fs.String("resolution", "auto", "resolution mode: auto, bundler, node16, or classic")
		ignores    stringList
		strict     = fs.Bool("strict", false, "exit 1 when warnings are present")
		noColor    = fs.Bool("no-color", false, "disable ANSI color")
		noEval     = fs.Bool("no-eval", false, "do not execute node to evaluate bundler configs")
		noCase     = fs.Bool("no-case", false, "do not compare filename casing")
		noIgnore   = fs.Bool("no-ignore", false, "do not skip node_modules, dist, and other build directories")
		quiet      = fs.Bool("quiet", false, "print the summary without problem details")
		explain    = fs.Bool("explain", false, "include candidate paths that were tried")
		showVer    = fs.Bool("version", false, "print version and exit")
	)
	fs.Var(&ignores, "ignore", "glob to ignore, relative to the project root (repeatable)")
	fs.Usage = func() {
		fmt.Fprintf(stderr, "stray finds imports that do not resolve.\n\nUsage:\n  stray [flags] [path...]\n\n")
		fs.PrintDefaults()
		fmt.Fprintf(stderr, "\nPaths are files, directories, or globs relative to -root. Quote globs so the shell does not expand them.\n")
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *showVer {
		fmt.Fprintf(stdout, "stray %s\n", version.Version)
		return 0
	}
	style := strings.ToLower(*format)
	switch style {
	case "tree", "markdown", "md", "json", "compact", "github", "interactive":
	default:
		fmt.Fprintf(stderr, "unknown format %q\n", *format)
		return 2
	}
	switch *resolution {
	case "auto", "bundler", "node16", "classic":
	default:
		fmt.Fprintf(stderr, "unknown resolution %q\n", *resolution)
		return 2
	}
	fail := *failOn
	if *strict {
		fail = "warning"
	}
	switch fail {
	case "none", "warning", "error":
	default:
		fmt.Fprintf(stderr, "unknown -fail-on %q\n", *failOn)
		return 2
	}
	var exts []string
	if strings.TrimSpace(*ext) != "" {
		for _, part := range strings.Split(*ext, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				exts = append(exts, part)
			}
		}
	}
	proj, err := config.Load(*root, config.Options{
		Eval:       !*noEval,
		Probe:      true,
		Resolution: *resolution,
	})
	if err != nil {
		fmt.Fprintf(stderr, "stray: %v\n", err)
		return 2
	}
	files, missing, err := scan.Files(proj.Root, fs.Args(), scan.Options{
		Exts:     exts,
		Ignore:   ignores,
		NoIgnore: *noIgnore,
	})
	if err != nil {
		fmt.Fprintf(stderr, "stray: %v\n", err)
		return 2
	}
	if len(missing) > 0 {
		fmt.Fprintf(stderr, "stray: no files matched %s\n", strings.Join(missing, ", "))
		return 2
	}
	sort.Strings(files)
	resolver := resolve.New(proj, !*noCase)
	var problems []model.Problem
	ignored := 0
	for _, file := range files {
		buf, err := os.ReadFile(file)
		if err != nil {
			rel, _ := relFile(proj.Root, file)
			problems = append(problems, model.Problem{
				File:     rel,
				Line:     1,
				Severity: model.Error,
				Code:     "unreadable",
				Detail:   err.Error(),
			})
			continue
		}
		found, skip := resolver.File(file, string(buf))
		ignored += skip
		problems = append(problems, found...)
	}
	sort.Slice(problems, func(i, j int) bool {
		if problems[i].File != problems[j].File {
			return problems[i].File < problems[j].File
		}
		if problems[i].Line != problems[j].Line {
			return problems[i].Line < problems[j].Line
		}
		if problems[i].Specifier != problems[j].Specifier {
			return problems[i].Specifier < problems[j].Specifier
		}
		return problems[i].Code < problems[j].Code
	})
	rep := model.Report{
		Version:        version.Version,
		Root:           proj.Root,
		Scanned:        len(files),
		IgnoredVirtual: ignored,
		Problems:       problems,
		Detected: model.Detected{
			Resolution: proj.Mode,
			Configs:    proj.Configs,
			Notes:      proj.Notes,
			Bundler:    proj.BundlerName,
		},
	}
	for _, tool := range proj.Tools {
		rep.Detected.Tools = append(rep.Detected.Tools, model.Tool{Name: tool.Name, Version: tool.Version, Path: tool.Path})
	}
	filesWith := map[string]bool{}
	for _, p := range problems {
		filesWith[p.File] = true
		if p.Severity == model.Error {
			rep.Errors++
		} else {
			rep.Warnings++
		}
	}
	rep.FilesWithIssues = len(filesWith)
	color := wantColor(stdout, *noColor)
	if err := report.Write(stdout, rep, report.Options{
		Format:  style,
		Color:   color,
		Explain: *explain,
		Quiet:   *quiet,
		In:      stdin,
	}); err != nil {
		fmt.Fprintf(stderr, "stray: %v\n", err)
		return 2
	}
	switch fail {
	case "none":
		return 0
	case "warning":
		if rep.Errors > 0 || rep.Warnings > 0 {
			return 1
		}
	default:
		if rep.Errors > 0 {
			return 1
		}
	}
	return 0
}
