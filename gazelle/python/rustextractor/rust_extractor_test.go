package rustextractor

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func benchmarkSource(index int) string {
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

func TestExtractFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "module.py")
	if err := os.WriteFile(path, []byte(benchmarkSource(1)), 0o644); err != nil {
		t.Fatal(err)
	}

	outputs, err := ExtractFiles([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if len(outputs) != 1 {
		t.Fatalf("got %d files, want 1", len(outputs))
	}
	out := outputs[0]
	if !out.HasMain {
		t.Fatal("expected has_main")
	}
	if len(out.Comments) != 1 || out.Comments[0] != "# gazelle:ignore ignored.module.1" {
		t.Fatalf("unexpected comments: %#v", out.Comments)
	}
	wantModules := []string{
		"os",
		"sys",
		"pathlib.Path",
		"typing.TYPE_CHECKING",
		"package_1.alpha",
		"package_1.beta",
		"type_package_1.TypedThing",
		"local_1.helper",
	}
	if len(out.Modules) != len(wantModules) {
		t.Fatalf("got modules %#v, want %d modules", out.Modules, len(wantModules))
	}
	for idx, want := range wantModules {
		if out.Modules[idx].Name != want {
			t.Fatalf("module %d: got %q, want %q", idx, out.Modules[idx].Name, want)
		}
	}
	if !out.Modules[6].TypeCheckingOnly {
		t.Fatalf("expected TYPE_CHECKING import to be marked type-checking only")
	}
}

func BenchmarkExtractFiles(b *testing.B) {
	const fileCount = 256
	dir := b.TempDir()
	files := make([]string, 0, fileCount)
	var totalBytes int64
	for idx := 0; idx < fileCount; idx++ {
		path := filepath.Join(dir, fmt.Sprintf("module_%03d.py", idx))
		code := []byte(benchmarkSource(idx))
		if err := os.WriteFile(path, code, 0o644); err != nil {
			b.Fatal(err)
		}
		files = append(files, path)
		totalBytes += int64(len(code))
	}

	b.SetBytes(totalBytes)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := ExtractFiles(files); err != nil {
			b.Fatal(err)
		}
	}
}
