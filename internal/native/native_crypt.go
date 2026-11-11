package native

import (
	aescipher "crypto/aes"
	ciphermode "crypto/cipher"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"geblang/internal/runtime"
	"hash/crc32"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/blake2b"
	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/sha3"
)

func registerCrypt(r *Registry) {
	r.Register("crypt", "md5", func(args []runtime.Value) (runtime.Value, error) {
		data, err := singleHashInput(args, "crypt.md5")
		if err != nil {
			return nil, err
		}
		sum := md5.Sum(data)
		return runtime.String{Value: hex.EncodeToString(sum[:])}, nil
	})
	r.Register("crypt", "sha1", func(args []runtime.Value) (runtime.Value, error) {
		data, err := singleHashInput(args, "crypt.sha1")
		if err != nil {
			return nil, err
		}
		sum := sha1.Sum(data)
		return runtime.String{Value: hex.EncodeToString(sum[:])}, nil
	})
	r.Register("crypt", "sha256", func(args []runtime.Value) (runtime.Value, error) {
		data, err := singleHashInput(args, "crypt.sha256")
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(data)
		return runtime.String{Value: hex.EncodeToString(sum[:])}, nil
	})
	r.Register("crypt", "sha512", func(args []runtime.Value) (runtime.Value, error) {
		data, err := singleHashInput(args, "crypt.sha512")
		if err != nil {
			return nil, err
		}
		sum := sha512.Sum512(data)
		return runtime.String{Value: hex.EncodeToString(sum[:])}, nil
	})
	r.Register("crypt", "sha3_256", func(args []runtime.Value) (runtime.Value, error) {
		data, err := singleHashInput(args, "crypt.sha3_256")
		if err != nil {
			return nil, err
		}
		sum := sha3.Sum256(data)
		return runtime.String{Value: hex.EncodeToString(sum[:])}, nil
	})
	r.Register("crypt", "blake2b", func(args []runtime.Value) (runtime.Value, error) {
		data, err := singleHashInput(args, "crypt.blake2b")
		if err != nil {
			return nil, err
		}
		sum := blake2b.Sum256(data)
		return runtime.String{Value: hex.EncodeToString(sum[:])}, nil
	})
	r.Register("crypt", "crc32", func(args []runtime.Value) (runtime.Value, error) {
		data, err := singleHashInput(args, "crypt.crc32")
		if err != nil {
			return nil, err
		}
		return runtime.NewInt64(int64(crc32.ChecksumIEEE(data))), nil
	})
	r.Register("crypt", "hmacSha256", func(args []runtime.Value) (runtime.Value, error) {
		key, msg, err := hmacInputs(args, "crypt.hmacSha256")
		if err != nil {
			return nil, err
		}
		mac := hmac.New(sha256.New, key)
		_, _ = mac.Write(msg)
		return runtime.String{Value: hex.EncodeToString(mac.Sum(nil))}, nil
	})
	r.Register("crypt", "hmacSha256Bytes", func(args []runtime.Value) (runtime.Value, error) {
		key, msg, err := hmacInputs(args, "crypt.hmacSha256Bytes")
		if err != nil {
			return nil, err
		}
		mac := hmac.New(sha256.New, key)
		_, _ = mac.Write(msg)
		return runtime.Bytes{Value: mac.Sum(nil)}, nil
	})
	r.Register("crypt", "bcryptHash", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 && len(args) != 2 {
			return nil, fmt.Errorf("crypt.bcryptHash expects password and optional cost")
		}
		password, ok := args[0].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("crypt.bcryptHash password must be string")
		}
		cost := bcrypt.DefaultCost
		if len(args) == 2 {
			costVal, ok := AsInt64(args[1])
			if !ok {
				return nil, fmt.Errorf("crypt.bcryptHash cost must be int")
			}
			cost = int(costVal)
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(password.Value), cost)
		if err != nil {
			return nil, err
		}
		return runtime.String{Value: string(hash)}, nil
	})
	r.Register("crypt", "bcryptVerify", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("crypt.bcryptVerify expects password and hash")
		}
		password, ok := args[0].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("crypt.bcryptVerify password must be string")
		}
		hash, ok := args[1].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("crypt.bcryptVerify hash must be string")
		}
		err := bcrypt.CompareHashAndPassword([]byte(hash.Value), []byte(password.Value))
		return runtime.Bool{Value: err == nil}, nil
	})
	r.Register("crypt", "argon2idHash", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 && len(args) != 2 {
			return nil, fmt.Errorf("crypt.argon2idHash expects password and optional options")
		}
		password, ok := args[0].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("crypt.argon2idHash password must be string")
		}
		params := defaultArgon2idParams()
		if len(args) == 2 {
			options, ok := args[1].(runtime.Dict)
			if !ok {
				return nil, fmt.Errorf("crypt.argon2idHash options must be dict")
			}
			if err := applyArgon2idOptions(&params, options); err != nil {
				return nil, err
			}
		}
		salt := make([]byte, params.saltLength)
		if _, err := rand.Read(salt); err != nil {
			return nil, err
		}
		hash := argon2.IDKey([]byte(password.Value), salt, params.time, params.memory, params.parallelism, params.keyLength)
		encoded := fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
			params.memory,
			params.time,
			params.parallelism,
			base64.RawStdEncoding.EncodeToString(salt),
			base64.RawStdEncoding.EncodeToString(hash),
		)
		return runtime.String{Value: encoded}, nil
	})
	r.Register("crypt", "argon2idVerify", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("crypt.argon2idVerify expects password and hash")
		}
		password, ok := args[0].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("crypt.argon2idVerify password must be string")
		}
		encoded, ok := args[1].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("crypt.argon2idVerify hash must be string")
		}
		params, salt, expected, err := parseArgon2idHash(encoded.Value)
		if err != nil {
			return runtime.Bool{Value: false}, nil
		}
		actual := argon2.IDKey([]byte(password.Value), salt, params.time, params.memory, params.parallelism, uint32(len(expected)))
		return runtime.Bool{Value: subtle.ConstantTimeCompare(actual, expected) == 1}, nil
	})
	r.Register("crypt", "passwordHash", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 && len(args) != 2 {
			return nil, fmt.Errorf("crypt.passwordHash expects password and optional opts")
		}
		password, ok := args[0].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("crypt.passwordHash password must be string")
		}
		algorithm := "bcrypt"
		var opts runtime.Dict
		var haveOpts bool
		if len(args) == 2 {
			opts, ok = args[1].(runtime.Dict)
			if !ok {
				return nil, fmt.Errorf("crypt.passwordHash opts must be dict")
			}
			haveOpts = true
			if alg := dictString(opts, "algorithm"); alg != "" {
				algorithm = alg
			}
		}
		switch algorithm {
		case "bcrypt", "2y", "PASSWORD_BCRYPT":
			cost := bcrypt.DefaultCost
			if haveOpts {
				if v, ok := dictInt64(opts, "cost"); ok {
					cost = int(v)
				}
			}
			hash, err := bcrypt.GenerateFromPassword([]byte(password.Value), cost)
			if err != nil {
				return nil, err
			}
			out := string(hash)
			if strings.HasPrefix(out, "$2a$") || strings.HasPrefix(out, "$2b$") {
				out = "$2y$" + out[4:]
			}
			return runtime.String{Value: out}, nil
		case "argon2id", "PASSWORD_ARGON2ID":
			params := defaultArgon2idParams()
			if haveOpts {
				if err := applyArgon2idOptions(&params, opts); err != nil {
					return nil, err
				}
			}
			salt := make([]byte, params.saltLength)
			if _, err := rand.Read(salt); err != nil {
				return nil, err
			}
			hash := argon2.IDKey([]byte(password.Value), salt, params.time, params.memory, params.parallelism, params.keyLength)
			return runtime.String{Value: fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
				params.memory, params.time, params.parallelism,
				base64.RawStdEncoding.EncodeToString(salt),
				base64.RawStdEncoding.EncodeToString(hash))}, nil
		case "argon2i", "PASSWORD_ARGON2I":
			params := defaultArgon2idParams()
			if haveOpts {
				if err := applyArgon2idOptions(&params, opts); err != nil {
					return nil, err
				}
			}
			salt := make([]byte, params.saltLength)
			if _, err := rand.Read(salt); err != nil {
				return nil, err
			}
			hash := argon2.Key([]byte(password.Value), salt, params.time, params.memory, params.parallelism, params.keyLength)
			return runtime.String{Value: fmt.Sprintf("$argon2i$v=19$m=%d,t=%d,p=%d$%s$%s",
				params.memory, params.time, params.parallelism,
				base64.RawStdEncoding.EncodeToString(salt),
				base64.RawStdEncoding.EncodeToString(hash))}, nil
		}
		return nil, fmt.Errorf("crypt.passwordHash: unknown algorithm %q (expected bcrypt, argon2id, argon2i)", algorithm)
	})
	r.Register("crypt", "passwordVerify", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("crypt.passwordVerify expects password and hash")
		}
		password, ok := args[0].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("crypt.passwordVerify password must be string")
		}
		encoded, ok := args[1].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("crypt.passwordVerify hash must be string")
		}
		hash := encoded.Value
		switch {
		case strings.HasPrefix(hash, "$2a$"), strings.HasPrefix(hash, "$2b$"), strings.HasPrefix(hash, "$2y$"):
			err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password.Value))
			return runtime.Bool{Value: err == nil}, nil
		case strings.HasPrefix(hash, "$argon2"):
			params, salt, expected, variant, err := parseArgon2Hash(hash)
			if err != nil {
				return runtime.Bool{Value: false}, nil
			}
			var actual []byte
			switch variant {
			case "argon2id":
				actual = argon2.IDKey([]byte(password.Value), salt, params.time, params.memory, params.parallelism, uint32(len(expected)))
			case "argon2i":
				actual = argon2.Key([]byte(password.Value), salt, params.time, params.memory, params.parallelism, uint32(len(expected)))
			default:
				return runtime.Bool{Value: false}, nil
			}
			return runtime.Bool{Value: subtle.ConstantTimeCompare(actual, expected) == 1}, nil
		}
		return runtime.Bool{Value: false}, nil
	})
	r.Register("crypt", "randomHex", func(args []runtime.Value) (runtime.Value, error) {
		size, err := singleInt64(args, "crypt.randomHex")
		if err != nil {
			return nil, err
		}
		if size < 0 || size > 1<<20 {
			return nil, fmt.Errorf("crypt.randomHex byte count out of range")
		}
		data := make([]byte, size)
		if _, err := rand.Read(data); err != nil {
			return nil, err
		}
		return runtime.String{Value: hex.EncodeToString(data)}, nil
	})
	r.Register("crypt", "base64Encode", func(args []runtime.Value) (runtime.Value, error) {
		text, err := singleString(args, "crypt.base64Encode")
		if err != nil {
			return nil, err
		}
		return runtime.String{Value: base64.StdEncoding.EncodeToString([]byte(text))}, nil
	})
	r.Register("crypt", "base64Decode", func(args []runtime.Value) (runtime.Value, error) {
		text, err := singleString(args, "crypt.base64Decode")
		if err != nil {
			return nil, err
		}
		decoded, err := base64.StdEncoding.DecodeString(text)
		if err != nil {
			return nil, err
		}
		return runtime.String{Value: string(decoded)}, nil
	})
	r.Register("crypt", "jwtSign", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) < 2 || len(args) > 3 {
			return nil, fmt.Errorf("crypt.jwtSign expects payload, key, and optional opts")
		}
		alg := "HS256"
		kid := ""
		var allowed []string
		if len(args) == 3 {
			a, err := jwtOptsAlg(args[2], "crypt.jwtSign")
			if err != nil {
				return nil, err
			}
			if a != "" {
				alg = a
			}
			al, err := jwtOptsAllowedAlgs(args[2], "crypt.jwtSign")
			if err != nil {
				return nil, err
			}
			allowed = al
			if opts, ok := args[2].(runtime.Dict); ok {
				kid = dictString(opts, "kid")
			}
		}
		return jwtSignWithAlg(args[0], args[1], alg, allowed, kid, "crypt.jwtSign")
	})
	r.Register("crypt", "jwtVerify", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) < 2 || len(args) > 3 {
			return nil, fmt.Errorf("crypt.jwtVerify expects token, key, and optional opts")
		}
		token, ok := args[0].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("crypt.jwtVerify token must be string")
		}
		var allowed []string
		if len(args) == 3 {
			a, err := jwtOptsAllowedAlgs(args[2], "crypt.jwtVerify")
			if err != nil {
				return nil, err
			}
			allowed = a
		}
		return jwtVerifyWithAlg(token.Value, args[1], allowed, "crypt.jwtVerify")
	})
	r.Register("crypt", "jwtDecode", func(args []runtime.Value) (runtime.Value, error) {
		token, err := singleString(args, "crypt.jwtDecode")
		if err != nil {
			return nil, err
		}
		parts := strings.SplitN(token, ".", 3)
		if len(parts) != 3 {
			return nil, fmt.Errorf("crypt.jwtDecode invalid JWT format")
		}
		headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
		if err != nil {
			return nil, fmt.Errorf("crypt.jwtDecode invalid header encoding")
		}
		payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
		if err != nil {
			return nil, fmt.Errorf("crypt.jwtDecode invalid payload encoding")
		}
		headerVal, parseErr := ParseJSONText(string(headerBytes))
		if parseErr != nil {
			return nil, fmt.Errorf("crypt.jwtDecode invalid header JSON")
		}
		payloadVal, parseErr := ParseJSONText(string(payloadBytes))
		if parseErr != nil {
			return nil, fmt.Errorf("crypt.jwtDecode invalid payload JSON")
		}
		headerKey := runtime.String{Value: "header"}
		payloadKey := runtime.String{Value: "payload"}
		entries := map[string]runtime.DictEntry{
			DictKey(headerKey):  {Key: headerKey, Value: headerVal},
			DictKey(payloadKey): {Key: payloadKey, Value: payloadVal},
		}
		return runtime.DictFromEntries(entries, nil), nil
	})
	r.Register("crypt", "aesEncrypt", aesEncryptFn)
	r.Register("crypt", "aesDecrypt", aesDecryptFn)
	r.Register("crypt", "chacha20Encrypt", chacha20EncryptFn)
	r.Register("crypt", "chacha20Decrypt", chacha20DecryptFn)
}

