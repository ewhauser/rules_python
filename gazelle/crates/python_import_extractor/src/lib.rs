mod parser;

use rayon::prelude::*;
use std::slice;

struct ParsedFile {
    modules: Vec<ParsedModule>,
    comments: Vec<String>,
    has_main: bool,
    error: Option<String>,
}

struct ParsedModule {
    name: String,
    line_number: u32,
    from: String,
    type_checking_only: bool,
}

fn encode_response(files: Vec<ParsedFile>) -> Vec<u8> {
    let mut out = Vec::with_capacity(response_capacity(&files));
    out.extend_from_slice(b"RPY3");
    write_u32(&mut out, files.len());
    for file in files {
        write_string(&mut out, file.error.as_deref().unwrap_or(""));
        out.push(u8::from(file.has_main));

        write_u32(&mut out, file.comments.len());
        for comment in file.comments {
            write_string(&mut out, &comment);
        }

        write_u32(&mut out, file.modules.len());
        for module in file.modules {
            write_string(&mut out, &module.name);
            out.extend_from_slice(&module.line_number.to_le_bytes());
            write_string(&mut out, &module.from);
            out.push(u8::from(module.type_checking_only));
        }
    }
    out
}

fn response_capacity(files: &[ParsedFile]) -> usize {
    const U32_LEN: usize = 4;
    const BOOL_LEN: usize = 1;

    let mut size = 4 + U32_LEN;
    for file in files {
        size += U32_LEN + file.error.as_deref().unwrap_or("").len();
        size += BOOL_LEN;

        size += U32_LEN;
        for comment in &file.comments {
            size += U32_LEN + comment.len();
        }

        size += U32_LEN;
        for module in &file.modules {
            size += U32_LEN + module.name.len();
            size += U32_LEN;
            size += U32_LEN + module.from.len();
            size += BOOL_LEN;
        }
    }
    size
}

fn write_u32(out: &mut Vec<u8>, value: usize) {
    out.extend_from_slice(&(value as u32).to_le_bytes());
}

fn write_string(out: &mut Vec<u8>, value: &str) {
    write_u32(out, value.len());
    out.extend_from_slice(value.as_bytes());
}

fn parse_request(req: &[u8]) -> Vec<&str> {
    req.split(|b| *b == 0)
        .filter(|part| !part.is_empty())
        .filter_map(|part| std::str::from_utf8(part).ok())
        .collect()
}

/// Parse a NUL-separated list of file paths and return a binary response.
///
/// # Safety
/// `req_ptr` must point to `req_len` valid bytes. `out_resp_ptr` and
/// `out_resp_len` must be valid out-pointers. The bytes written to
/// `*out_resp_ptr` are owned by the caller until
/// `rules_python_py_extract_free` is called with the same pointer and length.
#[no_mangle]
pub unsafe extern "C" fn rules_python_py_extract_dispatch(
    req_ptr: *const u8,
    req_len: usize,
    out_resp_ptr: *mut *mut u8,
    out_resp_len: *mut usize,
) {
    let req = if req_len == 0 {
        &[][..]
    } else {
        slice::from_raw_parts(req_ptr, req_len)
    };
    let files = parse_request(req)
        .into_par_iter()
        .map(|file| match parser::parse_file(&file) {
            Ok(parsed) => ParsedFile {
                modules: parsed
                    .modules
                    .into_iter()
                    .map(|module| ParsedModule {
                        name: module.name,
                        line_number: module.line_number,
                        from: module.from,
                        type_checking_only: module.type_checking_only,
                    })
                    .collect(),
                comments: parsed.comments,
                has_main: parsed.has_main,
                error: None,
            },
            Err(err) => ParsedFile {
                modules: Vec::new(),
                comments: Vec::new(),
                has_main: false,
                error: Some(err),
            },
        })
        .collect();

    let mut boxed = encode_response(files).into_boxed_slice();
    unsafe {
        *out_resp_len = boxed.len();
        *out_resp_ptr = boxed.as_mut_ptr();
    }
    std::mem::forget(boxed);
}

/// # Safety
/// `ptr` must have been returned by `rules_python_py_extract_dispatch` paired
/// with the same `len`. Calling with a null pointer is a no-op.
#[no_mangle]
pub unsafe extern "C" fn rules_python_py_extract_free(ptr: *mut u8, len: usize) {
    if ptr.is_null() || len == 0 {
        return;
    }
    let _ = Box::from_raw(slice::from_raw_parts_mut(ptr, len));
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::ptr;

    #[test]
    fn dispatch_empty_request_returns_empty_response() {
        let mut out_ptr: *mut u8 = ptr::null_mut();
        let mut out_len = 0usize;
        unsafe {
            rules_python_py_extract_dispatch(ptr::null(), 0, &mut out_ptr, &mut out_len);
        }
        assert!(!out_ptr.is_null());
        assert!(out_len > 0);
        let bytes = unsafe { slice::from_raw_parts(out_ptr, out_len) };
        assert_eq!(bytes, b"RPY3\0\0\0\0");
        unsafe {
            rules_python_py_extract_free(out_ptr, out_len);
        }
    }
}
