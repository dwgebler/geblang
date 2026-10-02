package native

import (
	"math/big"
	"strings"
	"sync"
	"testing"

	"geblang/internal/runtime"
)

func TestLocaleFormatsExactDecimalAndCurrencyPrecision(t *testing.T) {
	registry := NewBuiltinRegistry()
	exact, ok := new(big.Rat).SetString("12345678901234567890.123456")
	if !ok {
		t.Fatal("invalid fixture")
	}
	options := runtime.NewDictHint(2)
	options.PutEntry(DictKey(runtime.String{Value: "minFractionDigits"}), runtime.DictEntry{
		Key: runtime.String{Value: "minFractionDigits"}, Value: runtime.NewInt64(6),
	})
	options.PutEntry(DictKey(runtime.String{Value: "maxFractionDigits"}), runtime.DictEntry{
		Key: runtime.String{Value: "maxFractionDigits"}, Value: runtime.NewInt64(6),
	})

	number, err := registry.Call("locale", "formatNumber", []runtime.Value{
		runtime.Decimal{Value: exact}, runtime.String{Value: "en"}, options,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := number.(runtime.String).Value; got != "12,345,678,901,234,567,890.123456" {
		t.Fatalf("exact decimal changed: %q", got)
	}
	currency, err := registry.Call("locale", "formatCurrency", []runtime.Value{
		runtime.NewInt64(1235), runtime.String{Value: "JPY"}, runtime.String{Value: "en"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := currency.(runtime.String).Value; strings.Contains(got, ".") || !strings.Contains(got, "1,235") {
		t.Fatalf("JPY should have no fractional digits: %q", got)
	}
}

func TestLocaleConcurrentCallsKeepTagsIndependent(t *testing.T) {
	registry := NewBuiltinRegistry()
	var workers sync.WaitGroup
	errors := make(chan string, 2)
	for _, tc := range []struct {
		tag  string
		want string
	}{{"en", "12,345"}, {"de", "12.345"}} {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for range 100 {
				result, err := registry.Call("locale", "formatNumber", []runtime.Value{
					runtime.NewInt64(12345), runtime.String{Value: tc.tag},
				})
				if err != nil {
					errors <- err.Error()
					return
				}
				if got := result.(runtime.String).Value; got != tc.want {
					errors <- got
					return
				}
			}
		}()
	}
	workers.Wait()
	close(errors)
	for err := range errors {
		t.Fatalf("locale result changed across concurrent calls: %s", err)
	}
}