// aeadBytesArg extracts a byte slice from a runtime String or Bytes value.
// AES/ChaCha20 callers can pass either, since cipher keys / nonces are often
// generated by other crypt functions that return strings.
func aeadBytesArg(v runtime.Value, name string) ([]byte, error) {
	switch x := v.(type) {
	case runtime.Bytes:
		return x.Value, nil
	case runtime.String:
		return []byte(x.Value), nil
	default:
		return nil, fmt.Errorf("%s must be bytes or string", name)
	}
}

// aeadResultDict packs an AEAD output into {"nonce": Bytes, "ciphertext": Bytes}.
func aeadResultDict(nonce, ciphertext []byte) runtime.Dict {
	nonceKey := runtime.String{Value: "nonce"}
	ctKey := runtime.String{Value: "ciphertext"}
	entries := map[string]runtime.DictEntry{
		DictKey(nonceKey): {Key: nonceKey, Value: runtime.Bytes{Value: nonce}},
		DictKey(ctKey):    {Key: ctKey, Value: runtime.Bytes{Value: ciphertext}},
	}
	return runtime.DictFromEntries(entries, nil)
}

// aeadOptionalAAD returns the AAD bytes from args[start] if present, else nil.
func aeadOptionalAAD(args []runtime.Value, start int, name string) ([]byte, error) {
	if len(args) <= start {
		return nil, nil
	}
	if _, ok := args[start].(runtime.Null); ok {
		return nil, nil
	}
	return aeadBytesArg(args[start], name+" associated data")
}

