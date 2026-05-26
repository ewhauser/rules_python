# Rust Extractor Spike

This note records an initial feasibility pass for replacing the Python Gazelle
import extractor with a Rust-backed batch extractor, following the architecture
used by `hermeticbuild/gazelle_ts`.

## `gazelle_ts` Architecture

At commit `29010bf1ba220e78d28349f8d473305465c282e3`, `gazelle_ts` is not
tree-sitter-based. It uses Rust `oxc` to parse TypeScript, builds a
`rust_static_library`, and links it into the Go Gazelle language extension via
cgo.

The reusable pattern is:

- Go batches source file paths for a package.
- Go marshals a protobuf request and calls a plugin-namespaced C ABI.
- Rust reads and parses files in parallel with `rayon`.
- Rust returns parsed import/global data in a protobuf response.
- Go keeps Gazelle rule generation and dependency resolution logic.

For Python, the analogous boundary should return the existing `ParserOutput`
shape: file name, parsed imports, parsed Gazelle comments, and `has_main`.

## Current `rules_python` Parser

`rules_python` already uses tree-sitter through Go bindings:

- `FileParser.ParseFile` reads one file and calls `Parse`.
- `ParseCode` creates a Go tree-sitter parser and parses bytes.
- `python3Parser.parse` runs per-file parsing concurrently, capped at six
  concurrent parses.

A small benchmark harness was added in `parser_benchmark_test.go` to make this
path measurable.

On an Apple M5 Max:

| Benchmark | Result |
| --- | ---: |
| `BenchmarkFileParserParse` | about 129-156 us/op |
| raw tree-sitter parse with new parser | about 70-73 us/op |
| raw tree-sitter parse with reused parser | about 70-72 us/op |
| `BenchmarkPython3ParserParseManyFiles`, 256 files | about 8.4-8.8 ms/op |

Reusing the Go tree-sitter parser did not materially improve parse time.
Raising the current six-file concurrency cap made the 256-file parser benchmark
slower and noisier, so that experiment was reverted.

## End-to-End Baseline

A synthetic workspace with 512 packages and 16 Python files per package
generated in about 0.85-0.87 seconds after warmup with the current Gazelle
binary.

CPU profiling that run showed:

| Function | Cumulative CPU |
| --- | ---: |
| `python.(*FileParser).ParseFile` | 0.90s / 2.82s |
| `python.ParseCode` | 0.50s / 2.82s |
| `go-tree-sitter.(*Parser).ParseCtx` | 0.50s / 2.82s |
| `os.ReadFile` | 0.35s / 2.82s |
| `python.(*Resolver).Resolve` | 0.27s / 2.82s |

This means the parser is a real hot path, but parser replacement alone is not
guaranteed to produce a 2x end-to-end speedup for every workload.

## Throwaway Rust Spike

A temporary Rust tree-sitter spike was built in
`/private/tmp/rules-python-rust-parser-spike`. It approximates the existing
parser output by extracting imports, comments, and `if __name__ == "__main__"`
from the same synthetic source shape used by the Go benchmark.

With 256 files per operation:

| Rust mode | Result |
| --- | ---: |
| serial in-memory parse | about 5.7-5.9 ms/op |
| parallel in-memory parse | about 0.95-0.98 ms/op |
| parallel parse with file reads | about 2.56-2.61 ms/op |

Compared with the Go `python3Parser.parse` benchmark at about 8.4-8.8 ms/op,
the Rust file-reading spike is promising enough to justify an integrated
prototype.

## Integrated Bazel/cgo Spike

An integrated private prototype was added under
`gazelle/crates/python_import_extractor` with a Go bridge in
`gazelle/python/rustextractor`. The default Gazelle language target,
`//python:python`, now excludes the Go tree-sitter parser and links the Rust
bridge instead. A separate `//python:python_go` target keeps the previous Go
parser available for parity tests and benchmarks.

The first bridge used a NUL-separated request and JSON response to keep the ABI
small while preserving the current parsed shape. The current spike keeps the
NUL-separated request and returns a compact binary response. The `RPY3` response
omits the per-module filepath and per-file absolute path because the Go caller
already has that context. The Go bridge also decodes directly from the C-owned
response buffer instead of first copying the whole buffer. On the Rust side,
file paths borrowed from the request are parsed without copying, each Rayon
worker reuses a thread-local tree-sitter parser, and common vectors are
pre-sized for the synthetic import shape.

The Rust crate needs separate build shapes:

- a normal `cgo_rust_library` for Rust unit tests, because Rust test harnesses
  require unwind-compatible panic behavior;
- an optimized `opt_cgo_rust_library` for the cgo static library, matching the
  `gazelle_ts` pattern of pinning the Rust side to opt settings.

With the optimized cgo target, the focused benchmark now shows:

| Benchmark | Result |
| --- | ---: |
| current Go parser, 256 files | about 8.3-14.4 ms/op |
| Rust cgo bridge with JSON, 256 files | about 4.7-5.0 ms/op |
| Rust cgo bridge with binary response, 256 files | about 2.8 ms/op |
| Rust cgo bridge with `RPY2`, 256 files | about 3.6-4.6 ms/op |
| Rust cgo bridge with `RPY3`, 256 files | about 3.6-4.1 ms/op |

The low-noise JSON runs put the Rust bridge about 1.7-1.8x faster for this
parser/extractor workload. A later, longer pass under more host load measured
the Go path at 11.7-14.4 ms/op and the Rust bridge at 4.8-5.0 ms/op. The
binary response makes the parser-only path comfortably faster than 2x. A noisy
post-`RPY2` parser-only check measured the Go path at 13.2-47.1 ms/op and the
Rust bridge at 3.6-4.6 ms/op; allocations fell from roughly 5.8 MB/op on the Go
parser to roughly 0.30 MB/op on the Rust bridge. After `RPY3`, the bridge
measured 3.6-4.1 ms/op and allocations fell again to roughly 0.27 MB/op.

