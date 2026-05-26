// Copyright 2026 The Bazel Authors. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of it at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package python

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bazel-contrib/rules_python/gazelle/pythonconfig"
)

func TestFindPythonFilesSkipsGeneratedAndDependencyDirs(t *testing.T) {
	root := t.TempDir()
	writeFile := func(path string) {
		t.Helper()
		fullPath := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fullPath, []byte("import os\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	writeFile("pkg/module.py")
	writeFile("bazel-out/generated.py")
	writeFile("bazel-workspace/generated.py")
	writeFile(".venv/lib/site-packages/pkg.py")
	writeFile("node_modules/tool/generated.py")
	writeFile("__pycache__/cached.py")

	paths, err := findPythonFiles(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 {
		t.Fatalf("got paths %#v, want exactly one source path", paths)
	}
	want := filepath.Join(root, "pkg/module.py")
	if paths[0] != want {
		t.Fatalf("got %q, want %q", paths[0], want)
	}
}

func TestFindPythonFilesUsesGazelleAndPythonIgnoreConfig(t *testing.T) {
	root := t.TempDir()
	writeFile := func(path string) {
		t.Helper()
		fullPath := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fullPath, []byte("import os\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	writeFile("root_keep.py")
	writeFile("generated.py")
	writeFile("pkg/keep.py")
	writeFile("pkg/generated.py")
	writeFile("pkg/local.py")
	writeFile("pkg/excluded/skip.py")
	writeFile("other/local.py")

	rootCfg := pythonconfig.New(root, "")
	rootCfg.AddIgnoreFile("generated.py")
	rootCfg.AddExcludedPattern(filepath.Join("pkg", "excluded"))
	pkgCfg := rootCfg.NewChild()
	pkgCfg.AddIgnoreFile("local.py")
	cfgs := pythonconfig.Configs{
		"":    rootCfg,
		"pkg": pkgCfg,
	}

	paths, err := findPythonFiles(root, newPythonRepoFileFilter(cfgs, rootCfg.ExcludedPatterns()))
	if err != nil {
		t.Fatal(err)
	}

	got := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		relPath, err := filepath.Rel(root, path)
		if err != nil {
			t.Fatal(err)
		}
		got[relPath] = struct{}{}
	}
	want := []string{
		"root_keep.py",
		filepath.Join("pkg", "keep.py"),
		filepath.Join("other", "local.py"),
	}
	if len(got) != len(want) {
		t.Fatalf("got paths %#v, want exactly %#v", got, want)
	}
	for _, relPath := range want {
		if _, ok := got[relPath]; !ok {
			t.Fatalf("got paths %#v, missing %q", got, relPath)
		}
	}
}