func aesEncryptFn(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 || len(args) > 3 {
		return nil, fmt.Errorf("crypt.aesEncrypt expects (key, plaintext, [associatedData])")
	}
	key, err := aeadBytesArg(args[0], "crypt.aesEncrypt key")
	if err != nil {
		return nil, err
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("crypt.aesEncrypt requires a 32-byte AES-256 key (got %d bytes)", len(key))
	}
	plaintext, err := aeadBytesArg(args[1], "crypt.aesEncrypt plaintext")
	if err != nil {
		return nil, err
	}
	aad, err := aeadOptionalAAD(args, 2, "crypt.aesEncrypt")
	if err != nil {
		return nil, err
	}
	block, err := aescipher.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("crypt.aesEncrypt: %w", err)
	}
	gcm, err := ciphermode.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("crypt.aesEncrypt: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("crypt.aesEncrypt nonce: %w", err)
	}
	ciphertext := gcm.Seal(nil, nonce, plaintext, aad)
	return aeadResultDict(nonce, ciphertext), nil
}

func aesDecryptFn(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 3 || len(args) > 4 {
		return nil, fmt.Errorf("crypt.aesDecrypt expects (key, nonce, ciphertext, [associatedData])")
	}
	key, err := aeadBytesArg(args[0], "crypt.aesDecrypt key")
	if err != nil {
		return nil, err
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("crypt.aesDecrypt requires a 32-byte AES-256 key (got %d bytes)", len(key))
	}
	nonce, err := aeadBytesArg(args[1], "crypt.aesDecrypt nonce")
	if err != nil {
		return nil, err
	}
	ciphertext, err := aeadBytesArg(args[2], "crypt.aesDecrypt ciphertext")
	if err != nil {
		return nil, err
	}
	aad, err := aeadOptionalAAD(args, 3, "crypt.aesDecrypt")
	if err != nil {
		return nil, err
	}
	block, err := aescipher.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("crypt.aesDecrypt: %w", err)
	}
	gcm, err := ciphermode.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("crypt.aesDecrypt: %w", err)
	}
	if len(nonce) != gcm.NonceSize() {
		return nil, fmt.Errorf("crypt.aesDecrypt nonce must be %d bytes", gcm.NonceSize())
	}
	plaintext, err := gcm.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return nil, fmt.Errorf("crypt.aesDecrypt: authentication failed")
	}
	return runtime.Bytes{Value: plaintext}, nil
}