One important linker finding: linking the Rust tree-sitter crate and the Go
tree-sitter dependency into one final binary conflicts because both provide the
same C tree-sitter symbols. The default target avoids that by replacing the Go
tree-sitter parser in the final binary. The Go parser remains available only in
the separate `//python:python_go` comparison target.

## Behavior and End-to-End Checks

The default Rust-backed Gazelle binary now runs the existing golden fixture
suite through `//python:python_test`. The preserved Go parser path runs the same
suite through `//python:python_go_test`. Both paths pass the same 79 generated
Gazelle fixture tests.

Focused validation run:

- `//python:python_test`
- `//python:python_go_test`
- `//python:default_test`
- `//python/rustextractor:rustextractor_test`
- `//pythonconfig:pythonconfig_test`
- `//crates/python_import_extractor:test`

End-to-end benchmarks show that the speedup is sensitive to package shape and
batch boundary:

| Shape | Go | Rust | Speedup |
| --- | ---: | ---: | ---: |
| 512 pkg x 16 files, JSON | 896 ms | 808 ms | 1.11x |
| 32 pkg x 256 files, JSON | 1.43 s | 682 ms | 2.10x |
| 512 pkg x 16 files, cache + binary | 1.13 s | 476 ms | 2.37x |
| 512 pkg x 16 files, cache + binary + skips | 1.77 s | 799 ms | 2.22x |
| 512 pkg x 16 files, config-aware cache | 714 ms | 339 ms | 2.11x |
| 512 pkg x 16 files, Rust default target | 730 ms | 348 ms | 2.10x |
| 512 pkg x 16 files, `RPY3` borrowed request | 818 ms | 342 ms | 2.39x |

The repo cache walks the workspace once, asks Rust to extract every `.py` file
in one batch, and then lets package-level parsing reuse those results. Together
with the binary response, the 512-package synthetic workload crosses the 2x
line on mean time. The machine was noisy, but the medians also landed right at
about 2x: 992 ms for Go and 494 ms for Rust.

The current cache skips obvious non-source trees before sending paths to Rust:
Bazel output directories, hidden VCS/cache directories, virtualenv directories,
`node_modules`, and `__pycache__`. It also applies a repo-file filter from the
Gazelle config before preparse, so `gazelle:exclude` patterns and inherited
`python_ignore_files` directives do not get sent to Rust. Exact excluded
directory patterns also exclude their children.

A follow-up benchmark with the hard-coded skip rules still crossed the 2x line,
though with large local outliers: 1.77 s Go mean vs 799 ms Rust mean, 1.35 s Go
median vs 662 ms Rust median, and 1.55 s Go trimmed mean vs 689 ms Rust trimmed
mean.

After adding the config-aware preparse filter, a 20-run `-mode=diff` refresh on
the same 512-package synthetic workspace measured 714 ms mean and 639 ms median
for the Go parser, versus 339 ms mean and 335 ms median for the Rust cache. That
puts the Rust path at 2.11x on mean and 1.91x on median for this run.

After making `//python:gazelle_binary` the Rust-backed default and preserving
`//python:gazelle_binary_go` for comparison, a 40-run `-mode=diff` refresh on
the same workspace measured 730 ms mean and 666 ms median for the Go target,
versus 348 ms mean and 338 ms median for the Rust default. That puts the default
Rust path at 2.10x on mean and 1.97x on median for that run. A later post-`RPY2`
run was much noisier but still showed the Rust default ahead: 548 ms Rust mean
vs 1.57 s Go mean, and 465 ms Rust median vs 1.32 s Go median.

The latest `RPY3` run, with Rust listed first, measured 818 ms mean and 820 ms
median for the Go target, versus 342 ms mean and 336 ms median for the Rust
default. That puts the Rust target at 2.39x on mean and 2.44x on median. A
reverse-order guard run stayed noisier but still cleared 2x on median: 828 ms
Go median versus 400 ms Rust median.

A shared generation bottleneck also showed up while profiling: missing
`gazelle_python.yaml` manifests were repeatedly `stat`ed because a missing
manifest and an unloaded manifest were both represented as `nil`. The spike now
caches that missing-manifest state in `pythonconfig.Config`, with a focused
unit test.

## Current Prototype Shape

The current implementation now follows the usable parts of the `gazelle_ts`
shape:

1. A Rust crate lives under `gazelle/crates/python_import_extractor`.
2. The crate is built as a static cgo library with a namespaced C ABI.
3. Go batches file paths, calls Rust once per repository cache fill, and keeps
   annotations, target generation, and dependency resolution unchanged.
4. The default `//python:python` target uses the Rust extractor, while
   `//python:python_go` keeps the previous parser available for comparison.
5. The full `gazelle/python/testdata` golden suite passes on both paths.

The main integration risk is not parser logic; it is Bazel distribution cost.
Following `gazelle_ts` means adding Rust toolchain, crate, cgo, and platform
configuration to the Gazelle plugin module. That cost should be weighed against
the measured end-to-end speedup now that the prototype is wired into the
default target.

To make the 2x goal production-ready across package layouts, the next technical
experiments should be:

1. Compare the compact binary response with protobuf for maintainability and
   compatibility with the `gazelle_ts` pattern.
2. Profile the Rust extractor with Rust symbols enabled to separate file IO,
   tree-sitter parse, and response encoding costs.
3. Decide whether the Go comparison target should stay in-tree as a fallback or
   be removed before proposing the production patch.
