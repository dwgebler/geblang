package native

import (
	"encoding/base32"
	"encoding/base64"
	"fmt"
	"geblang/internal/runtime"
	"html"
	"math/big"
	"net/url"
	"strings"
	"sync"

	"github.com/microcosm-cc/bluemonday"
)

// htmlSanitizePolicy is built on first use: constructing it costs about a millisecond of every startup.
var htmlSanitizePolicy = sync.OnceValue(bluemonday.UGCPolicy)

func registerEncoding(r *Registry) {
	r.Register("encoding", "base64Encode", func(args []runtime.Value) (runtime.Value, error) {
		data, err := encodingInputBytes(args, "encoding.base64Encode")
		if err != nil {
			return nil, err
		}
		return runtime.String{Value: base64.StdEncoding.EncodeToString(data)}, nil
	})
	r.Register("encoding", "base64Decode", func(args []runtime.Value) (runtime.Value, error) {
		s, err := singleString(args, "encoding.base64Decode")
		if err != nil {
			return nil, err
		}
		decoded, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			return nil, fmt.Errorf("encoding.base64Decode: %v", err)
		}
		return runtime.String{Value: string(decoded)}, nil
	})
	r.Register("encoding", "urlEncode", func(args []runtime.Value) (runtime.Value, error) {
		s, err := singleString(args, "encoding.urlEncode")
		if err != nil {
			return nil, err
		}
		return runtime.String{Value: url.QueryEscape(s)}, nil
	})
	r.Register("encoding", "urlDecode", func(args []runtime.Value) (runtime.Value, error) {
		s, err := singleString(args, "encoding.urlDecode")
		if err != nil {
			return nil, err
		}
		decoded, err := url.QueryUnescape(s)
		if err != nil {
			return nil, fmt.Errorf("encoding.urlDecode: %v", err)
		}
		return runtime.String{Value: decoded}, nil
	})
	r.Register("encoding", "htmlEscape", func(args []runtime.Value) (runtime.Value, error) {
		s, err := singleString(args, "encoding.htmlEscape")
		if err != nil {
			return nil, err
		}
		return runtime.String{Value: html.EscapeString(s)}, nil
	})
	r.Register("encoding", "htmlUnescape", func(args []runtime.Value) (runtime.Value, error) {
		s, err := singleString(args, "encoding.htmlUnescape")
		if err != nil {
			return nil, err
		}
		return runtime.String{Value: html.UnescapeString(s)}, nil
	})
	r.Register("encoding", "sanitizeHtml", func(args []runtime.Value) (runtime.Value, error) {
		s, err := singleString(args, "encoding.sanitizeHtml")
		if err != nil {
			return nil, err
		}
		return runtime.String{Value: htmlSanitizePolicy().Sanitize(s)}, nil
	})
	r.Register("encoding", "base32Encode", base32EncodeFn)
	r.Register("encoding", "base32Decode", base32DecodeFn)
	r.Register("encoding", "base58Encode", base58EncodeFn)
	r.Register("encoding", "base58Decode", base58DecodeFn)
	r.Register("encoding", "base64UrlEncode", func(args []runtime.Value) (runtime.Value, error) {
		data, err := encodingInputBytes(args, "encoding.base64UrlEncode")
		if err != nil {
			return nil, err
		}
		return runtime.String{Value: base64.RawURLEncoding.EncodeToString(data)}, nil
	})
	r.Register("encoding", "base64UrlDecode", func(args []runtime.Value) (runtime.Value, error) {
		s, err := singleString(args, "encoding.base64UrlDecode")
		if err != nil {
			return nil, err
		}
		decoded, err := decodeBase64Url(s)
		if err != nil {
			return nil, fmt.Errorf("encoding.base64UrlDecode: %v", err)
		}
		return runtime.String{Value: string(decoded)}, nil
	})
}