func chacha20EncryptFn(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 || len(args) > 3 {
		return nil, fmt.Errorf("crypt.chacha20Encrypt expects (key, plaintext, [associatedData])")
	}
	key, err := aeadBytesArg(args[0], "crypt.chacha20Encrypt key")
	if err != nil {
		return nil, err
	}
	if len(key) != chacha20poly1305.KeySize {
		return nil, fmt.Errorf("crypt.chacha20Encrypt requires a %d-byte key (got %d bytes)", chacha20poly1305.KeySize, len(key))
	}
	plaintext, err := aeadBytesArg(args[1], "crypt.chacha20Encrypt plaintext")
	if err != nil {
		return nil, err
	}
	aad, err := aeadOptionalAAD(args, 2, "crypt.chacha20Encrypt")
	if err != nil {
		return nil, err
	}
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, fmt.Errorf("crypt.chacha20Encrypt: %w", err)
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("crypt.chacha20Encrypt nonce: %w", err)
	}
	ciphertext := aead.Seal(nil, nonce, plaintext, aad)
	return aeadResultDict(nonce, ciphertext), nil
}

func chacha20DecryptFn(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 3 || len(args) > 4 {
		return nil, fmt.Errorf("crypt.chacha20Decrypt expects (key, nonce, ciphertext, [associatedData])")
	}
	key, err := aeadBytesArg(args[0], "crypt.chacha20Decrypt key")
	if err != nil {
		return nil, err
	}
	if len(key) != chacha20poly1305.KeySize {
		return nil, fmt.Errorf("crypt.chacha20Decrypt requires a %d-byte key (got %d bytes)", chacha20poly1305.KeySize, len(key))
	}
	nonce, err := aeadBytesArg(args[1], "crypt.chacha20Decrypt nonce")
	if err != nil {
		return nil, err
	}
	ciphertext, err := aeadBytesArg(args[2], "crypt.chacha20Decrypt ciphertext")
	if err != nil {
		return nil, err
	}
	aad, err := aeadOptionalAAD(args, 3, "crypt.chacha20Decrypt")
	if err != nil {
		return nil, err
	}
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, fmt.Errorf("crypt.chacha20Decrypt: %w", err)
	}
	if len(nonce) != aead.NonceSize() {
		return nil, fmt.Errorf("crypt.chacha20Decrypt nonce must be %d bytes", aead.NonceSize())
	}
	plaintext, err := aead.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return nil, fmt.Errorf("crypt.chacha20Decrypt: authentication failed")
	}
	return runtime.Bytes{Value: plaintext}, nil
}

