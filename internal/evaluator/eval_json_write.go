package evaluator

import "geblang/internal/runtime"

func (e *Evaluator) writeJSONBytes(stream runtime.Value, data []byte) (int, error) {
	writer, err := e.ioWriter(stream)
	if err != nil {
		return 0, err
	}
	return writer.Write(data)
}
