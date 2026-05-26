package rustextractor

/*
#include <stddef.h>
#include <stdint.h>

void rules_python_py_extract_dispatch(
    const uint8_t *req_ptr,
    size_t req_len,
    uint8_t **out_resp_ptr,
    size_t *out_resp_len);

void rules_python_py_extract_free(uint8_t *ptr, size_t len);
*/
import "C"

import (
	"fmt"
	"strings"
	"unsafe"
)

type ParserOutput struct {
	FileName string
	Modules  []Module
	Comments []string
	HasMain  bool
}

type Module struct {
	Name             string
	LineNumber       uint32
	Filepath         string
	From             string
	TypeCheckingOnly bool
}

func ExtractFile(file string) (ParserOutput, error) {
	outputs, err := ExtractFiles([]string{file})
	if err != nil {
		return ParserOutput{}, err
	}
	if len(outputs) != 1 {
		return ParserOutput{}, fmt.Errorf("rust extractor returned %d files, want 1", len(outputs))
	}
	return outputs[0], nil
}

func ExtractFiles(files []string) ([]ParserOutput, error) {
	req := []byte(strings.Join(files, "\x00"))
	if len(req) > 0 {
		req = append(req, 0)
	}

	var reqPtr *C.uint8_t
	if len(req) > 0 {
		reqPtr = (*C.uint8_t)(unsafe.Pointer(&req[0]))
	}

	var respPtr *C.uint8_t
	var respLen C.size_t
	C.rules_python_py_extract_dispatch(reqPtr, C.size_t(len(req)), &respPtr, &respLen)
	if respPtr == nil || respLen == 0 {
		return nil, fmt.Errorf("rust extractor returned an empty response")
	}
	defer C.rules_python_py_extract_free(respPtr, respLen)

	respSize := int(respLen)
	if C.size_t(respSize) != respLen {
		return nil, fmt.Errorf("rust extractor response is too large: %d bytes", respLen)
	}
	respBytes := unsafe.Slice((*byte)(unsafe.Pointer(respPtr)), respSize)
	return decodeResponse(respBytes, files)
}

type responseReader struct {
	data []byte
	pos  int
}

func decodeResponse(data []byte, files []string) ([]ParserOutput, error) {
	reader := &responseReader{data: data}
	magic, err := reader.readBytes(4)
	if err != nil {
		return nil, err
	}
	if string(magic) != "RPY3" {
		return nil, fmt.Errorf("decode rust extractor response: bad magic %q", string(magic))
	}

	fileCount, err := reader.readU32()
	if err != nil {
		return nil, err
	}
	if int(fileCount) != len(files) {
		return nil, fmt.Errorf("rust extractor returned %d files, want %d", fileCount, len(files))
	}
	outputs := make([]ParserOutput, 0, fileCount)
	for idx := 0; idx < int(fileCount); idx++ {
		filename := files[idx]
		fileErr, err := reader.readString()
		if err != nil {
			return nil, err
		}
		hasMain, err := reader.readBool()
		if err != nil {
			return nil, err
		}

		commentCount, err := reader.readU32()
		if err != nil {
			return nil, err
		}
		comments := make([]string, 0, commentCount)
		for commentIdx := 0; commentIdx < int(commentCount); commentIdx++ {
			comment, err := reader.readString()
			if err != nil {
				return nil, err
			}
			comments = append(comments, comment)
		}

		moduleCount, err := reader.readU32()
		if err != nil {
			return nil, err
		}
		modules := make([]Module, 0, moduleCount)
		for moduleIdx := 0; moduleIdx < int(moduleCount); moduleIdx++ {
			module, err := reader.readModule()
			if err != nil {
				return nil, err
			}
			modules = append(modules, module)
		}

		if fileErr != "" {
			return nil, fmt.Errorf("rust extractor failed for %s: %s", filename, fileErr)
		}
		out := ParserOutput{
			FileName: filename,
			HasMain:  hasMain,
			Modules:  modules,
			Comments: comments,
		}
		outputs = append(outputs, out)
	}
	if reader.pos != len(reader.data) {
		trailingBytes := len(reader.data) - reader.pos
		return nil, fmt.Errorf("decode rust extractor response: %d trailing bytes", trailingBytes)
	}
	return outputs, nil
}

func (r *responseReader) readModule() (Module, error) {
	name, err := r.readString()
	if err != nil {
		return Module{}, err
	}
	lineNumber, err := r.readU32()
	if err != nil {
		return Module{}, err
	}
	from, err := r.readString()
	if err != nil {
		return Module{}, err
	}
	typeCheckingOnly, err := r.readBool()
	if err != nil {
		return Module{}, err
	}
	return Module{
		Name:             name,
		LineNumber:       lineNumber,
		From:             from,
		TypeCheckingOnly: typeCheckingOnly,
	}, nil
}

func (r *responseReader) readString() (string, error) {
	size, err := r.readU32()
	if err != nil {
		return "", err
	}
	bytes, err := r.readBytes(int(size))
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

func (r *responseReader) readBool() (bool, error) {
	bytes, err := r.readBytes(1)
	if err != nil {
		return false, err
	}
	return bytes[0] != 0, nil
}

func (r *responseReader) readU32() (uint32, error) {
	bytes, err := r.readBytes(4)
	if err != nil {
		return 0, err
	}
	return uint32(bytes[0]) |
		uint32(bytes[1])<<8 |
		uint32(bytes[2])<<16 |
		uint32(bytes[3])<<24, nil
}

func (r *responseReader) readBytes(size int) ([]byte, error) {
	if size < 0 || r.pos+size > len(r.data) {
		return nil, fmt.Errorf("decode rust extractor response: truncated at byte %d", r.pos)
	}
	bytes := r.data[r.pos : r.pos+size]
	r.pos += size
	return bytes, nil
}
