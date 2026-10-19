// Package report prints a scan in tree, markdown, JSON, compact, GitHub, or interactive form.
package report

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/martinshaw/stray/internal/model"
)

// Options selects a format and how much detail to print.
type Options struct {
	Format  string
	Color   bool
	Explain bool
	Quiet   bool
	In      io.Reader
}

// Write renders rep to w.
func Write(w io.Writer, rep model.Report, opt Options) error {
	switch opt.Format {
	case "json":
		return writeJSON(w, rep, opt)
	case "markdown", "md":
		return writeMarkdown(w, rep, opt)
	case "compact":
		return writeCompact(w, rep, opt)
	case "github":
		return writeGitHub(w, rep)
	case "interactive":
		return writeInteractive(w, rep, opt)
	default:
		return writeTree(w, rep, opt)
	}
}

func writeJSON(w io.Writer, rep model.Report, opt Options) error {
	if !opt.Explain {
		for i := range rep.Problems {
			rep.Problems[i].Attempts = nil
		}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(rep)
}

func writeCompact(w io.Writer, rep model.Report, opt Options) error {
	if !opt.Quiet {
		for _, p := range rep.Problems {
			fmt.Fprintf(w, "%s %s %s:%d:%d %s %s\n", p.Severity, p.Code, p.File, p.Line, p.Column, p.Specifier, p.Detail)
		}
	}
	fmt.Fprintf(w, "%d errors, %d warnings, %d files, %d scanned\n", rep.Errors, rep.Warnings, rep.FilesWithIssues, rep.Scanned)
	return nil
}

func writeGitHub(w io.Writer, rep model.Report) error {
	for _, p := range rep.Problems {
		kind := "error"
		if p.Severity == model.Warning {
			kind = "warning"
		}
		fmt.Fprintf(w, "::%s file=%s,line=%d,col=%d,title=%s::%s — %s\n", kind, p.File, p.Line, max(p.Column, 1), p.Code, p.Specifier, p.Detail)
	}
	fmt.Fprintf(w, "%d errors, %d warnings, %d files, %d scanned\n", rep.Errors, rep.Warnings, rep.FilesWithIssues, rep.Scanned)
	return nil
}

func writeMarkdown(w io.Writer, rep model.Report, opt Options) error {
	fmt.Fprintf(w, "# stray report\n\n")
	fmt.Fprintf(w, "Scanned **%d** files. **%d** files with problems (**%d** errors, **%d** warnings).\n\n", rep.Scanned, rep.FilesWithIssues, rep.Errors, rep.Warnings)
	writeDetectedMD(w, rep)
	if opt.Quiet || len(rep.Problems) == 0 {
		if len(rep.Problems) == 0 {
			fmt.Fprintf(w, "No problematic imports.\n")
		}
		return nil
	}
	for _, group := range groupFiles(rep.Problems) {
		fmt.Fprintf(w, "## `%s`\n\n", group.path)
		fmt.Fprintf(w, "| Line | Severity | Code | Specifier | Detail |\n| --- | --- | --- | --- | --- |\n")
		for _, p := range group.problems {
			fmt.Fprintf(w, "| %d | %s | `%s` | `%s` | %s |\n", p.Line, p.Severity, p.Code, mdCell(p.Specifier), mdCell(p.Detail))
			if opt.Explain {
				for _, attempt := range p.Attempts {
					fmt.Fprintf(w, "| | | | | tried `%s` |\n", mdCell(attempt))
				}
			}
		}
		fmt.Fprintln(w)
	}
	return nil
}

func writeDetectedMD(w io.Writer, rep model.Report) {
	fmt.Fprintf(w, "## Detected\n\n")
	fmt.Fprintf(w, "- resolution: `%s`\n", rep.Detected.Resolution)
	if rep.Detected.Bundler != "" {
		fmt.Fprintf(w, "- bundler: `%s`\n", rep.Detected.Bundler)
	}
	for _, cfg := range rep.Detected.Configs {
		fmt.Fprintf(w, "- config: `%s`\n", cfg)
	}
	if len(rep.Detected.Tools) > 0 {
		var parts []string
		for _, tool := range rep.Detected.Tools {
			if tool.Version != "" {
				parts = append(parts, tool.Name+" "+tool.Version)
			} else {
				parts = append(parts, tool.Name)
			}
		}
		fmt.Fprintf(w, "- tools: %s\n", strings.Join(parts, ", "))
	}
	for _, note := range rep.Detected.Notes {
		fmt.Fprintf(w, "- note: %s\n", note)
	}
	if rep.IgnoredVirtual > 0 {
		fmt.Fprintf(w, "- ignored %d framework virtual imports\n", rep.IgnoredVirtual)
	}
	fmt.Fprintln(w)
}

func writeTree(w io.Writer, rep model.Report, opt Options) error {
	header(w, rep, opt)
	if opt.Quiet {
		return nil
	}
	if len(rep.Problems) == 0 {
		fmt.Fprintln(w, "No problematic imports.")
		return nil
	}
	root := treeNode{name: ""}
	for _, group := range groupFiles(rep.Problems) {
		root.insert(group.path, group.problems)
	}
	root.print(w, "", true, true, opt)
	return nil
}

func header(w io.Writer, rep model.Report, opt Options) {
	fmt.Fprintf(w, "%sstray %s%s\n", paint(bold, opt.Color), rep.Version, paint(reset, opt.Color))
	fmt.Fprintf(w, "%d scanned · %d errors · %d warnings · %d files\n", rep.Scanned, rep.Errors, rep.Warnings, rep.FilesWithIssues)
	fmt.Fprintf(w, "root %s\n", rep.Root)
	fmt.Fprintf(w, "resolution %s\n", rep.Detected.Resolution)
	if rep.Detected.Bundler != "" {
		fmt.Fprintf(w, "bundler %s\n", rep.Detected.Bundler)
	}
	for _, cfg := range rep.Detected.Configs {
		fmt.Fprintf(w, "config %s\n", cfg)
	}
	if len(rep.Detected.Tools) > 0 {
		var parts []string
		for _, tool := range rep.Detected.Tools {
			if tool.Version != "" {
				parts = append(parts, tool.Name+" "+tool.Version)
			} else {
				parts = append(parts, tool.Name)
			}
		}
		fmt.Fprintf(w, "tools %s\n", strings.Join(parts, ", "))
	}
	for _, note := range rep.Detected.Notes {
		fmt.Fprintf(w, "note %s\n", note)
	}
	if rep.IgnoredVirtual > 0 {
		fmt.Fprintf(w, "ignored %d framework virtual imports\n", rep.IgnoredVirtual)
	}
	fmt.Fprintln(w)
}

func writeInteractive(w io.Writer, rep model.Report, opt Options) error {
	groups := groupFiles(rep.Problems)
	fmt.Fprintf(w, "stray %s — %d files, %d errors, %d warnings. Enter a number, a, or q.\n", rep.Version, len(groups), rep.Errors, rep.Warnings)
	for i, group := range groups {
		fmt.Fprintf(w, "  %d  %s  (%d)\n", i+1, group.path, len(group.problems))
	}
	if opt.In == nil {
		return nil
	}
	sc := bufio.NewScanner(opt.In)
	for sc.Scan() {
		text := strings.TrimSpace(sc.Text())
		switch text {
		case "", "q", "quit", "exit":
			if text == "" {
				continue
			}
			return nil
		case "a", "all":
			if err := writeTree(w, rep, opt); err != nil {
				return err
			}
		default:
			n, err := strconv.Atoi(text)
			if err != nil || n < 1 || n > len(groups) {
				fmt.Fprintf(w, "enter 1-%d, a, or q\n", len(groups))
				continue
			}
			one := rep
			one.Problems = groups[n-1].problems
			if err := writeTree(w, one, opt); err != nil {
				return err
			}
		}
	}
	return sc.Err()
}

type fileGroup struct {
	path     string
	problems []model.Problem
}

func groupFiles(problems []model.Problem) []fileGroup {
	order := []string{}
	by := map[string][]model.Problem{}
	for _, p := range problems {
		if _, ok := by[p.File]; !ok {
			order = append(order, p.File)
		}
		by[p.File] = append(by[p.File], p)
	}
	var groups []fileGroup
	for _, path := range order {
		groups = append(groups, fileGroup{path: path, problems: by[path]})
	}
	return groups
}

type treeNode struct {
	name     string
	kids     []*treeNode
	index    map[string]*treeNode
	problems []model.Problem
	file     bool
}

func (n *treeNode) insert(path string, problems []model.Problem) {
	parts := strings.Split(path, "/")
	cur := n
	for i, part := range parts {
		if cur.index == nil {
			cur.index = map[string]*treeNode{}
		}
		child, ok := cur.index[part]
		if !ok {
			child = &treeNode{name: part}
			cur.index[part] = child
			cur.kids = append(cur.kids, child)
		}
		if i == len(parts)-1 {
			child.file = true
			child.problems = problems
		}
		cur = child
	}
}

func (n *treeNode) print(w io.Writer, prefix string, last bool, isRoot bool, opt Options) {
	if !isRoot {
		branch := "├── "
		if last {
			branch = "└── "
		}
		name := n.name
		if n.file {
			name = paint(bold, opt.Color) + name + paint(reset, opt.Color)
		}
		fmt.Fprintf(w, "%s%s%s\n", prefix, branch, name)
	}
	childPrefix := prefix
	if !isRoot {
		if last {
			childPrefix += "    "
		} else {
			childPrefix += "│   "
		}
	}
	if n.file {
		for i, p := range n.problems {
			lastP := i == len(n.problems)-1
			branch := "├── "
			if lastP {
				branch = "└── "
			}
			fmt.Fprintf(w, "%s%s%s\n", childPrefix, branch, formatProblem(p, opt))
			cont := "│   "
			if lastP {
				cont = "    "
			}
			if p.Detail != "" {
				fmt.Fprintf(w, "%s%s%s\n", childPrefix, cont, paint(dim, opt.Color)+p.Detail+paint(reset, opt.Color))
			}
			if opt.Explain {
				for _, attempt := range p.Attempts {
					fmt.Fprintf(w, "%s%s%s\n", childPrefix, cont, paint(dim, opt.Color)+"tried "+attempt+paint(reset, opt.Color))
				}
			}
		}
		return
	}
	sort.Slice(n.kids, func(i, j int) bool {
		if n.kids[i].file != n.kids[j].file {
			return !n.kids[i].file && n.kids[j].file
		}
		return n.kids[i].name < n.kids[j].name
	})
	for i, child := range n.kids {
		child.print(w, childPrefix, i == len(n.kids)-1, false, opt)
	}
}

func formatProblem(p model.Problem, opt Options) string {
	sev := string(p.Severity)
	if opt.Color {
		if p.Severity == model.Error {
			sev = paint(red, true) + sev + paint(reset, true)
		} else {
			sev = paint(yellow, true) + sev + paint(reset, true)
		}
	}
	return fmt.Sprintf("%s %s:%d  %s", sev, p.Code, p.Line, p.Specifier)
}

func mdCell(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.ReplaceAll(s, "`", "'")
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}

const (
	red    = "\x1b[31m"
	yellow = "\x1b[33m"
	dim    = "\x1b[2m"
	bold   = "\x1b[1m"
	reset  = "\x1b[0m"
)

func paint(code string, on bool) string {
	if !on {
		return ""
	}
	return code
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
