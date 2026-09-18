package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// physicalSandboxPath resolves the configured root once, then refuses every
// symlink below that root. Kernel callers may create a missing final path after
// this check, but may never traverse an existing symlink to escape the physical
// sandbox.
func physicalSandboxPath(root, rel, label string) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return "", fmt.Errorf("%s root required", label)
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(absRoot, 0755); err != nil {
		return "", err
	}
	realRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return "", fmt.Errorf("%s root resolution failed: %w", label, err)
	}
	realRoot, err = filepath.Abs(realRoot)
	if err != nil {
		return "", err
	}

	rel = strings.TrimSpace(rel)
	if rel == "" || filepath.IsAbs(rel) {
		return "", fmt.Errorf("%s path must be relative", label)
	}
	clean := filepath.Clean(rel)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("%s path escapes root: %q", label, rel)
	}
	target := filepath.Join(realRoot, clean)
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	prefix := realRoot + string(os.PathSeparator)
	if absTarget != realRoot && !strings.HasPrefix(absTarget, prefix) {
		return "", fmt.Errorf("%s path escapes root: %q", label, rel)
	}

	current := realRoot
	for _, part := range strings.Split(clean, string(os.PathSeparator)) {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, lstatErr := os.Lstat(current)
		if lstatErr == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				return "", fmt.Errorf("%s path contains symlink component: %q", label, rel)
			}
			continue
		}
		if errors.Is(lstatErr, os.ErrNotExist) {
			break
		}
		return "", lstatErr
	}
	return absTarget, nil
}
