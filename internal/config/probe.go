package config

import (
	"context"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/martinshaw/stray/internal/project"
)

func probeTools() []project.Tool {
	names := []string{"node", "npm", "pnpm", "yarn", "bun", "tsc", "vue-tsc", "vite", "webpack", "nuxt", "eslint"}
	out := make([]project.Tool, len(names))
	var wg sync.WaitGroup
	for i, name := range names {
		wg.Add(1)
		go func(i int, name string) {
			defer wg.Done()
			out[i] = probeOne(name)
		}(i, name)
	}
	wg.Wait()
	var found []project.Tool
	for _, tool := range out {
		if tool.Path != "" {
			found = append(found, tool)
		}
	}
	return found
}

func probeOne(name string) project.Tool {
	path, err := exec.LookPath(name)
	if err != nil {
		return project.Tool{Name: name}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--version")
	buf, err := cmd.Output()
	ver := ""
	if err == nil {
		line := strings.TrimSpace(string(buf))
		if i := strings.IndexByte(line, '\n'); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if len(line) > 60 {
			line = line[:60]
		}
		ver = line
	}
	return project.Tool{Name: name, Path: path, Version: ver}
}
