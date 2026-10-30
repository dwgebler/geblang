package native

import (
	"bytes"
	"errors"
	"geblang/internal/runtime"
	"io"
	"strings"
	"testing"
)

type shortJSONSink struct {
	bytes.Buffer
	limit     int
	failAfter int
}

func (s *shortJSONSink) Write(data []byte) (int, error) {
	if s.failAfter > 0 && s.Len() >= s.failAfter {
		return 0, io.ErrClosedPipe
	}
	n := len(data)
	if n > s.limit {
		n = s.limit
	}
	if s.failAfter > 0 && n > s.failAfter-s.Len() {
		n = s.failAfter - s.Len()
	}
	return s.Buffer.Write(data[:n])
}
func TestJSONWriteMatchesStringifyWithShortWrites(t *testing.T) {
	value, err := ParseJSONText(`{"z":1.25,"a":["line\n","text"]}`)
	if err != nil {
		t.Fatal(err)
	}
	expected, encodeErr := EncodeJSONValue(value)
	if encodeErr != nil {
		t.Fatal(encodeErr)
	}
	output := &shortJSONSink{limit: 2}

	count, writeErr := WriteJSONValueCtx(ConversionContext{}, value, output)

	if writeErr != nil || count != int64(len(expected)) || output.String() != expected {
		t.Fatalf("got %q / %d / %v, want %q", output.String(), count, writeErr, expected)
	}
}
func TestJSONWriteStopsOnPartialIOFailure(t *testing.T) {
	value := &runtime.List{}
	for i := 0; i < 5000; i++ {
		value.Elements = append(value.Elements, runtime.String{Value: strings.Repeat("a", 20)})
	}
	output := &shortJSONSink{limit: 3, failAfter: 20000}

	count, err := WriteJSONValueCtx(ConversionContext{}, value, output)

	if !errors.Is(err, io.ErrClosedPipe) || count != 20000 || output.Len() != 20000 {
		t.Fatalf("partial write: %d, %d, %v", count, output.Len(), err)
	}
}
func TestJSONWriteRejectsZeroProgressAndExcessiveCounts(t *testing.T) {
	for _, write := range []func([]byte) (int, error){func([]byte) (int, error) { return 0, nil }, func(data []byte) (int, error) { return len(data) + 1, nil }} {
		output := &jsonStreamWriter{write: write}
		if _, err := WriteJSONValueCtx(ConversionContext{}, runtime.SmallInt{Value: 1}, output); err == nil {
			t.Fatal("invalid writer succeeded")
		}
	}
}
func BenchmarkJSONWrite(b *testing.B) {
	value, err := ParseJSONText(benchJSON)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := WriteJSONValueCtx(ConversionContext{}, value, io.Discard); err != nil {
			b.Fatal(err)
		}
	}
}