type argon2idParams struct {
	memory      uint32
	time        uint32
	parallelism uint8
	keyLength   uint32
	saltLength  int
}

func defaultArgon2idParams() argon2idParams {
	return argon2idParams{
		memory:      64 * 1024,
		time:        3,
		parallelism: 4,
		keyLength:   32,
		saltLength:  16,
	}
}

func applyArgon2idOptions(params *argon2idParams, options runtime.Dict) error {
	if value, ok := dictInt64(options, "memory"); ok {
		if value < 8 || value > 4*1024*1024 {
			return fmt.Errorf("crypt.argon2idHash memory must be between 8 and 4194304 KiB")
		}
		params.memory = uint32(value)
	}
	if value, ok := dictInt64(options, "time"); ok {
		if value < 1 || value > 32 {
			return fmt.Errorf("crypt.argon2idHash time must be between 1 and 32")
		}
		params.time = uint32(value)
	}
	if value, ok := dictInt64(options, "parallelism"); ok {
		if value < 1 || value > 255 {
			return fmt.Errorf("crypt.argon2idHash parallelism must be between 1 and 255")
		}
		params.parallelism = uint8(value)
	}
	if value, ok := dictInt64(options, "keyLength"); ok {
		if value < 16 || value > 1024 {
			return fmt.Errorf("crypt.argon2idHash keyLength must be between 16 and 1024")
		}
		params.keyLength = uint32(value)
	}
	if value, ok := dictInt64(options, "saltLength"); ok {
		if value < 8 || value > 1024 {
			return fmt.Errorf("crypt.argon2idHash saltLength must be between 8 and 1024")
		}
		params.saltLength = int(value)
	}
	return nil
}