// decodeBase64Url accepts both unpadded (RawURLEncoding, JOSE) and padded
// (URLEncoding) input so callers don't have to know which producer emitted
// the string.
func decodeBase64Url(s string) ([]byte, error) {
	trimmed := strings.TrimRight(s, "=")
	if decoded, err := base64.RawURLEncoding.DecodeString(trimmed); err == nil {
		return decoded, nil
	}
	return base64.URLEncoding.DecodeString(s)
}

// base58Alphabet is the Bitcoin / IPFS alphabet (no 0, O, I, l).
const base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

func base32EncodeFn(args []runtime.Value) (runtime.Value, error) {
	data, err := encodingInputBytes(args, "encoding.base32Encode")
	if err != nil {
		return nil, err
	}
	return runtime.String{Value: base32.StdEncoding.EncodeToString(data)}, nil
}

func base32DecodeFn(args []runtime.Value) (runtime.Value, error) {
	s, err := singleString(args, "encoding.base32Decode")
	if err != nil {
		return nil, err
	}
	// Accept both padded and unpadded inputs.
	decoded, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.TrimRight(s, "="))
	if err != nil {
		decoded, err = base32.StdEncoding.DecodeString(s)
		if err != nil {
			return nil, fmt.Errorf("encoding.base32Decode: %v", err)
		}
	}
	return runtime.Bytes{Value: decoded}, nil
}

func base58EncodeFn(args []runtime.Value) (runtime.Value, error) {
	data, err := encodingInputBytes(args, "encoding.base58Encode")
	if err != nil {
		return nil, err
	}
	return runtime.String{Value: base58Encode(data)}, nil
}

func base58DecodeFn(args []runtime.Value) (runtime.Value, error) {
	s, err := singleString(args, "encoding.base58Decode")
	if err != nil {
		return nil, err
	}
	decoded, err := base58Decode(s)
	if err != nil {
		return nil, fmt.Errorf("encoding.base58Decode: %v", err)
	}
	return runtime.Bytes{Value: decoded}, nil
}

// encodingInputBytes accepts either a string or bytes value as input so the
// new base32/base58 functions can be fed crypto.randomBytes output directly.
func encodingInputBytes(args []runtime.Value, name string) ([]byte, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects one argument", name)
	}
	switch v := args[0].(type) {
	case runtime.String:
		return []byte(v.Value), nil
	case runtime.Bytes:
		return v.Value, nil
	default:
		return nil, fmt.Errorf("%s expects string or bytes, got %s", name, v.TypeName())
	}
}

// base58Encode encodes bytes using the Bitcoin alphabet.
func base58Encode(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	leading := 0
	for _, b := range data {
		if b != 0 {
			break
		}
		leading++
	}
	x := new(big.Int).SetBytes(data)
	base := big.NewInt(58)
	mod := new(big.Int)
	var encoded []byte
	for x.Sign() > 0 {
		x.DivMod(x, base, mod)
		encoded = append(encoded, base58Alphabet[mod.Int64()])
	}
	for i := 0; i < leading; i++ {
		encoded = append(encoded, base58Alphabet[0])
	}
	// reverse
	for i, j := 0, len(encoded)-1; i < j; i, j = i+1, j-1 {
		encoded[i], encoded[j] = encoded[j], encoded[i]
	}
	return string(encoded)
}

// base58Decode decodes a base58 string. Returns an error on invalid characters.
func base58Decode(s string) ([]byte, error) {
	x := new(big.Int)
	base := big.NewInt(58)
	leading := 0
	for _, c := range s {
		if c == rune(base58Alphabet[0]) {
			leading++
			continue
		}
		break
	}
	for _, c := range s {
		idx := strings.IndexRune(base58Alphabet, c)
		if idx < 0 {
			return nil, fmt.Errorf("invalid base58 character %q", c)
		}
		x.Mul(x, base)
		x.Add(x, big.NewInt(int64(idx)))
	}
	decoded := x.Bytes()
	out := make([]byte, leading+len(decoded))
	copy(out[leading:], decoded)
	return out, nil
}
