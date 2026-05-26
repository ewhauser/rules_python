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
	"strings"
	"sync"

	"github.com/bazel-contrib/rules_python/gazelle/python/rustextractor"
	"github.com/emirpasic/gods/sets/treeset"
)

type rustParseCache struct {
	once    sync.Once
	outputs map[string]rustextractor.ParserOutput
	err     error
}

var rustParseCaches sync.Map

func (p *python3Parser) parseFiles(pyFilenames *treeset.Set) ([]*ParserOutput, error) {
	cache, err := getRustParseCache(p.repoRoot, p.shouldParseRepoFile)
	if err != nil {
		return nil, err
	}

	values := pyFilenames.Values()
	filenames := make([]string, 0, len(values))
	extracted := make([]rustextractor.ParserOutput, 0, len(values))
	for _, value := range values {
		filename := value.(string)
		path := filepath.Join(p.repoRoot, p.relPackagePath, filename)
		file, ok := cache.outputs[path]
		if !ok {
			file, err = rustextractor.ExtractFile(path)
			if err != nil {
				return nil, err
			}
		}
		filenames = append(filenames, filename)
		extracted = append(extracted, file)
	}

	results := make([]*ParserOutput, 0, len(extracted))
	for idx, file := range extracted {
		filename := filenames[idx]
		relFilepath := filepath.Join(p.relPackagePath, filename)
		out := &ParserOutput{
			FileName: filename,
			Comments: make([]Comment, 0, len(file.Comments)),
			Modules:  make([]Module, 0, len(file.Modules)),
			HasMain:  file.HasMain,
		}
		for _, comment := range file.Comments {
			out.Comments = append(out.Comments, Comment(comment))
		}
		for _, module := range file.Modules {
			out.Modules = append(out.Modules, Module{
				Name:             module.Name,
				LineNumber:       module.LineNumber,
				Filepath:         relFilepath,
				From:             module.From,
				TypeCheckingOnly: module.TypeCheckingOnly,
			})
		}
		results = append(results, out)
	}
	return results, nil
}

func getRustParseCache(repoRoot string, shouldParse repoFileFilter) (*rustParseCache, error) {
	cache, _ := rustParseCaches.LoadOrStore(repoRoot, &rustParseCache{})
	rustCache := cache.(*rustParseCache)
	rustCache.once.Do(func() {
		paths, err := findPythonFiles(repoRoot, shouldParse)
		if err != nil {
			rustCache.err = err
			return
		}

		extracted, err := rustextractor.ExtractFiles(paths)
		if err != nil {
			rustCache.err = err
			return
		}

		outputs := make(map[string]rustextractor.ParserOutput, len(extracted))
		for _, file := range extracted {
			outputs[file.FileName] = file
		}
		rustCache.outputs = outputs
	})
	return rustCache, rustCache.err
}

func findPythonFiles(repoRoot string, shouldParse repoFileFilter) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(repoRoot, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != repoRoot && shouldSkipRustPreparseDir(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) == ".py" {
			if shouldParse != nil {
				relPath, err := filepath.Rel(repoRoot, path)
				if err != nil {
					return err
				}
				parse, err := shouldParse(relPath)
				if err != nil {
					return err
				}
				if !parse {
					return nil
				}
			}
			paths = append(paths, path)
		}
		return nil
	})
	return paths, err
}

func shouldSkipRustPreparseDir(name string) bool {
	switch name {
	case ".git", ".hg", ".mypy_cache", ".pytest_cache", ".ruff_cache",
		".svn", ".tox", ".venv", "__pycache__", "bazel-bin",
		"bazel-out", "bazel-testlogs", "node_modules", "venv":
		return true
	}
	return strings.HasPrefix(name, "bazel-")
}
