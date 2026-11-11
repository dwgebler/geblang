package native

import (
	"encoding/json"
	"errors"

	"geblang/internal/runtime"
)

type ParseError struct {
	Message string
	Line    int64
	Column  int64
	Offset  int64
}

func JSONParseError(err error, text string) ParseError {
	offset := int64(-1)
	var syntaxErr *json.SyntaxError
	var typeErr *json.UnmarshalTypeError
	switch {
	case errors.As(err, &syntaxErr):
		offset = syntaxErr.Offset
	case errors.As(err, &typeErr):
		offset = typeErr.Offset
	}
	if offset < 0 {
		offset = int64(len(text) + 1)
	}
	return NewParseError(err.Error(), text, offset)
}

func NewParseError(message string, text string, offset int64) ParseError {
	line, column := lineColumn(text, offset)
	return ParseError{
		Message: message,
		Line:    line,
		Column:  column,
		Offset:  offset,
	}
}

func ParseErrorValue(parseErr ParseError) runtime.Value {
	return runtime.DictFromEntries(map[string]runtime.DictEntry{
		dictKey("message"): {Key: runtime.String{Value: "message"}, Value: runtime.String{Value: parseErr.Message}},
		dictKey("line"):    {Key: runtime.String{Value: "line"}, Value: runtime.NewInt64(parseErr.Line)},
		dictKey("column"):  {Key: runtime.String{Value: "column"}, Value: runtime.NewInt64(parseErr.Column)},
		dictKey("offset"):  {Key: runtime.String{Value: "offset"}, Value: runtime.NewInt64(parseErr.Offset)},
	}, nil)
}

func ParseResult(ok bool, value runtime.Value, parseErr *ParseError) runtime.Value {
	errorValue := runtime.Value(runtime.Null{})
	if parseErr != nil {
		errorValue = ParseErrorValue(*parseErr)
	}
	return runtime.DictFromEntries(map[string]runtime.DictEntry{
		dictKey("ok"):    {Key: runtime.String{Value: "ok"}, Value: runtime.Bool{Value: ok}},
		dictKey("value"): {Key: runtime.String{Value: "value"}, Value: value},
		dictKey("error"): {Key: runtime.String{Value: "error"}, Value: errorValue},
	}, nil)
}

func ValidationResult(valid bool, parseErr *ParseError) runtime.Value {
	errorValue := runtime.Value(runtime.Null{})
	if parseErr != nil {
		errorValue = ParseErrorValue(*parseErr)
	}
	return runtime.DictFromEntries(map[string]runtime.DictEntry{
		dictKey("valid"): {Key: runtime.String{Value: "valid"}, Value: runtime.Bool{Value: valid}},
		dictKey("error"): {Key: runtime.String{Value: "error"}, Value: errorValue},
	}, nil)
}

func lineColumn(text string, offset int64) (int64, int64) {
	if offset <= 0 {
		return 0, 0
	}
	runes := []rune(text)
	target := int(offset)
	if target > len(text) {
		target = len(text)
	}
	line := int64(1)
	column := int64(1)
	bytesSeen := 0
	for _, r := range runes {
		if bytesSeen >= target-1 {
			return line, column
		}
		bytesSeen += len(string(r))
		if r == '\n' {
			line++
			column = 1
		} else {
			column++
		}
	}
	return line, column
}

func dictKey(key string) string {
	return "s" + key
}