func parseArgon2idHash(encoded string) (argon2idParams, []byte, []byte, error) {
	params, salt, hash, _, err := parseArgon2Hash(encoded)
	return params, salt, hash, err
}

// parseArgon2Hash accepts any of the three Argon2 variants PHP emits:
// $argon2i$, $argon2d$, and $argon2id$. Returns the variant name so callers
// can dispatch to the right argon2 derivation function.
func parseArgon2Hash(encoded string) (argon2idParams, []byte, []byte, string, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 {
		return argon2idParams{}, nil, nil, "", fmt.Errorf("invalid argon2 hash")
	}
	variant := parts[1]
	if variant != "argon2id" && variant != "argon2i" && variant != "argon2d" {
		return argon2idParams{}, nil, nil, "", fmt.Errorf("invalid argon2 hash")
	}
	if parts[2] != "v=19" {
		return argon2idParams{}, nil, nil, "", fmt.Errorf("invalid argon2 hash")
	}
	params := argon2idParams{keyLength: 32}
	for _, item := range strings.Split(parts[3], ",") {
		pair := strings.SplitN(item, "=", 2)
		if len(pair) != 2 {
			return argon2idParams{}, nil, nil, "", fmt.Errorf("invalid argon2 parameters")
		}
		value, err := strconv.ParseUint(pair[1], 10, 32)
		if err != nil {
			return argon2idParams{}, nil, nil, "", err
		}
		switch pair[0] {
		case "m":
			params.memory = uint32(value)
		case "t":
			params.time = uint32(value)
		case "p":
			if value > 255 {
				return argon2idParams{}, nil, nil, "", fmt.Errorf("invalid argon2 parallelism")
			}
			params.parallelism = uint8(value)
		default:
			return argon2idParams{}, nil, nil, "", fmt.Errorf("unknown argon2 parameter")
		}
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return argon2idParams{}, nil, nil, "", err
	}
	hash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return argon2idParams{}, nil, nil, "", err
	}
	if params.memory == 0 || params.time == 0 || params.parallelism == 0 || len(salt) == 0 || len(hash) == 0 {
		return argon2idParams{}, nil, nil, "", fmt.Errorf("invalid argon2 hash")
	}
	params.keyLength = uint32(len(hash))
	return params, salt, hash, variant, nil
}

// singleHashInput accepts either a string or bytes value and returns
// the raw bytes. Used by every crypt hash so binary content can be
// hashed without round-tripping through hex.
func singleHashInput(args []runtime.Value, label string) ([]byte, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects exactly one argument", label)
	}
	switch v := args[0].(type) {
	case runtime.String:
		return []byte(v.Value), nil
	case runtime.Bytes:
		return v.Value, nil
	}
	return nil, fmt.Errorf("%s expects a string or bytes argument", label)
}

// hmacInputs accepts (key, message) pairs where either side may be
// string or bytes. Used by hmacSha256 + hmacSha256Bytes.
func hmacInputs(args []runtime.Value, label string) ([]byte, []byte, error) {
	if len(args) != 2 {
		return nil, nil, fmt.Errorf("%s expects exactly two arguments", label)
	}
	key, err := bytesLike(args[0], label+" key")
	if err != nil {
		return nil, nil, err
	}
	msg, err := bytesLike(args[1], label+" message")
	if err != nil {
		return nil, nil, err
	}
	return key, msg, nil
}

func bytesLike(v runtime.Value, label string) ([]byte, error) {
	switch x := v.(type) {
	case runtime.String:
		return []byte(x.Value), nil
	case runtime.Bytes:
		return x.Value, nil
	}
	return nil, fmt.Errorf("%s must be string or bytes", label)
}
