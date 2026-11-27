package native

import (
	"strings"
	"testing"
	"time"

	"geblang/internal/runtime"
)

func validityOpts(entries map[string]runtime.Value) runtime.Dict {
	d := runtime.NewDict()
	for k, v := range entries {
		key := runtime.String{Value: k}
		d.PutEntry(DictKey(key), runtime.DictEntry{Key: key, Value: v})
	}
	return d
}

func TestCertValidityBackdatesByClockSkew(t *testing.T) {
	before := time.Now()
	notBefore, notAfter, err := certValidity(validityOpts(nil), "fn")
	after := time.Now()
	if err != nil {
		t.Fatal(err)
	}
	if notBefore.Before(before.Add(-certClockSkew)) || notBefore.After(after.Add(-certClockSkew)) {
		t.Fatalf("notBefore %v is not %v before issuance (%v..%v)", notBefore, certClockSkew, before, after)
	}
	year := 365 * 24 * time.Hour
	if notAfter.Before(before.Add(year)) || notAfter.After(after.Add(year)) {
		t.Fatalf("notAfter %v is not 365 days after issuance (%v..%v)", notAfter, before, after)
	}
}

func TestCertValidityExplicitNotBefore(t *testing.T) {
	notBefore, _, err := certValidity(validityOpts(map[string]runtime.Value{"notBefore": runtime.String{Value: "2026-01-02T03:04:05Z"}}), "fn")
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC); !notBefore.Equal(want) {
		t.Fatalf("notBefore = %v, want %v", notBefore, want)
	}
}

func TestCertValidityRejectsBadNotBefore(t *testing.T) {
	cases := map[string]string{
		"yesterday":            "must be an RFC 3339 time",
		"2999-01-01T00:00:00Z": "must be before the end of the validity period",
	}
	for raw, want := range cases {
		_, _, err := certValidity(validityOpts(map[string]runtime.Value{"notBefore": runtime.String{Value: raw}}), "fn")
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("notBefore %q: err = %v, want %q", raw, err, want)
		}
	}
}
