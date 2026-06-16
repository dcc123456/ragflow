/*
Copyright 2026 The InfiniFlow Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package utility

import (
	"os"
	"path/filepath"
	"runtime"
)

// GetProjectRoot returns the project root directory.
//
// It tries multiple strategies so that it works both in development (go run / go test)
// and in production (compiled binary running in Docker or bare metal):
//
//  1. RAGFLOW_CONF_DIR environment variable (explicit override)
//  2. Walk up from the executable directory to find a directory containing
//     go.mod (dev) or conf/ (production Docker layout)
//  3. Walk up from the working directory (same heuristic)
//  4. Walk up from this source file's compile-time path (dev only — the path
//     is embedded in the binary and may not exist at runtime)
//  5. Fallback to the current working directory or "."
func GetProjectRoot() string {
	// 1. Environment variable override
	if confDir := os.Getenv("RAGFLOW_CONF_DIR"); confDir != "" {
		return confDir
	}

	// 2. Walk up from the executable path (production: /ragflow/bin/admin_server → /ragflow)
	if exePath, err := os.Executable(); err == nil {
		// Resolve symlinks so the directory walk is reliable
		if resolved, err := filepath.EvalSymlinks(exePath); err == nil {
			exePath = resolved
		}
		if root := findProjectRoot(filepath.Dir(exePath)); root != "" {
			return root
		}
	}

	// 3. Walk up from the current working directory
	if wd, err := os.Getwd(); err == nil {
		if root := findProjectRoot(wd); root != "" {
			return root
		}
	}

	// 4. Walk up from the source file path (development only)
	_, curFile, _, ok := runtime.Caller(0)
	if ok {
		if root := findProjectRoot(filepath.Dir(curFile)); root != "" {
			return root
		}
	}

	// 5. Last-resort fallback: working directory or "."
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return "."
}

// findProjectRoot walks up from dir until it finds a directory that contains
// either a "go.mod" file (module root in development) or a "conf" directory
// (production layout where config files live). Returns "" if not found.
func findProjectRoot(dir string) string {
	for {
		// "go.mod" marker — present in development checkout
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		// "conf" directory — present in both dev and production Docker layout
		if info, err := os.Stat(filepath.Join(dir, "conf")); err == nil && info.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "" // reached filesystem root without a match
		}
		dir = parent
	}
}
