package native

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"math/big"

	"geblang/internal/runtime"
)

func registerCryptJWK(r *Registry) {
	r.Register("crypt", "jwk", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) < 1 || len(args) > 2 {
			return nil, fmt.Errorf("crypt.jwk expects a PEM string and optional opts")
		}
		pemStr, ok := args[0].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("crypt.jwk key must be a PEM string")
		}
		opts := runtime.Dict{}
		if len(args) == 2 {
			d, ok := args[1].(runtime.Dict)
			if !ok {
				return nil, fmt.Errorf("crypt.jwk opts must be a dict")
			}
			opts = d
		}
		return jwkFromPEM(pemStr.Value, opts, "crypt.jwk")
	})
	r.Register("crypt", "jwks", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("crypt.jwks expects a list of keys")
		}
		list, ok := args[0].(*runtime.List)
		if !ok {
			return nil, fmt.Errorf("crypt.jwks keys must be a list")
		}
		keys := make([]runtime.Value, 0, len(list.Elements))
		for i, el := range list.Elements {
			switch entry := el.(type) {
			case runtime.String:
				jwk, err := jwkFromPEM(entry.Value, runtime.Dict{}, "crypt.jwks")
				if err != nil {
					return nil, fmt.Errorf("crypt.jwks key %d: %w", i, err)
				}
				keys = append(keys, jwk)
			case runtime.Dict:
				if dictString(entry, "kty") != "" {
					keys = append(keys, entry)
					continue
				}
				pemStr := dictString(entry, "pem")
				if pemStr == "" {
					return nil, fmt.Errorf("crypt.jwks key %d: dict entries need kty (a JWK) or pem", i)
				}
				jwk, err := jwkFromPEM(pemStr, entry, "crypt.jwks")
				if err != nil {
					return nil, fmt.Errorf("crypt.jwks key %d: %w", i, err)
				}
				keys = append(keys, jwk)
			default:
				return nil, fmt.Errorf("crypt.jwks key %d must be a PEM string or dict", i)
			}
		}
		keysKey := runtime.String{Value: "keys"}
		return runtime.DictFromEntries(map[string]runtime.DictEntry{
			DictKey(keysKey): {Key: keysKey, Value: &runtime.List{Elements: keys}},
		}, nil), nil
	})
}

// jwkFromPEM builds the RFC 7517 public JWK for a public, certificate,
// or private PEM (private keys contribute their public half). opts may
// set kid (default: the RFC 7638 thumbprint), alg (default per key
// type), and use (default "sig").
func jwkFromPEM(pemStr string, opts runtime.Dict, label string) (runtime.Dict, error) {
	pub, err := parsePublicKeyPEM(pemStr, label)
	if err != nil {
		priv, privErr := parsePrivKeyPEM(pemStr, label)
		if privErr != nil {
			return runtime.Dict{}, err
		}
		switch k := priv.(type) {
		case *rsa.PrivateKey:
			pub = k.Public()
		case *ecdsa.PrivateKey:
			pub = k.Public()
		case ed25519.PrivateKey:
			pub = k.Public()
		default:
			return runtime.Dict{}, fmt.Errorf("%s unsupported key type", label)
		}
	}
	b64 := base64.RawURLEncoding.EncodeToString
	var members []runtime.DictEntry
	var thumbprintJSON string
	defaultAlg := ""
	add := func(name, value string) {
		key := runtime.String{Value: name}
		members = append(members, runtime.DictEntry{Key: key, Value: runtime.String{Value: value}})
	}
	switch k := pub.(type) {
	case *rsa.PublicKey:
		n := b64(k.N.Bytes())
		e := b64(big.NewInt(int64(k.E)).Bytes())
		add("kty", "RSA")
		add("n", n)
		add("e", e)
		thumbprintJSON = `{"e":"` + e + `","kty":"RSA","n":"` + n + `"}`
		defaultAlg = "RS256"
	case *ecdsa.PublicKey:
		crv, alg, err := jwkCurveName(k.Curve)
		if err != nil {
			return runtime.Dict{}, fmt.Errorf("%s %w", label, err)
		}
		byteLen := (k.Curve.Params().BitSize + 7) / 8
		x := b64(k.X.FillBytes(make([]byte, byteLen)))
		y := b64(k.Y.FillBytes(make([]byte, byteLen)))
		add("kty", "EC")
		add("crv", crv)
		add("x", x)
		add("y", y)
		thumbprintJSON = `{"crv":"` + crv + `","kty":"EC","x":"` + x + `","y":"` + y + `"}`
		defaultAlg = alg
	case ed25519.PublicKey:
		x := b64(k)
		add("kty", "OKP")
		add("crv", "Ed25519")
		add("x", x)
		thumbprintJSON = `{"crv":"Ed25519","kty":"OKP","x":"` + x + `"}`
		defaultAlg = "EdDSA"
	default:
		return runtime.Dict{}, fmt.Errorf("%s unsupported public key type", label)
	}
	kid := dictString(opts, "kid")
	if kid == "" {
		sum := sha256.Sum256([]byte(thumbprintJSON))
		kid = base64.RawURLEncoding.EncodeToString(sum[:])
	}
	alg := dictString(opts, "alg")
	if alg == "" {
		alg = defaultAlg
	}
	use := dictString(opts, "use")
	if use == "" {
		use = "sig"
	}
	add("kid", kid)
	add("alg", alg)
	add("use", use)
	entries := make(map[string]runtime.DictEntry, len(members))
	for _, entry := range members {
		entries[DictKey(entry.Key)] = entry
	}
	return runtime.DictFromEntries(entries, nil), nil
}

