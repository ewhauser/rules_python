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
	"path"
	"path/filepath"

	"github.com/bazel-contrib/rules_python/gazelle/pythonconfig"
	"github.com/bmatcuk/doublestar/v4"
	"github.com/emirpasic/gods/lists/singlylinkedlist"
)

type repoFileFilter func(repoPath string) (bool, error)

func newPythonRepoFileFilter(
	cfgs pythonconfig.Configs,
	excludedPatterns *singlylinkedlist.List,
) repoFileFilter {
	excludePatterns := make([]string, 0)
	if excludedPatterns != nil {
		it := excludedPatterns.Iterator()
		for it.Next() {
			excludePatterns = append(excludePatterns, it.Value().(string))
		}
	}

	return func(repoPath string) (bool, error) {
		repoPath = filepath.Clean(repoPath)
		isExcluded, err := matchesExcludedRepoPath(excludePatterns, repoPath)
		if err != nil {
			return false, err
		}
		if isExcluded {
			return false, nil
		}

		relDir := path.Dir(filepath.ToSlash(repoPath))
		if relDir == "." {
			relDir = ""
		}
		cfg := cfgs[relDir]
		if cfg == nil {
			cfg = cfgs.ParentForPackage(relDir)
		}
		if cfg != nil && cfg.IgnoresFile(filepath.Base(repoPath)) {
			return false, nil
		}
		return true, nil
	}
}

func matchesExcludedRepoPath(patterns []string, repoPath string) (bool, error) {
	for _, pattern := range patterns {
		isExcluded, err := doublestar.Match(pattern, repoPath)
		if err != nil {
			return false, err
		}
		if isExcluded {
			return true, nil
		}
		for parent := filepath.Dir(repoPath); parent != "." && parent != ""; {
			isExcluded, err := doublestar.Match(pattern, parent)
			if err != nil {
				return false, err
			}
			if isExcluded {
				return true, nil
			}
			parent = filepath.Dir(parent)
		}
	}
	return false, nil
}
