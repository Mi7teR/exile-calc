// Package pobcode converts Path of Building export codes to and from a Build.
//
// A PoB code is the build XML, zlib-compressed, base64-encoded with the
// URL-safe alphabet ("+" -> "-", "/" -> "_"). Source: src/Classes/ImportTab.lua
// (generateCodeOut / ImportBuild) in PathOfBuilding Community.
package pobcode

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
)

// maxXMLSize caps the inflated XML: codes come from users, so a small code
// must not inflate into gigabytes.
const maxXMLSize = 32 << 20

var errTooLarge = errors.New("pobcode: build XML exceeds 32 MiB")

// Decode turns a PoB code into build XML. It accepts both base64 alphabets,
// with or without padding, and ignores whitespace and line breaks.
func Decode(code string) ([]byte, error) {
	clean := strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\t', '\r', '\n', '=':
			return -1
		case '-':
			return '+'
		case '_':
			return '/'
		}
		return r
	}, code)
	if clean == "" {
		return nil, errors.New("pobcode: empty code")
	}
	compressed, err := base64.RawStdEncoding.DecodeString(clean)
	if err != nil {
		return nil, fmt.Errorf("pobcode: base64: %w", err)
	}
	zr, err := zlib.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, fmt.Errorf("pobcode: zlib: %w", err)
	}
	defer zr.Close()
	x, err := io.ReadAll(io.LimitReader(zr, maxXMLSize+1))
	if err != nil {
		return nil, fmt.Errorf("pobcode: zlib: %w", err)
	}
	if len(x) > maxXMLSize {
		return nil, errTooLarge
	}
	return x, nil
}

// Encode turns build XML into a PoB code the way PoB does: zlib, then
// base64 with the URL-safe alphabet and padding.
func Encode(xml []byte) (string, error) {
	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	if _, err := zw.Write(xml); err != nil {
		return "", fmt.Errorf("pobcode: zlib: %w", err)
	}
	if err := zw.Close(); err != nil {
		return "", fmt.Errorf("pobcode: zlib: %w", err)
	}
	return base64.URLEncoding.EncodeToString(buf.Bytes()), nil
}

// FromCode decodes a PoB code and parses the build.
func FromCode(code string) (*Build, error) {
	x, err := Decode(code)
	if err != nil {
		return nil, err
	}
	return Parse(x)
}

// Code serialises the build to XML and encodes it as a PoB code.
func (b *Build) Code() (string, error) {
	x, err := b.XML()
	if err != nil {
		return "", err
	}
	return Encode(x)
}
