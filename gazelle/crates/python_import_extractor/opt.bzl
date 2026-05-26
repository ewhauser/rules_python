"""Rust target wrappers for cgo-compatible Python import extractor builds."""

load("@rules_rs//rs:rust_library.bzl", "rust_library")
load("@rules_rust//rust:rust_common.bzl", "COMMON_PROVIDERS")
load("@with_cfg.bzl", "with_cfg")

_ALLOCATOR_MANGLED_SYMBOLS = Label(
    "@rules_rust//rust/settings:experimental_use_allocator_libraries_with_mangled_symbols",
)

cgo_rust_library, _cgo_rust_library_internal = (
    with_cfg(rust_library, extra_providers = COMMON_PROVIDERS)
    .set(_ALLOCATOR_MANGLED_SYMBOLS, True)
    .build()
)

opt_cgo_rust_library, _opt_cgo_rust_library_internal = (
    with_cfg(rust_library, extra_providers = COMMON_PROVIDERS)
    .set("compilation_mode", "opt")
    .set(_ALLOCATOR_MANGLED_SYMBOLS, True)
    .set(
        Label("@rules_rust//:extra_rustc_flags"),
        [
            "-Ccodegen-units=1",
            "-Cpanic=abort",
            "-Cstrip=symbols",
            "-Clto=thin",
            "-Cembed-bitcode=yes",
        ],
    )
    .build()
)
