package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This server no longer asks anyone for a license: it neither gates requests behind an
// activation nor talks to a licensing service. The original licensing runtime (pkg/core)
// was removed on purpose, see docs/LICENCA-ANALISE.md. This test fails if code that brings
// the gate or the phone-home back is added, so that it cannot return unnoticed.
func TestNoLicenseGateOrPhoneHome(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}

	forbidden := []string{
		"evolutionfoundation.com", // the upstream's licensing host
		"LICENSE_REQUIRED",        // the gate's error code
		"/v1/heartbeat",           // periodic report to the licensing service
		"/v1/activate",
		"/license/",
		"runtime_configs", // where the activation was kept
	}

	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "manager", "passkey-helper", ".claude":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, word := range forbidden {
			if strings.Contains(string(src), word) {
				rel, _ := filepath.Rel(root, path)
				t.Errorf("%s mentions %q: the license gate and its phone-home were removed, see docs/LICENCA-ANALISE.md", rel, word)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(root, "pkg", "core")); err == nil {
		t.Error("pkg/core (the license runtime) is back")
	}
}
