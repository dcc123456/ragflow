package common_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoRootLocate walks up from the current working directory to find the git
// repository root. It works for both the main checkout and git worktrees,
// where .git is a file rather than a directory.
func repoRootLocate(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	for i := 0; i < 12; i++ {
		if fi, err := os.Stat(filepath.Join(dir, ".git")); err == nil && fi != nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatal("could not locate repo root containing .git")
	return ""
}

// TestEEDeepDocImageRenamed guards the EE DeepDoc docker image naming
// convention: the image (and the container name used in docs/tooling) must be
// deepdoc_cpu_ee (ee at the end), matching the parse-method name already used
// in code (pdf_deepdoc_dispatch_ee.go). The legacy deepdoc_ee_cpu image name
// must not survive in build/docs/tooling.
func TestEEDeepDocImageRenamed(t *testing.T) {
	root := repoRootLocate(t)

	oldDockerfile := filepath.Join(root, "Dockerfile_deepdoc_ee_cpu")
	if _, err := os.Stat(oldDockerfile); err == nil {
		t.Fatalf("legacy Dockerfile %s must be renamed to Dockerfile_deepdoc_cpu_ee", oldDockerfile)
	}

	newDockerfile := filepath.Join(root, "Dockerfile_deepdoc_cpu_ee")
	if _, err := os.Stat(newDockerfile); err != nil {
		t.Fatalf("expected Dockerfile_deepdoc_cpu_ee at %s: %v", newDockerfile, err)
	}

	// Files that previously referenced the legacy image/container name. After
	// the rename each must drop deepdoc_ee_cpu and adopt deepdoc_cpu_ee.
	checked := []string{
		"Dockerfile_deepdoc_cpu_ee",
		"docs/develop/ee_deepdoc_parity.md",
		"docs/develop/ee_deepdoc_model_conversion.md",
		"tools/scripts/export_tsr_ee_onnx.py",
	}
	for _, rel := range checked {
		p := filepath.Join(root, rel)
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		body := string(data)
		if strings.Contains(body, "deepdoc_ee_cpu") {
			t.Errorf("%s still references legacy image name deepdoc_ee_cpu", rel)
		}
		if !strings.Contains(body, "deepdoc_cpu_ee") {
			t.Errorf("%s does not reference the new image name deepdoc_cpu_ee", rel)
		}
	}
}
