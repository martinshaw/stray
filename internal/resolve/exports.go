package resolve

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/martinshaw/stray/internal/jsonc"
)

func resolveExports(exports json.RawMessage, subpath string, conditions []string) (string, string) {
	exports = bytes.TrimSpace(exports)
	if len(exports) == 0 || bytes.Equal(exports, []byte("null")) {
		return "", "missing"
	}
	return walkExport(exports, subpath, conditions, false)
}

func walkExport(raw json.RawMessage, subpath string, conditions []string, consumed bool) (string, string) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return "", "denied"
	}
	switch raw[0] {
	case '"':
		if !consumed && subpath != "." {
			return "", "denied"
		}
		var target string
		if err := json.Unmarshal(raw, &target); err != nil || target == "" {
			return "", "denied"
		}
		return target, "ok"
	case '[':
		var arr []json.RawMessage
		if json.Unmarshal(raw, &arr) != nil {
			return "", "denied"
		}
		for _, el := range arr {
			target, status := walkExport(el, subpath, conditions, consumed)
			if status == "ok" {
				return target, "ok"
			}
		}
		return "", "denied"
	case '{':
		kvs, ok, err := jsonc.Object(raw)
		if err != nil || !ok {
			return "", "denied"
		}
		subpathMap := false
		for _, kv := range kvs {
			if strings.HasPrefix(kv.Key, ".") || strings.HasPrefix(kv.Key, "#") {
				subpathMap = true
				break
			}
		}
		if subpathMap {
			for _, kv := range kvs {
				if kv.Key == subpath {
					return walkExport(kv.Val, ".", conditions, true)
				}
			}
			bestLen := -1
			var best json.RawMessage
			capture := ""
			for _, kv := range kvs {
				got, matched := matchStar(kv.Key, subpath)
				if !matched || len(kv.Key) <= bestLen {
					continue
				}
				bestLen = len(kv.Key)
				best = kv.Val
				capture = got
			}
			if bestLen < 0 {
				return "", "denied"
			}
			target, status := walkExport(best, ".", conditions, true)
			if status != "ok" {
				return "", status
			}
			if strings.Contains(target, "*") {
				target = strings.Replace(target, "*", capture, 1)
			}
			return target, "ok"
		}
		for _, kv := range kvs {
			if kv.Key != "default" && !containsStr(conditions, kv.Key) {
				continue
			}
			target, status := walkExport(kv.Val, subpath, conditions, consumed)
			if status == "ok" || status == "denied" {
				return target, status
			}
		}
		return "", "denied"
	default:
		return "", "denied"
	}
}

func matchStar(pattern, name string) (string, bool) {
	star := strings.Index(pattern, "*")
	if star < 0 {
		if pattern == name {
			return "", true
		}
		return "", false
	}
	prefix, suffix := pattern[:star], pattern[star+1:]
	if len(name) < len(prefix)+len(suffix) {
		return "", false
	}
	if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
		return "", false
	}
	return name[len(prefix) : len(name)-len(suffix)], true
}

func containsStr(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}
