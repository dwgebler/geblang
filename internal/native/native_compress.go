package native

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"geblang/internal/runtime"
	"io"
)

func registerCompress(r *Registry) {
	r.Register("compress", "gzip", func(args []runtime.Value) (runtime.Value, error) {
		data, err := singleBytes(args, "compress.gzip")
		if err != nil {
			return nil, err
		}
		var out bytes.Buffer
		writer := gzip.NewWriter(&out)
		if _, err := writer.Write(data); err != nil {
			_ = writer.Close()
			return nil, err
		}
		if err := writer.Close(); err != nil {
			return nil, err
		}
		return runtime.Bytes{Value: out.Bytes()}, nil
	})
	r.Register("compress", "gunzip", func(args []runtime.Value) (runtime.Value, error) {
		data, budget, err := bytesWithDecompressBudget(args, "compress.gunzip")
		if err != nil {
			return nil, err
		}
		reader, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		defer reader.Close()
		out, err := budget.readAll(reader)
		if err != nil {
			return nil, fmt.Errorf("compress.gunzip: %w", err)
		}
		return runtime.Bytes{Value: out}, nil
	})
}

const defaultMaxDecompressedBytes = 256 << 20

// decompressBudget caps the total bytes decompressed by one call so a small archive cannot expand without bound.
type decompressBudget struct {
	limit     int64
	remaining int64
}

// bytesWithDecompressBudget reads (data, maxBytes = 256 MiB); maxBytes 0 disables the cap.
func bytesWithDecompressBudget(args []runtime.Value, label string) ([]byte, *decompressBudget, error) {
	if len(args) != 1 && len(args) != 2 {
		return nil, nil, fmt.Errorf("%s expects data and optional maxBytes", label)
	}
	data, ok := args[0].(runtime.Bytes)
	if !ok {
		return nil, nil, fmt.Errorf("%s expects a bytes argument", label)
	}
	limit := int64(defaultMaxDecompressedBytes)
	if len(args) == 2 {
		n, ok := AsInt64(args[1])
		if !ok || n < 0 {
			return nil, nil, fmt.Errorf("%s maxBytes must be a non-negative int", label)
		}
		limit = n
	}
	if limit == 0 {
		return data.Value, nil, nil
	}
	return data.Value, &decompressBudget{limit: limit, remaining: limit}, nil
}

func (b *decompressBudget) readAll(r io.Reader) ([]byte, error) {
	if b == nil {
		return io.ReadAll(r)
	}
	out, err := io.ReadAll(io.LimitReader(r, b.remaining+1))
	if err != nil {
		return nil, err
	}
	if int64(len(out)) > b.remaining {
		return nil, runtime.ClassifiedError{Class: "IOError", Message: fmt.Sprintf("decompressed data exceeds %d bytes", b.limit)}
	}
	b.remaining -= int64(len(out))
	return out, nil
}
