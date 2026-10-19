// Package project describes the configs stray detected for a repository.
package project

import "encoding/json"

// Alias is one import prefix contributed by tsconfig, Vite, webpack, or Nuxt.
type Alias struct {
	Prefix      string
	Suffix      string
	Wildcard    bool
	Exact       bool
	PrefixMatch bool
	Targets     []string
	Source      string
	Origin      string
}

// TSConfig is one parsed TypeScript project.
type TSConfig struct {
	File     string
	Dir      string
	Include  []string
	Exclude  []string
	BaseURL  string
	Mode     string
	Suffixes []string
	Solution bool
	Aliases  []Alias
}

// Scope is a directory with its own package.json, inheriting parent aliases.
type Scope struct {
	Dir      string
	Name     string
	Type     string
	Deps     map[string]bool
	Imports  json.RawMessage
	Aliases  []Alias
	TS       []TSConfig
	ExtraMod []string
	Exts     []string
}

// Tool is a binary found on PATH while loading the project.
type Tool struct {
	Name    string
	Version string
	Path    string
}

// Project is everything stray knows about a repository root.
type Project struct {
	Root           string
	Scopes         []Scope
	Bundler        bool
	Nuxt           bool
	Vite           bool
	Webpack        bool
	Mode           string
	Tools          []Tool
	Notes          []string
	Configs        []string
	WorkspaceNames map[string]string
	BundlerName    string
}
