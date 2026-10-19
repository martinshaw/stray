package config

import (
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/martinshaw/stray/internal/project"
)

const nodeEvalScript = `
const { pathToFileURL } = require("url");
const path = require("path");

function simplify(alias) {
  const out = [];
  if (!alias) return out;
  const list = Array.isArray(alias)
    ? alias
    : Object.entries(alias).map(([find, replacement]) => ({ find, replacement }));
  for (const item of list) {
    let find = item.find;
    const replacement = item.replacement;
    if (find instanceof RegExp) find = find.toString();
    if (typeof find !== "string" || typeof replacement !== "string") continue;
    out.push({ find, replacement });
  }
  return out;
}

(async () => {
  const file = process.argv[1];
  let mod;
  try {
    mod = await import(pathToFileURL(file).href);
  } catch (err) {
    try { mod = require(file); }
    catch (err2) {
      console.error(String(err2 && err2.stack || err2));
      process.exit(2);
    }
  }
  let cfg = mod && (mod.default ?? mod);
  if (cfg && typeof cfg === "object" && cfg.default) cfg = cfg.default;
  if (typeof cfg === "function") {
    try { cfg = await cfg({ mode: "development", command: "serve" }); }
    catch (e) { cfg = await cfg(); }
  }
  const alias = cfg && cfg.resolve && cfg.resolve.alias;
  process.stdout.write(JSON.stringify({ alias: simplify(alias) }));
})().catch((err) => {
  console.error(String(err && err.stack || err));
  process.exit(1);
});
`

func evalWithNode(configPath string) ([]project.Alias, []string, error) {
	node, err := exec.LookPath("node")
	if err != nil {
		return nil, nil, err
	}
	args := []string{}
	if strings.HasSuffix(configPath, ".ts") || strings.HasSuffix(configPath, ".mts") || strings.HasSuffix(configPath, ".cts") {
		if nodeMajor(node) >= 22 {
			args = append(args, "--experimental-strip-types")
		}
	}
	args = append(args, "-e", nodeEvalScript, configPath)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, args...)
	cmd.Dir = filepath.Dir(configPath)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, []string{"node could not evaluate " + filepath.Base(configPath)}, err
	}
	var parsed struct {
		Alias []struct {
			Find        string `json:"find"`
			Replacement string `json:"replacement"`
		} `json:"alias"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &parsed); err != nil {
		return nil, nil, err
	}
	var aliases []project.Alias
	for _, item := range parsed.Alias {
		prefix := item.Find
		if strings.HasPrefix(prefix, "/") {
			body := strings.Trim(prefix, "/")
			body = strings.TrimPrefix(body, "^")
			body = strings.TrimSuffix(body, "$")
			body = strings.ReplaceAll(body, `\/`, "/")
			body = strings.ReplaceAll(body, "(.*)", "")
			if strings.ContainsAny(body, "()[]{}|+?^$\\") {
				continue
			}
			prefix = body
		}
		if prefix == "" || item.Replacement == "" {
			continue
		}
		aliases = append(aliases, project.Alias{
			Prefix:      prefix,
			PrefixMatch: true,
			Targets:     []string{item.Replacement},
		})
	}
	return aliases, nil, nil
}

func nodeMajor(node string) int {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, node, "--version").Output()
	if err != nil {
		return 0
	}
	s := strings.TrimSpace(string(out))
	s = strings.TrimPrefix(s, "v")
	major := s
	if i := strings.Index(major, "."); i >= 0 {
		major = major[:i]
	}
	n := 0
	for _, c := range major {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n
}
