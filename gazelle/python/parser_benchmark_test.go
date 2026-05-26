// Copyright 2026 The Bazel Authors. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
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
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/emirpasic/gods/sets/treeset"
	godsutils "github.com/emirpasic/gods/utils"
	sitter "github.com/smacker/go-tree-sitter"
	sitterpython "github.com/smacker/go-tree-sitter/python"
)

func benchmarkPythonSource(index int) string {
	return fmt.Sprintf(`# gazelle:ignore ignored.module.%[1]d
import os
import sys as system
from pathlib import Path
from typing import TYPE_CHECKING
from package_%[1]d import alpha, beta as beta_alias

if TYPE_CHECKING:
    from type_package_%[1]d import TypedThing

def run(value):
    from local_%[1]d import helper
    return helper(Path(str(value)))

if __name__ == "__main__":
    run(%[1]d)
`, index)
}

func BenchmarkFileParserParse(b *testing.B) {
	code := []byte(benchmarkPythonSource(1))
	b.SetBytes(int64(len(code)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p := NewFileParser()
		p.SetCodeAndFile(code, "pkg", "module.py")
		if _, err := p.Parse(context.Background()); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkTreeSitterParseNewParser(b *testing.B) {
	code := []byte(benchmarkPythonSource(1))
	b.SetBytes(int64(len(code)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		parser := sitter.NewParser()
		parser.SetLanguage(sitterpython.GetLanguage())
		tree, err := parser.ParseCtx(context.Background(), nil, code)
		if err != nil {
			b.Fatal(err)
		}
		tree.Close()
		parser.Close()
	}
}

func BenchmarkTreeSitterParseReusedParser(b *testing.B) {
	code := []byte(benchmarkPythonSource(1))
	parser := sitter.NewParser()
	parser.SetLanguage(sitterpython.GetLanguage())
	b.Cleanup(parser.Close)

	b.SetBytes(int64(len(code)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tree, err := parser.ParseCtx(context.Background(), nil, code)
		if err != nil {
			b.Fatal(err)
		}
		tree.Close()
	}
}

func BenchmarkPython3ParserParseManyFiles(b *testing.B) {
	const fileCount = 256
	dir := b.TempDir()
	filenames := treeset.NewWith(godsutils.StringComparator)
	var totalBytes int64
	for i := 0; i < fileCount; i++ {
		name := fmt.Sprintf("module_%03d.py", i)
		code := []byte(benchmarkPythonSource(i))
		if err := os.WriteFile(filepath.Join(dir, name), code, 0o644); err != nil {
			b.Fatal(err)
		}
		filenames.Add(name)
		totalBytes += int64(len(code))
	}
	parser := newPython3Parser(dir, "", func(string) bool { return false }, nil)

	b.SetBytes(totalBytes)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, _, err := parser.parse(filenames); err != nil {
			b.Fatal(err)
		}
	}
}
