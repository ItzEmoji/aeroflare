package compress

import (
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"hash"
	"io"

	"github.com/klauspost/compress/zstd"
	"github.com/ulikunitz/xz"
)

// Type represents a compression algorithm.
type Type string

const (
	None Type = "none"
	Xz   Type = "xz"
	Gzip Type = "gzip"
	Zstd Type = "zstd"
)

// Extension returns the file extension suffix for the compression type.
// e.g. "nar.zst" for Zstd, "nar.xz" for Xz, "nar" for None.
func (t Type) Extension() string {
	switch t {
	case None:
		return "nar"
	case Xz:
		return "nar.xz"
	case Gzip:
		return "nar.gz"
	case Zstd:
		return "nar.zst"
	default:
		return "nar." + string(t)
	}
}

// ParseType parses a string into a compression Type.
func ParseType(s string) (Type, error) {
	switch s {
	case "none":
		return None, nil
	case "xz":
		return Xz, nil
	case "gzip", "gz":
		return Gzip, nil
	case "zstd", "zst":
		return Zstd, nil
	default:
		return "", fmt.Errorf("unsupported compression type: %s (supported: none, xz, gzip, zstd)", s)
	}
}

type countingWriter struct {
	w io.Writer
	n int64
}

func (cw *countingWriter) Write(p []byte) (int, error) {
	n, err := cw.w.Write(p)
	cw.n += int64(n)
	return n, err
}

type nopWriteCloser struct {
	io.Writer
}

func (nopWriteCloser) Close() error { return nil }

// Writer wraps an io.Writer with compression and tracks the SHA-256 hash
// and total size of the compressed output.
type Writer struct {
	cw     *countingWriter
	hasher hash.Hash
	comp   io.WriteCloser
}

// NewWriter creates a compression writer that wraps the underlying writer w.
// Data written to the returned Writer is compressed and written to w.
// After Close, the hash and size of the compressed output can be retrieved.
func NewWriter(w io.Writer, t Type) (*Writer, error) {
	h := sha256.New()
	cw := &countingWriter{w: io.MultiWriter(w, h)}

	var comp io.WriteCloser
	var err error
	switch t {
	case None:
		comp = nopWriteCloser{Writer: cw}
	case Xz:
		comp, err = xz.NewWriter(cw)
		if err != nil {
			return nil, fmt.Errorf("create xz writer: %w", err)
		}
	case Gzip:
		comp = gzip.NewWriter(cw)
	case Zstd:
		comp, err = zstd.NewWriter(cw)
		if err != nil {
			return nil, fmt.Errorf("create zstd writer: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported compression type: %s", t)
	}

	return &Writer{
		cw:     cw,
		hasher: h,
		comp:   comp,
	}, nil
}

// Write compresses data and writes it to the underlying writer.
func (w *Writer) Write(p []byte) (int, error) {
	return w.comp.Write(p)
}

// Close closes the compression writer, flushing any buffered data.
// Must be called before Hash() and Size().
func (w *Writer) Close() error {
	return w.comp.Close()
}

// Hash returns the SHA-256 hash of the compressed output.
// Must be called after Close.
func (w *Writer) Hash() []byte {
	return w.hasher.Sum(nil)
}

// Size returns the total size in bytes of the compressed output.
// Must be called after Close.
func (w *Writer) Size() int64 {
	return w.cw.n
}
