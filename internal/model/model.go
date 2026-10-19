// Package model holds the scan report written by every output format.
package model

// Severity is how seriously an import should be treated.
type Severity string

const (
	// Error means the import will not resolve on a case-sensitive filesystem.
	Error Severity = "error"
	// Warning means the import resolves, but it is fragile or unchecked.
	Warning Severity = "warning"
)

// Problem is one suspicious import.
type Problem struct {
	File      string   `json:"file"`
	Line      int      `json:"line"`
	Column    int      `json:"column,omitempty"`
	Specifier string   `json:"specifier"`
	Kind      string   `json:"kind"`
	Code      string   `json:"code"`
	Severity  Severity `json:"severity"`
	Detail    string   `json:"detail"`
	Attempts  []string `json:"attempts,omitempty"`
}

// Tool is a project binary stray noticed on PATH.
type Tool struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
	Path    string `json:"path,omitempty"`
}

// Detected describes the toolchain and config stray used.
type Detected struct {
	Resolution string   `json:"resolution"`
	Configs    []string `json:"configs"`
	Tools      []Tool   `json:"tools"`
	Notes      []string `json:"notes,omitempty"`
	Bundler    string   `json:"bundler,omitempty"`
}

// Report is the full result of a scan.
type Report struct {
	Version         string    `json:"version"`
	Root            string    `json:"root"`
	Scanned         int       `json:"scanned"`
	FilesWithIssues int       `json:"filesWithIssues"`
	Errors          int       `json:"errors"`
	Warnings        int       `json:"warnings"`
	IgnoredVirtual  int       `json:"ignoredVirtual"`
	Detected        Detected  `json:"detected"`
	Problems        []Problem `json:"problems"`
}
