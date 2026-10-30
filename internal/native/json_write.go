package native

import (
	"bytes"
	"fmt"
	"geblang/internal/runtime"
	"io"
)

type jsonStreamWriter struct {
	write     func([]byte) (int, error)
	count     int64
	stabilize bool
}

func (w *jsonStreamWriter) Write(data []byte) (int, error) {
	if w.stabilize {
		data = append([]byte(nil), data...)
	}
	total := 0
	for total < len(data) {
		n, err := w.write(data[total:])
		if n < 0 || n > len(data)-total {
			return total, runtime.ClassifiedError{Class: "ValueError", Message: "json.write: invalid write count"}
		}
		total += n
		w.count += int64(n)
		if err != nil {
			return total, err
		}
		if n == 0 {
			return total, runtime.ClassifiedError{Class: "IOError", Message: "json.write: stream made no progress"}
		}
	}
	return total, nil
}

const jsonStreamBufferSize = 16384

type jsonStreamState struct{ destination *jsonStreamWriter }

func (s *jsonStreamState) flush(buffer *bytes.Buffer) error {
	_, err := s.destination.Write(buffer.Bytes())
	buffer.Reset()
	return err
}

// WriteJSONValueCtx owns only its buffer; the destination remains borrowed.
func WriteJSONValueCtx(ctx ConversionContext, value runtime.Value, writer io.Writer) (int64, error) {
	destination := &jsonStreamWriter{write: writer.Write}
	ctx.jsonStream = &jsonStreamState{destination: destination}
	buffer := &bytes.Buffer{}
	buffer.Grow(jsonStreamBufferSize)
	if err := encodeJSONValueInto(ctx, buffer, value); err != nil {
		return destination.count, err
	}
	err := ctx.jsonStream.flush(buffer)
	return destination.count, err
}

func jsonWriteCtx(ctx ConversionContext, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("json.write expects value and stream")
	}
	stream := args[1]
	if _, path := stream.(runtime.String); path {
		return nil, runtime.ClassifiedError{Class: "TypeError", Message: "json.write requires an open writable stream, not a path"}
	}
	_, instanceStream := stream.(*runtime.Instance)
	writer := &jsonStreamWriter{stabilize: instanceStream, write: func(data []byte) (int, error) {
		if instance, ok := stream.(*runtime.Instance); ok && ctx.InstanceInvoker != nil {
			result, found, err := ctx.InstanceInvoker(instance, "__writeBytes", []runtime.Value{runtime.Bytes{Value: data}})
			if !found && err == nil {
				result, found, err = ctx.InstanceInvoker(instance, "__write", []runtime.Value{runtime.String{Value: string(data)}})
			}
			if err != nil {
				return 0, err
			}
			if !found {
				return 0, runtime.ClassifiedError{Class: "TypeError", Message: "json.write: stream has no write protocol"}
			}
			count, ok := IntValueToBigInt(result)
			if !ok || !count.IsInt64() {
				return 0, runtime.ClassifiedError{Class: "TypeError", Message: "json.write: stream must return an integer write count"}
			}
			n := count.Int64()
			if n < 0 || n > int64(len(data)) {
				return 0, runtime.ClassifiedError{Class: "ValueError", Message: "json.write: invalid write count"}
			}
			return int(n), nil
		}
		if ctx.StreamWriter == nil {
			return 0, runtime.ClassifiedError{Class: "TypeError", Message: "json.write: stream is unavailable"}
		}
		return ctx.StreamWriter(stream, data)
	}}
	count, err := WriteJSONValueCtx(ctx, args[0], writer)
	if err != nil {
		return nil, err
	}
	return runtime.NewInt64(count), nil
}
