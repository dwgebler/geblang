package native

import (
	"math/big"
	"strings"
	"testing"

	"geblang/internal/runtime"
)

func TestJSONRejectsInvalidKeyAfterUnsortedKeys(t *testing.T) {
	value := runtime.NewDict()
	for _, key := range []runtime.Value{runtime.String{Value: "z"}, runtime.String{Value: "a"}, runtime.NewInt64(3)} {
		value.PutEntry(DictKey(key), runtime.DictEntry{Key: key, Value: runtime.Null{}})
	}
	_, err := EncodeJSONValue(value)
	if err == nil || !strings.Contains(err.Error(), "string keys") {
		t.Fatalf("expected string-key error, got %v", err)
	}
}

func TestJSONRejectsCyclesAndAllowsSiblingAliases(t *testing.T) {
	list := &runtime.List{}
	list.Elements = []runtime.Value{list}
	dict := runtime.NewDict()
	dict.PutEntry("sself", runtime.DictEntry{Key: runtime.String{Value: "self"}, Value: dict})
	instance := &runtime.Instance{Fields: map[string]runtime.Value{}}
	instance.Fields["self"] = instance
	for _, value := range []runtime.Value{list, dict, instance} {
		if _, err := EncodeJSONValue(value); err == nil || !strings.Contains(err.Error(), "cyclic") {
			t.Fatalf("encode %T: %v", value, err)
		}
		if _, err := ValueToJSON(value); err == nil || !strings.Contains(err.Error(), "cyclic") {
			t.Fatalf("convert %T: %v", value, err)
		}
	}
	child := &runtime.List{Elements: []runtime.Value{runtime.NewInt64(1)}}
	got, err := EncodeJSONValue(&runtime.List{Elements: []runtime.Value{child, child}})
	if err != nil || got != "[[1],[1]]" {
		t.Fatalf("sibling aliases: %q, %v", got, err)
	}
}

func TestJSONRejectsExcessiveNesting(t *testing.T) {
	source := strings.Repeat("[", 600) + "0" + strings.Repeat("]", 600)
	if _, err := ParseJSONText(source); err == nil || !strings.Contains(err.Message, "depth") {
		t.Fatalf("parse: %v", err)
	}
	var value runtime.Value = runtime.NewInt64(0)
	for range 600 {
		value = &runtime.List{Elements: []runtime.Value{value}}
	}
	if _, err := EncodeJSONValue(value); err == nil || !strings.Contains(err.Error(), "depth") {
		t.Fatalf("encode: %v", err)
	}
}

func TestJSONEscapedStringsAndDecimalCompatibility(t *testing.T) {
	source := `["hello\nworld","\uD83D\uDE00","a\\b\"c"]`
	value, err := ParseJSONText(source)
	if err != nil {
		t.Fatal(err)
	}
	got, encodeErr := EncodeJSONValue(value)
	if encodeErr != nil || got != "[\"hello\\nworld\",\"\U0001F600\",\"a\\\\b\\\"c\"]" {
		t.Fatalf("got %q, %v", got, encodeErr)
	}
	decimal, _ := new(big.Rat).SetString("0.1234567890123456789")
	got, encodeErr = EncodeJSONValue(runtime.Decimal{Value: decimal})
	if encodeErr != nil || got != "0.1234567890" {
		t.Fatalf("decimal compatibility: %q, %v", got, encodeErr)
	}
}

func BenchmarkJSONEscapedRecords(b *testing.B) {
	source := "[" + strings.Repeat(`{"name":"a\nb"},`, 3999) + `{"name":"a\nb"}]`
	b.ReportAllocs()
	b.SetBytes(int64(len(source)))
	for b.Loop() {
		if _, err := ParseJSONText(source); err != nil {
			b.Fatal(err)
		}
	}
}

func TestJSONUnpairedSurrogateDoesNotConsumeFollowingEscape(t *testing.T) {
	for source, want := range map[string]string{`"\uD800\u0041"`: "\ufffdA", `"\uDC00\u0041"`: "\ufffdA", `"\uD800\uD800\uDC00"`: "\ufffd\U00010000"} {
		got, err := ParseJSONText(source)
		if err != nil || got.(runtime.String).Value != want {
			t.Fatalf("%s: %v, %v; want %q", source, got, err, want)
		}
	}
}

func TestJSONDepthBoundaryAgreesAcrossValidationAndEncoding(t *testing.T) {
	registry := NewBuiltinRegistry()
	for _, depth := range []int{511, 512, 513} {
		source := strings.Repeat("[", depth) + "0" + strings.Repeat("]", depth)
		parsed, err := ParseJSONText(source)
		valid, callErr := registry.Call("json", "validate", []runtime.Value{runtime.String{Value: source}})
		if callErr != nil {
			t.Fatal(callErr)
		}
		want := depth <= 512
		if (err == nil) != want || valid.(runtime.Bool).Value != want {
			t.Fatalf("depth %d: parse=%v validate=%v", depth, err, valid)
		}
		if want {
			if _, encodeErr := EncodeJSONValue(parsed); encodeErr != nil {
				t.Fatal(encodeErr)
			}
		}
	}
}
