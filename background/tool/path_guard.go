package tool

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type pathGuard struct {
	roots []string
}

func newPathGuard(roots ...string) pathGuard {
	guard := pathGuard{roots: make([]string, 0, len(roots))}
	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		abs, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		guard.roots = append(guard.roots, filepath.Clean(abs))
	}
	return guard
}

func (g pathGuard) describe() string {
	if len(g.roots) == 0 {
		return "any absolute path"
	}
	return strings.Join(g.roots, ", ")
}

func (g pathGuard) resolve(rawPath string, forWrite bool) (string, error) {
	rawPath = strings.TrimSpace(rawPath)
	if rawPath == "" {
		return "", fmt.Errorf("path is required")
	}
	if len(g.roots) == 0 {
		if !filepath.IsAbs(rawPath) {
			return "", fmt.Errorf("path must be absolute")
		}
		return filepath.Clean(rawPath), nil
	}

	var candidate string
	if filepath.IsAbs(rawPath) {
		candidate = filepath.Clean(rawPath)
	} else {
		candidate = filepath.Join(g.roots[0], rawPath)
	}
	abs, err := filepath.Abs(candidate)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)

	checkedPath := abs
	if forWrite {
		checkedPath = filepath.Dir(abs)
		if err := os.MkdirAll(checkedPath, 0o755); err != nil {
			return "", err
		}
	}
	if real, err := filepath.EvalSymlinks(checkedPath); err == nil {
		if forWrite {
			abs = filepath.Join(real, filepath.Base(abs))
		} else {
			abs = real
		}
	}

	for _, root := range g.roots {
		if isWithinRoot(abs, root) {
			return abs, nil
		}
	}
	return "", fmt.Errorf("path %s is outside allowed roots: %s", rawPath, g.describe())
}

func isWithinRoot(path string, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