func jwkCurveName(curve elliptic.Curve) (string, string, error) {
	switch curve {
	case elliptic.P256():
		return "P-256", "ES256", nil
	case elliptic.P384():
		return "P-384", "ES384", nil
	case elliptic.P521():
		return "P-521", "ES512", nil
	}
	return "", "", fmt.Errorf("unsupported EC curve")
}

// jwkVerifyKey is a verification key resolved from a JWK or JWKS dict.
type jwkVerifyKey struct {
	pub    interface{}
	secret []byte
	alg    string
}

// jwkResolve selects the verification key from a JWK or JWKS dict: by
// kid when the token header carries one, otherwise the sole key.
func jwkResolve(dict runtime.Dict, kid string) (*jwkVerifyKey, bool) {
	candidates := []runtime.Dict{}
	if keysValue, ok := dictLookup(dict, "keys"); ok {
		list, ok := keysValue.(*runtime.List)
		if !ok {
			return nil, false
		}
		for _, el := range list.Elements {
			if d, ok := el.(runtime.Dict); ok {
				candidates = append(candidates, d)
			}
		}
	} else {
		candidates = append(candidates, dict)
	}
	var match *runtime.Dict
	if kid != "" {
		for i := range candidates {
			if dictString(candidates[i], "kid") == kid {
				match = &candidates[i]
				break
			}
		}
	} else if len(candidates) == 1 {
		match = &candidates[0]
	}
	if match == nil {
		return nil, false
	}
	return jwkToVerifyKey(*match)
}

func jwkToVerifyKey(jwk runtime.Dict) (*jwkVerifyKey, bool) {
	decode := func(name string) ([]byte, bool) {
		raw := dictString(jwk, name)
		if raw == "" {
			return nil, false
		}
		decoded, err := base64.RawURLEncoding.DecodeString(raw)
		if err != nil {
			return nil, false
		}
		return decoded, true
	}
	key := &jwkVerifyKey{alg: dictString(jwk, "alg")}
	switch dictString(jwk, "kty") {
	case "RSA":
		n, okN := decode("n")
		e, okE := decode("e")
		if !okN || !okE {
			return nil, false
		}
		key.pub = &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(new(big.Int).SetBytes(e).Int64())}
	case "EC":
		x, okX := decode("x")
		y, okY := decode("y")
		if !okX || !okY {
			return nil, false
		}
		var curve elliptic.Curve
		switch dictString(jwk, "crv") {
		case "P-256":
			curve = elliptic.P256()
		case "P-384":
			curve = elliptic.P384()
		case "P-521":
			curve = elliptic.P521()
		default:
			return nil, false
		}
		key.pub = &ecdsa.PublicKey{Curve: curve, X: new(big.Int).SetBytes(x), Y: new(big.Int).SetBytes(y)}
	case "OKP":
		if dictString(jwk, "crv") != "Ed25519" {
			return nil, false
		}
		x, ok := decode("x")
		if !ok || len(x) != ed25519.PublicKeySize {
			return nil, false
		}
		key.pub = ed25519.PublicKey(x)
	case "oct":
		k, ok := decode("k")
		if !ok {
			return nil, false
		}
		key.secret = k
	default:
		return nil, false
	}
	return key, true
}

// jwkAllowedAlgs pins verification to the JWK's declared alg, or to
// the family its key type supports.
func jwkAllowedAlgs(key *jwkVerifyKey) []string {
	if key.alg != "" {
		return []string{key.alg}
	}
	switch key.pub.(type) {
	case *rsa.PublicKey:
		return []string{"RS256", "RS384", "RS512"}
	case *ecdsa.PublicKey:
		return []string{"ES256", "ES384", "ES512"}
	case ed25519.PublicKey:
		return []string{"EdDSA"}
	}
	if key.secret != nil {
		return []string{"HS256", "HS384", "HS512"}
	}
	return []string{}
}
