package native

import (
	gocrypto "crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"hash"
	"math/big"
	"net"
	"strings"
	"time"

	"geblang/internal/runtime"

	pkcs12 "software.sslmate.com/src/go-pkcs12"
)

func registerCryptPKI(r *Registry) {
	r.Register("crypt", "generateRsaKey", func(args []runtime.Value) (runtime.Value, error) {
		bits := int64(2048)
		if len(args) == 1 {
			n, err := singleInt64(args, "crypt.generateRsaKey")
			if err != nil {
				return nil, err
			}
			bits = n
		} else if len(args) > 1 {
			return nil, fmt.Errorf("crypt.generateRsaKey expects 0 or 1 arguments")
		}
		if bits < 1024 || bits > 8192 {
			return nil, fmt.Errorf("crypt.generateRsaKey bits must be between 1024 and 8192")
		}
		key, err := rsa.GenerateKey(rand.Reader, int(bits))
		if err != nil {
			return nil, fmt.Errorf("crypt.generateRsaKey: %w", err)
		}
		return marshalPrivKeyPEM(key)
	})

	r.Register("crypt", "generateEcKey", func(args []runtime.Value) (runtime.Value, error) {
		curveName := "P-256"
		if len(args) == 1 {
			s, ok := args[0].(runtime.String)
			if !ok {
				return nil, fmt.Errorf("crypt.generateEcKey curve must be string")
			}
			curveName = s.Value
		} else if len(args) > 1 {
			return nil, fmt.Errorf("crypt.generateEcKey expects 0 or 1 arguments")
		}
		curve, err := namedCurve(curveName, "crypt.generateEcKey")
		if err != nil {
			return nil, err
		}
		key, err := ecdsa.GenerateKey(curve, rand.Reader)
		if err != nil {
			return nil, fmt.Errorf("crypt.generateEcKey: %w", err)
		}
		return marshalPrivKeyPEM(key)
	})

	r.Register("crypt", "generateEd25519Key", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 0 {
			return nil, fmt.Errorf("crypt.generateEd25519Key expects no arguments")
		}
		_, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return nil, fmt.Errorf("crypt.generateEd25519Key: %w", err)
		}
		return marshalPrivKeyPEM(priv)
	})

	r.Register("crypt", "publicKey", func(args []runtime.Value) (runtime.Value, error) {
		pemStr, err := singleString(args, "crypt.publicKey")
		if err != nil {
			return nil, err
		}
		priv, err := parsePrivKeyPEM(pemStr, "crypt.publicKey")
		if err != nil {
			return nil, err
		}
		var pub interface{}
		switch k := priv.(type) {
		case *rsa.PrivateKey:
			pub = k.Public()
		case *ecdsa.PrivateKey:
			pub = k.Public()
		case ed25519.PrivateKey:
			pub = k.Public()
		default:
			return nil, fmt.Errorf("crypt.publicKey unsupported key type")
		}
		der, err := x509.MarshalPKIXPublicKey(pub)
		if err != nil {
			return nil, fmt.Errorf("crypt.publicKey marshal: %w", err)
		}
		return runtime.String{Value: string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))}, nil
	})

	r.Register("crypt", "generateSelfSignedCert", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("crypt.generateSelfSignedCert expects an options dict")
		}
		opts, ok := args[0].(runtime.Dict)
		if !ok {
			return nil, fmt.Errorf("crypt.generateSelfSignedCert options must be dict")
		}

		subject := parsePkixName(opts, "subject")
		dnsNames := parseDNSNames(opts)
		ipAddresses := parseIPAddresses(opts)
		validDays := dictInt(opts, "validDays", 365)

		var privKey interface{}
		var privKeyPEM string

		if existingPEM := dictStr(opts, "key"); existingPEM != "" {
			k, err := parsePrivKeyPEM(existingPEM, "crypt.generateSelfSignedCert")
			if err != nil {
				return nil, err
			}
			privKey = k
			privKeyPEM = existingPEM
		} else {
			keyType := dictStr(opts, "keyType")
			if keyType == "" {
				keyType = "EC-P256"
			}
			k, pemStr, err := generateKeyByType(keyType, "crypt.generateSelfSignedCert")
			if err != nil {
				return nil, err
			}
			privKey = k
			privKeyPEM = pemStr
		}

		pubKey, err := extractPublicKey(privKey, "crypt.generateSelfSignedCert")
		if err != nil {
			return nil, err
		}

		serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
		if err != nil {
			return nil, fmt.Errorf("crypt.generateSelfSignedCert serial: %w", err)
		}
		now := time.Now()
		tmpl := &x509.Certificate{
			SerialNumber:          serial,
			Subject:               subject,
			NotBefore:             now,
			NotAfter:              now.Add(time.Duration(validDays) * 24 * time.Hour),
			KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
			ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
			BasicConstraintsValid: true,
			IsCA:                  true,
			DNSNames:              dnsNames,
			IPAddresses:           ipAddresses,
		}
		certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, pubKey, privKey)
		if err != nil {
			return nil, fmt.Errorf("crypt.generateSelfSignedCert: %w", err)
		}
		certPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}))

		certKey := runtime.String{Value: "cert"}
		keyKey := runtime.String{Value: "key"}
		entries := map[string]runtime.DictEntry{
			DictKey(certKey): {Key: certKey, Value: runtime.String{Value: certPEM}},
			DictKey(keyKey):  {Key: keyKey, Value: runtime.String{Value: privKeyPEM}},
		}
		return runtime.Dict{Entries: entries}, nil
	})

	r.Register("crypt", "generateCsr", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("crypt.generateCsr expects an options dict")
		}
		opts, ok := args[0].(runtime.Dict)
		if !ok {
			return nil, fmt.Errorf("crypt.generateCsr options must be dict")
		}

		keyPEM := dictStr(opts, "key")
		if keyPEM == "" {
			return nil, fmt.Errorf("crypt.generateCsr options must include 'key'")
		}
		privKey, err := parsePrivKeyPEM(keyPEM, "crypt.generateCsr")
		if err != nil {
			return nil, err
		}

		subject := parsePkixName(opts, "subject")
		dnsNames := parseDNSNames(opts)
		ipAddresses := parseIPAddresses(opts)

		tmpl := &x509.CertificateRequest{
			Subject:     subject,
			DNSNames:    dnsNames,
			IPAddresses: ipAddresses,
		}
		csrDER, err := x509.CreateCertificateRequest(rand.Reader, tmpl, privKey)
		if err != nil {
			return nil, fmt.Errorf("crypt.generateCsr: %w", err)
		}
		return runtime.String{Value: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER}))}, nil
	})

	r.Register("crypt", "parseCert", func(args []runtime.Value) (runtime.Value, error) {
		pemStr, err := singleString(args, "crypt.parseCert")
		if err != nil {
			return nil, err
		}
		block, _ := pem.Decode([]byte(pemStr))
		if block == nil {
			return nil, fmt.Errorf("crypt.parseCert invalid PEM")
		}
		if block.Type != "CERTIFICATE" {
			return nil, fmt.Errorf("crypt.parseCert expected CERTIFICATE PEM, got %q", block.Type)
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("crypt.parseCert: %w", err)
		}

		dnsElems := make([]runtime.Value, len(cert.DNSNames))
		for i, n := range cert.DNSNames {
			dnsElems[i] = runtime.String{Value: n}
		}
		ipElems := make([]runtime.Value, len(cert.IPAddresses))
		for i, ip := range cert.IPAddresses {
			ipElems[i] = runtime.String{Value: ip.String()}
		}

		keyType := "unknown"
		switch cert.PublicKey.(type) {
		case *rsa.PublicKey:
			keyType = "RSA"
		case *ecdsa.PublicKey:
			keyType = "EC"
		case ed25519.PublicKey:
			keyType = "Ed25519"
		}

		subjectDict := pkixNameToDict(cert.Subject)
		issuerDict := pkixNameToDict(cert.Issuer)

		entries := map[string]runtime.DictEntry{}
		setEntry := func(key string, val runtime.Value) {
			k := runtime.String{Value: key}
			entries[DictKey(k)] = runtime.DictEntry{Key: k, Value: val}
		}
		setEntry("subject", subjectDict)
		setEntry("issuer", issuerDict)
		setEntry("dnsNames", &runtime.List{Elements: dnsElems})
		setEntry("ipAddresses", &runtime.List{Elements: ipElems})
		setEntry("notBefore", runtime.String{Value: cert.NotBefore.UTC().Format(time.RFC3339)})
		setEntry("notAfter", runtime.String{Value: cert.NotAfter.UTC().Format(time.RFC3339)})
		setEntry("serialNumber", runtime.String{Value: cert.SerialNumber.Text(16)})
		setEntry("keyType", runtime.String{Value: keyType})
		setEntry("isCA", runtime.Bool{Value: cert.IsCA})

		if pubDER, perr := x509.MarshalPKIXPublicKey(cert.PublicKey); perr == nil {
			pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})
			setEntry("publicKey", runtime.String{Value: string(pubPEM)})
		}
		extElems := make([]runtime.Value, 0, len(cert.Extensions))
		for _, ext := range cert.Extensions {
			extEntries := map[string]runtime.DictEntry{}
			putStr := func(k, v string) {
				key := runtime.String{Value: k}
				extEntries[DictKey(key)] = runtime.DictEntry{Key: key, Value: runtime.String{Value: v}}
			}
			putStr("oid", ext.Id.String())
			critKey := runtime.String{Value: "critical"}
			extEntries[DictKey(critKey)] = runtime.DictEntry{Key: critKey, Value: runtime.Bool{Value: ext.Critical}}
			valKey := runtime.String{Value: "value"}
			extEntries[DictKey(valKey)] = runtime.DictEntry{Key: valKey, Value: runtime.Bytes{Value: append([]byte(nil), ext.Value...)}}
			extElems = append(extElems, runtime.Dict{Entries: extEntries})
		}
		setEntry("extensions", &runtime.List{Elements: extElems})

		return runtime.Dict{Entries: entries}, nil
	})

	r.Register("crypt", "signCertificate", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("crypt.signCertificate expects an options dict")
		}
		opts, ok := args[0].(runtime.Dict)
		if !ok {
			return nil, fmt.Errorf("crypt.signCertificate options must be dict")
		}
		csrPEM := dictStr(opts, "csr")
		if csrPEM == "" {
			return nil, fmt.Errorf("crypt.signCertificate options must include 'csr'")
		}
		caCertPEM := dictStr(opts, "caCert")
		if caCertPEM == "" {
			return nil, fmt.Errorf("crypt.signCertificate options must include 'caCert'")
		}
		caKeyPEM := dictStr(opts, "caKey")
		if caKeyPEM == "" {
			return nil, fmt.Errorf("crypt.signCertificate options must include 'caKey'")
		}

		csrBlock, _ := pem.Decode([]byte(csrPEM))
		if csrBlock == nil || csrBlock.Type != "CERTIFICATE REQUEST" {
			return nil, fmt.Errorf("crypt.signCertificate csr must be a CERTIFICATE REQUEST PEM")
		}
		csr, err := x509.ParseCertificateRequest(csrBlock.Bytes)
		if err != nil {
			return nil, fmt.Errorf("crypt.signCertificate parse csr: %w", err)
		}
		if err := csr.CheckSignature(); err != nil {
			return nil, fmt.Errorf("crypt.signCertificate csr signature: %w", err)
		}

		caCertBlock, _ := pem.Decode([]byte(caCertPEM))
		if caCertBlock == nil || caCertBlock.Type != "CERTIFICATE" {
			return nil, fmt.Errorf("crypt.signCertificate caCert must be a CERTIFICATE PEM")
		}
		caCert, err := x509.ParseCertificate(caCertBlock.Bytes)
		if err != nil {
			return nil, fmt.Errorf("crypt.signCertificate parse caCert: %w", err)
		}
		caKey, err := parsePrivKeyPEM(caKeyPEM, "crypt.signCertificate")
		if err != nil {
			return nil, err
		}

		validDays := dictInt(opts, "validDays", 365)
		isCA := dictBool(opts, "isCA")
		serialBits := dictInt(opts, "serialBits", 128)
		if serialBits < 32 {
			serialBits = 128
		}
		serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), uint(serialBits)))
		if err != nil {
			return nil, fmt.Errorf("crypt.signCertificate serial: %w", err)
		}

		now := time.Now()
		keyUsage := x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment
		if isCA {
			keyUsage |= x509.KeyUsageCertSign
		}
		tmpl := &x509.Certificate{
			SerialNumber:          serial,
			Subject:               csr.Subject,
			NotBefore:             now,
			NotAfter:              now.Add(time.Duration(validDays) * 24 * time.Hour),
			KeyUsage:              keyUsage,
			ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
			BasicConstraintsValid: true,
			IsCA:                  isCA,
			DNSNames:              csr.DNSNames,
			IPAddresses:           csr.IPAddresses,
			EmailAddresses:        csr.EmailAddresses,
			URIs:                  csr.URIs,
		}
		if extraDNS := parseDNSNames(opts); len(extraDNS) > 0 {
			tmpl.DNSNames = append(tmpl.DNSNames, extraDNS...)
		}
		if extraIPs := parseIPAddresses(opts); len(extraIPs) > 0 {
			tmpl.IPAddresses = append(tmpl.IPAddresses, extraIPs...)
		}

		certDER, err := x509.CreateCertificate(rand.Reader, tmpl, caCert, csr.PublicKey, caKey)
		if err != nil {
			return nil, fmt.Errorf("crypt.signCertificate: %w", err)
		}
		certPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}))
		return runtime.String{Value: certPEM}, nil
	})

	r.Register("crypt", "pkcs12Decode", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) < 1 || len(args) > 2 {
			return nil, fmt.Errorf("crypt.pkcs12Decode expects pfx bytes and optional password")
		}
		pfxData, err := aeadBytesArg(args[0], "crypt.pkcs12Decode pfx")
		if err != nil {
			return nil, err
		}
		password := ""
		if len(args) == 2 {
			ps, ok := args[1].(runtime.String)
			if !ok {
				return nil, fmt.Errorf("crypt.pkcs12Decode password must be string")
			}
			password = ps.Value
		}
		privKey, leafCert, caCerts, err := pkcs12.DecodeChain(pfxData, password)
		if err != nil {
			return nil, fmt.Errorf("crypt.pkcs12Decode: %w", err)
		}
		keyPEM, err := marshalPrivKeyPEM(privKey)
		if err != nil {
			return nil, err
		}
		certPEMs := []string{}
		if leafCert != nil {
			certPEMs = append(certPEMs, string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafCert.Raw})))
		}
		caElems := make([]runtime.Value, 0, len(caCerts))
		for _, c := range caCerts {
			caElems = append(caElems, runtime.String{Value: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.Raw}))})
		}
		entries := map[string]runtime.DictEntry{}
		setEntry := func(key string, val runtime.Value) {
			k := runtime.String{Value: key}
			entries[DictKey(k)] = runtime.DictEntry{Key: k, Value: val}
		}
		setEntry("key", keyPEM)
		if len(certPEMs) > 0 {
			setEntry("cert", runtime.String{Value: certPEMs[0]})
		} else {
			setEntry("cert", runtime.Null{})
		}
		setEntry("caCerts", &runtime.List{Elements: caElems})
		return runtime.Dict{Entries: entries}, nil
	})

	r.Register("crypt", "jweEncrypt", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) < 2 || len(args) > 3 {
			return nil, fmt.Errorf("crypt.jweEncrypt expects payload, key, and optional opts")
		}
		alg := "dir"
		enc := "A256GCM"
		if len(args) == 3 {
			a, err := jwtOptsAlg(args[2], "crypt.jweEncrypt")
			if err != nil {
				return nil, err
			}
			if a != "" {
				alg = a
			}
			e, err := jweOptsString(args[2], "enc", "crypt.jweEncrypt")
			if err != nil {
				return nil, err
			}
			if e != "" {
				enc = e
			}
		}
		if enc != "A256GCM" {
			return nil, fmt.Errorf("crypt.jweEncrypt unsupported enc %q (supported: A256GCM)", enc)
		}
		payload, err := jwePayloadBytes(args[0], "crypt.jweEncrypt")
		if err != nil {
			return nil, err
		}
		cek, encryptedKey, err := jweWrapKey(alg, args[1], "crypt.jweEncrypt")
		if err != nil {
			return nil, err
		}
		header := fmt.Sprintf(`{"alg":%q,"enc":%q}`, alg, enc)
		headerB64 := base64.RawURLEncoding.EncodeToString([]byte(header))
		block, err := aes.NewCipher(cek)
		if err != nil {
			return nil, fmt.Errorf("crypt.jweEncrypt cipher: %w", err)
		}
		gcm, err := cipher.NewGCM(block)
		if err != nil {
			return nil, fmt.Errorf("crypt.jweEncrypt gcm: %w", err)
		}
		iv := make([]byte, gcm.NonceSize())
		if _, err := rand.Read(iv); err != nil {
			return nil, fmt.Errorf("crypt.jweEncrypt iv: %w", err)
		}
		sealed := gcm.Seal(nil, iv, payload, []byte(headerB64))
		tag := sealed[len(sealed)-gcm.Overhead():]
		ciphertext := sealed[:len(sealed)-gcm.Overhead()]
		token := strings.Join([]string{
			headerB64,
			base64.RawURLEncoding.EncodeToString(encryptedKey),
			base64.RawURLEncoding.EncodeToString(iv),
			base64.RawURLEncoding.EncodeToString(ciphertext),
			base64.RawURLEncoding.EncodeToString(tag),
		}, ".")
		return runtime.String{Value: token}, nil
	})

	r.Register("crypt", "jweDecrypt", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("crypt.jweDecrypt expects token and key")
		}
		tokenStr, ok := args[0].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("crypt.jweDecrypt token must be string")
		}
		parts := strings.Split(tokenStr.Value, ".")
		if len(parts) != 5 {
			return nil, fmt.Errorf("crypt.jweDecrypt: token must have 5 segments")
		}
		headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
		if err != nil {
			return nil, fmt.Errorf("crypt.jweDecrypt header: %w", err)
		}
		var hdr struct {
			Alg string `json:"alg"`
			Enc string `json:"enc"`
		}
		if err := json.Unmarshal(headerBytes, &hdr); err != nil {
			return nil, fmt.Errorf("crypt.jweDecrypt header json: %w", err)
		}
		if hdr.Enc != "A256GCM" {
			return nil, fmt.Errorf("crypt.jweDecrypt unsupported enc %q (supported: A256GCM)", hdr.Enc)
		}
		encryptedKey, err := base64.RawURLEncoding.DecodeString(parts[1])
		if err != nil {
			return nil, fmt.Errorf("crypt.jweDecrypt encryptedKey: %w", err)
		}
		iv, err := base64.RawURLEncoding.DecodeString(parts[2])
		if err != nil {
			return nil, fmt.Errorf("crypt.jweDecrypt iv: %w", err)
		}
		ciphertext, err := base64.RawURLEncoding.DecodeString(parts[3])
		if err != nil {
			return nil, fmt.Errorf("crypt.jweDecrypt ciphertext: %w", err)
		}
		tag, err := base64.RawURLEncoding.DecodeString(parts[4])
		if err != nil {
			return nil, fmt.Errorf("crypt.jweDecrypt tag: %w", err)
		}
		cek, err := jweUnwrapKey(hdr.Alg, args[1], encryptedKey, "crypt.jweDecrypt")
		if err != nil {
			return nil, err
		}
		block, err := aes.NewCipher(cek)
		if err != nil {
			return nil, fmt.Errorf("crypt.jweDecrypt cipher: %w", err)
		}
		gcm, err := cipher.NewGCM(block)
		if err != nil {
			return nil, fmt.Errorf("crypt.jweDecrypt gcm: %w", err)
		}
		sealed := append(ciphertext, tag...)
		plaintext, err := gcm.Open(nil, iv, sealed, []byte(parts[0]))
		if err != nil {
			return nil, fmt.Errorf("crypt.jweDecrypt: %w", err)
		}
		return runtime.Bytes{Value: plaintext}, nil
	})

	// Per-algorithm JWT functions are deprecated thin shims that
	// delegate to the unified crypt.jwtSign / crypt.jwtVerify pair.
	// Kept for one release window; remove in 1.5.0.

	r.Register("crypt", "jwtSignRS256", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("crypt.jwtSignRS256 expects payload and privatePem")
		}
		return jwtSignWithAlg(args[0], args[1], "RS256", nil, "", "crypt.jwtSignRS256")
	})
	r.Register("crypt", "jwtVerifyRS256", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("crypt.jwtVerifyRS256 expects token and publicPem")
		}
		token, ok := args[0].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("crypt.jwtVerifyRS256 token must be string")
		}
		return jwtVerifyWithAlg(token.Value, args[1], []string{"RS256"}, "crypt.jwtVerifyRS256")
	})
	r.Register("crypt", "jwtSignES256", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("crypt.jwtSignES256 expects payload and privatePem")
		}
		return jwtSignWithAlg(args[0], args[1], "ES256", nil, "", "crypt.jwtSignES256")
	})
	r.Register("crypt", "jwtVerifyES256", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("crypt.jwtVerifyES256 expects token and publicPem")
		}
		token, ok := args[0].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("crypt.jwtVerifyES256 token must be string")
		}
		return jwtVerifyWithAlg(token.Value, args[1], []string{"ES256"}, "crypt.jwtVerifyES256")
	})
}

// — helpers —

func marshalPrivKeyPEM(key interface{}) (runtime.Value, error) {
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("marshal private key: %w", err)
	}
	return runtime.String{Value: string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))}, nil
}

func parsePrivKeyPEM(pemStr, label string) (interface{}, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, fmt.Errorf("%s invalid PEM", label)
	}
	switch block.Type {
	case "PRIVATE KEY":
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("%s invalid PKCS#8 key: %w", label, err)
		}
		return key, nil
	case "RSA PRIVATE KEY":
		key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("%s invalid RSA key: %w", label, err)
		}
		return key, nil
	case "EC PRIVATE KEY":
		key, err := x509.ParseECPrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("%s invalid EC key: %w", label, err)
		}
		return key, nil
	default:
		return nil, fmt.Errorf("%s expected private key PEM, got %q", label, block.Type)
	}
}

func parsePublicKeyPEM(pemStr, label string) (interface{}, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, fmt.Errorf("%s invalid PEM", label)
	}
	switch block.Type {
	case "PUBLIC KEY":
		pub, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("%s invalid public key: %w", label, err)
		}
		return pub, nil
	case "CERTIFICATE":
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("%s invalid certificate: %w", label, err)
		}
		return cert.PublicKey, nil
	default:
		return nil, fmt.Errorf("%s expected PUBLIC KEY or CERTIFICATE PEM, got %q", label, block.Type)
	}
}

func extractPublicKey(priv interface{}, label string) (interface{}, error) {
	switch k := priv.(type) {
	case *rsa.PrivateKey:
		return k.Public(), nil
	case *ecdsa.PrivateKey:
		return k.Public(), nil
	case ed25519.PrivateKey:
		return k.Public(), nil
	default:
		return nil, fmt.Errorf("%s unsupported key type", label)
	}
}

func namedCurve(name, label string) (elliptic.Curve, error) {
	switch name {
	case "P-256":
		return elliptic.P256(), nil
	case "P-384":
		return elliptic.P384(), nil
	case "P-521":
		return elliptic.P521(), nil
	default:
		return nil, fmt.Errorf("%s unsupported curve %q; use P-256, P-384, or P-521", label, name)
	}
}

func generateKeyByType(keyType, label string) (interface{}, string, error) {
	var key interface{}
	var err error
	switch keyType {
	case "RSA2048":
		key, err = rsa.GenerateKey(rand.Reader, 2048)
	case "RSA4096":
		key, err = rsa.GenerateKey(rand.Reader, 4096)
	case "EC-P256":
		key, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	case "EC-P384":
		key, err = ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	case "EC-P521":
		key, err = ecdsa.GenerateKey(elliptic.P521(), rand.Reader)
	case "Ed25519":
		_, key, err = ed25519.GenerateKey(rand.Reader)
	default:
		return nil, "", fmt.Errorf("%s unsupported keyType %q; use RSA2048, RSA4096, EC-P256, EC-P384, EC-P521, or Ed25519", label, keyType)
	}
	if err != nil {
		return nil, "", fmt.Errorf("%s key gen: %w", label, err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, "", fmt.Errorf("%s marshal: %w", label, err)
	}
	pemStr := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
	return key, pemStr, nil
}

func dictStr(d runtime.Dict, key string) string {
	e, ok := d.GetEntry(DictKey(runtime.String{Value: key}))
	if !ok {
		return ""
	}
	s, ok := e.Value.(runtime.String)
	if !ok {
		return ""
	}
	return s.Value
}

func dictInt(d runtime.Dict, key string, defaultVal int) int {
	e, ok := d.GetEntry(DictKey(runtime.String{Value: key}))
	if !ok {
		return defaultVal
	}
	if n, ok := AsInt64(e.Value); ok {
		return int(n)
	}
	return defaultVal
}

func parsePkixName(opts runtime.Dict, key string) pkix.Name {
	entry, ok := opts.GetEntry(DictKey(runtime.String{Value: key}))
	if !ok {
		return pkix.Name{}
	}
	sub, ok := entry.Value.(runtime.Dict)
	if !ok {
		return pkix.Name{}
	}
	name := pkix.Name{}
	if cn := dictStr(sub, "commonName"); cn != "" {
		name.CommonName = cn
	}
	if org := dictStr(sub, "organization"); org != "" {
		name.Organization = []string{org}
	}
	if country := dictStr(sub, "country"); country != "" {
		name.Country = []string{country}
	}
	if state := dictStr(sub, "state"); state != "" {
		name.Province = []string{state}
	}
	if locality := dictStr(sub, "locality"); locality != "" {
		name.Locality = []string{locality}
	}
	return name
}

func parseDNSNames(opts runtime.Dict) []string {
	entry, ok := opts.GetEntry(DictKey(runtime.String{Value: "dnsNames"}))
	if !ok {
		return nil
	}
	list, ok := entry.Value.(*runtime.List)
	if !ok {
		return nil
	}
	var names []string
	for _, v := range list.Elements {
		if s, ok := v.(runtime.String); ok {
			names = append(names, s.Value)
		}
	}
	return names
}

func parseIPAddresses(opts runtime.Dict) []net.IP {
	entry, ok := opts.GetEntry(DictKey(runtime.String{Value: "ipAddresses"}))
	if !ok {
		return nil
	}
	list, ok := entry.Value.(*runtime.List)
	if !ok {
		return nil
	}
	var ips []net.IP
	for _, v := range list.Elements {
		if s, ok := v.(runtime.String); ok {
			if ip := net.ParseIP(s.Value); ip != nil {
				ips = append(ips, ip)
			}
		}
	}
	return ips
}

func pkixNameToDict(name pkix.Name) runtime.Value {
	entries := map[string]runtime.DictEntry{}
	setStr := func(key, val string) {
		if val == "" {
			return
		}
		k := runtime.String{Value: key}
		entries[DictKey(k)] = runtime.DictEntry{Key: k, Value: runtime.String{Value: val}}
	}
	setStr("commonName", name.CommonName)
	if len(name.Organization) > 0 {
		setStr("organization", name.Organization[0])
	}
	if len(name.Country) > 0 {
		setStr("country", name.Country[0])
	}
	if len(name.Province) > 0 {
		setStr("state", name.Province[0])
	}
	if len(name.Locality) > 0 {
		setStr("locality", name.Locality[0])
	}
	return runtime.Dict{Entries: entries}
}

func sha256Sum(input string) []byte {
	h := sha256.New()
	h.Write([]byte(input))
	return h.Sum(nil)
}

func jwtBuildSigInput(payloadVal runtime.Value, alg string, kid string) (string, error) {
	payloadJSON, err := ValueToJSON(payloadVal)
	if err != nil {
		return "", fmt.Errorf("payload: %w", err)
	}
	payloadBytes, err := json.Marshal(payloadJSON)
	if err != nil {
		return "", fmt.Errorf("payload encoding: %w", err)
	}
	headerJSON := fmt.Sprintf(`{"alg":%q,"typ":"JWT"}`, alg)
	if kid != "" {
		headerJSON = fmt.Sprintf(`{"alg":%q,"kid":%q,"typ":"JWT"}`, alg, kid)
	}
	header := base64.RawURLEncoding.EncodeToString([]byte(headerJSON))
	payload := base64.RawURLEncoding.EncodeToString(payloadBytes)
	return header + "." + payload, nil
}

func jwtDecodePayload(b64 string) (runtime.Value, error) {
	payloadBytes, err := base64.RawURLEncoding.DecodeString(b64)
	if err != nil {
		return runtime.Null{}, nil
	}
	value, parseErr := ParseJSONText(string(payloadBytes))
	if parseErr != nil {
		return runtime.Null{}, nil
	}
	return value, nil
}

// jwtHashForAlg returns the digest algorithm + bytes the supplied alg
// signs over. EdDSA and "none" do not parameterise a hash function;
// the caller handles them through dedicated code paths.
func jwtHashForAlg(alg string) (gocrypto.Hash, func() hash.Hash, error) {
	switch alg {
	case "HS256", "RS256", "ES256":
		return gocrypto.SHA256, sha256.New, nil
	case "HS384", "RS384", "ES384":
		return gocrypto.SHA384, sha512.New384, nil
	case "HS512", "RS512", "ES512":
		return gocrypto.SHA512, sha512.New, nil
	case "EdDSA":
		return 0, nil, nil
	case "none":
		return 0, nil, nil
	default:
		return 0, nil, fmt.Errorf("unsupported alg %q", alg)
	}
}

// jwtAlgIsAllowed enforces the alg allow-list semantics. allowedAlgs
// nil means "default policy": every supported algorithm EXCEPT
// "none" passes. A non-nil allowedAlgs is a strict membership check;
// callers must include "none" in the list to opt in to unsigned
// tokens. This is the standard defence against alg-confusion
// attacks AND keeps "none" off the default code path on both the
// sign and verify sides.
func jwtAlgIsAllowed(alg string, allowedAlgs []string) bool {
	if allowedAlgs == nil {
		return alg != "none"
	}
	return stringInSlice(alg, allowedAlgs)
}

// jwtKeyBytes extracts a byte slice from a String or Bytes runtime
// value. Used by HMAC sign / verify which accept either key shape.
func jwtKeyBytes(key runtime.Value, label string) ([]byte, error) {
	switch k := key.(type) {
	case runtime.String:
		return []byte(k.Value), nil
	case runtime.Bytes:
		return k.Value, nil
	default:
		return nil, fmt.Errorf("%s key must be string or bytes", label)
	}
}

// jwtKeyPEM extracts a PEM-encoded key string. PEM is text by
// convention so we require runtime.String here; bytes values are
// converted with `bytes.toString` at the call site if needed.
func jwtKeyPEM(key runtime.Value, label string) (string, error) {
	s, ok := key.(runtime.String)
	if !ok {
		return "", fmt.Errorf("%s key must be a PEM string", label)
	}
	return s.Value, nil
}

// jwtSignWithAlg dispatches the payload + key onto the alg-specific
// signing routine and returns the compact JWT serialisation.
// allowedAlgs guards which alg the caller is permitted to sign with;
// nil means "default policy" (everything except "none"). Pass
// []string{"none"} (or include "none" in your list) to explicitly
// produce an unsigned token.
func jwtSignWithAlg(payloadVal runtime.Value, key runtime.Value, alg string, allowedAlgs []string, kid string, label string) (runtime.Value, error) {
	if !jwtAlgIsAllowed(alg, allowedAlgs) {
		if alg == "none" {
			return nil, fmt.Errorf("%s: alg \"none\" rejected by default; pass opts.allowedAlgs containing \"none\" to opt in", label)
		}
		return nil, fmt.Errorf("%s: alg %q not in opts.allowedAlgs", label, alg)
	}
	hashID, newHash, err := jwtHashForAlg(alg)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", label, err)
	}
	sigInput, err := jwtBuildSigInput(payloadVal, alg, kid)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", label, err)
	}
	if alg == "none" {
		// Unsigned token: trailing dot, empty signature.
		return runtime.String{Value: sigInput + "."}, nil
	}
	var sig []byte
	switch {
	case strings.HasPrefix(alg, "HS"):
		secret, err := jwtKeyBytes(key, label)
		if err != nil {
			return nil, err
		}
		mac := hmac.New(newHash, secret)
		mac.Write([]byte(sigInput))
		sig = mac.Sum(nil)
	case strings.HasPrefix(alg, "RS"):
		pem, err := jwtKeyPEM(key, label)
		if err != nil {
			return nil, err
		}
		priv, err := parsePrivKeyPEM(pem, label)
		if err != nil {
			return nil, err
		}
		rsaKey, ok := priv.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("%s key must be RSA for %s", label, alg)
		}
		h := newHash()
		h.Write([]byte(sigInput))
		sig, err = rsa.SignPKCS1v15(rand.Reader, rsaKey, hashID, h.Sum(nil))
		if err != nil {
			return nil, fmt.Errorf("%s sign: %w", label, err)
		}
	case strings.HasPrefix(alg, "ES"):
		pem, err := jwtKeyPEM(key, label)
		if err != nil {
			return nil, err
		}
		priv, err := parsePrivKeyPEM(pem, label)
		if err != nil {
			return nil, err
		}
		ecKey, ok := priv.(*ecdsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("%s key must be EC for %s", label, alg)
		}
		if err := jwtCheckECCurveForAlg(alg, ecKey.Curve, label); err != nil {
			return nil, err
		}
		h := newHash()
		h.Write([]byte(sigInput))
		rVal, sVal, err := ecdsa.Sign(rand.Reader, ecKey, h.Sum(nil))
		if err != nil {
			return nil, fmt.Errorf("%s sign: %w", label, err)
		}
		keySize := (ecKey.Curve.Params().N.BitLen() + 7) / 8
		sig = make([]byte, 2*keySize)
		rVal.FillBytes(sig[:keySize])
		sVal.FillBytes(sig[keySize:])
	case alg == "EdDSA":
		pem, err := jwtKeyPEM(key, label)
		if err != nil {
			return nil, err
		}
		priv, err := parsePrivKeyPEM(pem, label)
		if err != nil {
			return nil, err
		}
		edKey, ok := priv.(ed25519.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("%s key must be Ed25519 for EdDSA", label)
		}
		sig = ed25519.Sign(edKey, []byte(sigInput))
	}
	return runtime.String{Value: sigInput + "." + base64.RawURLEncoding.EncodeToString(sig)}, nil
}

// jwtDefaultAllowedAlgs pins the acceptable algorithm family to the
// key type when the caller gave no opts.algs: PEM keys never verify
// HMAC tokens (closes the HS-with-public-PEM alg-confusion forgery),
// raw secrets verify only the HMAC family.
func jwtDefaultAllowedAlgs(key runtime.Value) []string {
	s, ok := key.(runtime.String)
	if !ok || !strings.Contains(s.Value, "-----BEGIN") {
		return []string{"HS256", "HS384", "HS512"}
	}
	pub, err := parsePublicKeyPEM(s.Value, "jwt")
	if err != nil {
		// Unparseable PEM: keep the loud per-family parse errors but
		// never fall back to HMAC.
		return []string{"RS256", "RS384", "RS512", "ES256", "ES384", "ES512", "EdDSA"}
	}
	switch pub.(type) {
	case *rsa.PublicKey:
		return []string{"RS256", "RS384", "RS512"}
	case *ecdsa.PublicKey:
		return []string{"ES256", "ES384", "ES512"}
	case ed25519.PublicKey:
		return []string{"EdDSA"}
	}
	return []string{"RS256", "RS384", "RS512", "ES256", "ES384", "ES512", "EdDSA"}
}

// jwtVerifyWithAlg reads the supplied token's header to pick a
// verifier. allowedAlgs (when non-empty) gates which algorithms the
// token header may declare - this is the standard defence against
// "alg confusion" attacks where a token claims `none` or an HMAC alg
// while the caller expects an asymmetric scheme.
func jwtVerifyWithAlg(token string, key runtime.Value, allowedAlgs []string, label string) (runtime.Value, error) {
	parts := strings.SplitN(token, ".", 3)
	if len(parts) != 3 {
		return runtime.Null{}, nil
	}
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return runtime.Null{}, nil
	}
	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return runtime.Null{}, nil
	}
	// A dict key is a JWK or JWKS document: resolve the verification
	// key (by kid when the token names one) and pin to its algorithm.
	var jwkKey *jwkVerifyKey
	if jwksDict, ok := key.(runtime.Dict); ok {
		resolved, found := jwkResolve(jwksDict, header.Kid)
		if !found {
			return runtime.Null{}, nil
		}
		jwkKey = resolved
	}
	if allowedAlgs == nil {
		if jwkKey != nil {
			allowedAlgs = jwkAllowedAlgs(jwkKey)
		} else {
			allowedAlgs = jwtDefaultAllowedAlgs(key)
		}
	}
	if !jwtAlgIsAllowed(header.Alg, allowedAlgs) {
		// Disallowed alg (including "none" by default): silent
		// verification failure, same shape as a bad signature.
		return runtime.Null{}, nil
	}
	hashID, newHash, err := jwtHashForAlg(header.Alg)
	if err != nil {
		return runtime.Null{}, nil
	}
	if header.Alg == "none" {
		// Caller opted in to unsigned tokens; trust the claims.
		// The third part must be empty per RFC 7519.
		if parts[2] != "" {
			return runtime.Null{}, nil
		}
		return jwtDecodePayload(parts[1])
	}
	sigBytes, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return runtime.Null{}, nil
	}
	sigInput := parts[0] + "." + parts[1]
	switch {
	case strings.HasPrefix(header.Alg, "HS"):
		var secret []byte
		if jwkKey != nil {
			if jwkKey.secret == nil {
				return runtime.Null{}, nil
			}
			secret = jwkKey.secret
		} else {
			s, err := jwtKeyBytes(key, label)
			if err != nil {
				return nil, err
			}
			secret = s
		}
		mac := hmac.New(newHash, secret)
		mac.Write([]byte(sigInput))
		if !hmac.Equal(sigBytes, mac.Sum(nil)) {
			return runtime.Null{}, nil
		}
	case strings.HasPrefix(header.Alg, "RS"):
		pub, err := jwtAsymmetricKey(key, jwkKey, label)
		if err != nil {
			return nil, err
		}
		rsaPub, ok := pub.(*rsa.PublicKey)
		if !ok {
			return runtime.Null{}, nil
		}
		h := newHash()
		h.Write([]byte(sigInput))
		if err := rsa.VerifyPKCS1v15(rsaPub, hashID, h.Sum(nil), sigBytes); err != nil {
			return runtime.Null{}, nil
		}
	case strings.HasPrefix(header.Alg, "ES"):
		if len(sigBytes) == 0 || len(sigBytes)%2 != 0 {
			return runtime.Null{}, nil
		}
		pub, err := jwtAsymmetricKey(key, jwkKey, label)
		if err != nil {
			return nil, err
		}
		ecPub, ok := pub.(*ecdsa.PublicKey)
		if !ok {
			return runtime.Null{}, nil
		}
		if err := jwtCheckECCurveForAlg(header.Alg, ecPub.Curve, label); err != nil {
			return runtime.Null{}, nil
		}
		h := newHash()
		h.Write([]byte(sigInput))
		half := len(sigBytes) / 2
		rVal := new(big.Int).SetBytes(sigBytes[:half])
		sVal := new(big.Int).SetBytes(sigBytes[half:])
		if !ecdsa.Verify(ecPub, h.Sum(nil), rVal, sVal) {
			return runtime.Null{}, nil
		}
	case header.Alg == "EdDSA":
		pub, err := jwtAsymmetricKey(key, jwkKey, label)
		if err != nil {
			return nil, err
		}
		edPub, ok := pub.(ed25519.PublicKey)
		if !ok {
			return runtime.Null{}, nil
		}
		if !ed25519.Verify(edPub, []byte(sigInput), sigBytes) {
			return runtime.Null{}, nil
		}
	}
	return jwtDecodePayload(parts[1])
}

// jwtAsymmetricKey yields the public key for the RS/ES/EdDSA verify
// branches: the resolved JWK when the caller passed one, else the PEM.
func jwtAsymmetricKey(key runtime.Value, jwkKey *jwkVerifyKey, label string) (interface{}, error) {
	if jwkKey != nil {
		if jwkKey.pub == nil {
			return nil, fmt.Errorf("%s JWK is not an asymmetric key", label)
		}
		return jwkKey.pub, nil
	}
	pemStr, err := jwtKeyPEM(key, label)
	if err != nil {
		return nil, err
	}
	return parsePublicKeyPEM(pemStr, label)
}

func jwtCheckECCurveForAlg(alg string, curve elliptic.Curve, label string) error {
	want := ""
	switch alg {
	case "ES256":
		want = "P-256"
	case "ES384":
		want = "P-384"
	case "ES512":
		want = "P-521"
	default:
		return nil
	}
	if curve.Params().Name != want {
		return fmt.Errorf("%s expects %s for %s, got %s", label, want, alg, curve.Params().Name)
	}
	return nil
}

func stringInSlice(s string, ss []string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

// jwtOptsAlg pulls opts["alg"] from a dict argument.
func jwtOptsAlg(v runtime.Value, label string) (string, error) {
	dict, ok := v.(runtime.Dict)
	if !ok {
		return "", fmt.Errorf("%s opts must be a dict", label)
	}
	for _, dk := range dict.EntryKeys() {
		entry, _ := dict.GetEntry(dk)
		key, ok := entry.Key.(runtime.String)
		if !ok || key.Value != "alg" {
			continue
		}
		alg, ok := entry.Value.(runtime.String)
		if !ok {
			return "", fmt.Errorf("%s opts.alg must be a string", label)
		}
		return alg.Value, nil
	}
	return "", nil
}

// jwtOptsAllowedAlgs pulls opts["allowedAlgs"] (list<string>) from a dict.
func jwtOptsAllowedAlgs(v runtime.Value, label string) ([]string, error) {
	dict, ok := v.(runtime.Dict)
	if !ok {
		return nil, fmt.Errorf("%s opts must be a dict", label)
	}
	for _, dk := range dict.EntryKeys() {
		entry, _ := dict.GetEntry(dk)
		key, ok := entry.Key.(runtime.String)
		if !ok || key.Value != "allowedAlgs" {
			continue
		}
		list, ok := entry.Value.(*runtime.List)
		if !ok {
			return nil, fmt.Errorf("%s opts.allowedAlgs must be a list", label)
		}
		out := make([]string, 0, len(list.Elements))
		for i, el := range list.Elements {
			s, ok := el.(runtime.String)
			if !ok {
				return nil, fmt.Errorf("%s opts.allowedAlgs[%d] must be string", label, i)
			}
			out = append(out, s.Value)
		}
		return out, nil
	}
	return nil, nil
}

// jweOptsString pulls a generic string-typed opts field (used for
// JWE's "enc" parameter and similar). Returns "" when the field is
// absent.
func jweOptsString(v runtime.Value, field, label string) (string, error) {
	dict, ok := v.(runtime.Dict)
	if !ok {
		return "", fmt.Errorf("%s opts must be a dict", label)
	}
	for _, dk := range dict.EntryKeys() {
		entry, _ := dict.GetEntry(dk)
		key, ok := entry.Key.(runtime.String)
		if !ok || key.Value != field {
			continue
		}
		s, ok := entry.Value.(runtime.String)
		if !ok {
			return "", fmt.Errorf("%s opts.%s must be a string", label, field)
		}
		return s.Value, nil
	}
	return "", nil
}

// jwePayloadBytes accepts a string or bytes payload for JWE
// encryption. Strings are interpreted as UTF-8; bytes pass through.
func jwePayloadBytes(v runtime.Value, label string) ([]byte, error) {
	switch p := v.(type) {
	case runtime.String:
		return []byte(p.Value), nil
	case runtime.Bytes:
		return p.Value, nil
	}
	return nil, fmt.Errorf("%s payload must be string or bytes", label)
}

// jweWrapKey produces the Content Encryption Key (CEK) and the
// encrypted-key segment of a JWE compact serialisation. For alg "dir"
// the supplied key IS the CEK and the encrypted-key segment is empty.
// For alg "RSA-OAEP" / "RSA-OAEP-256" a fresh 32-byte CEK is generated
// and wrapped with the supplied RSA public key.
func jweWrapKey(alg string, key runtime.Value, label string) (cek, encryptedKey []byte, err error) {
	switch alg {
	case "dir":
		cek, err = jwtKeyBytes(key, label)
		if err != nil {
			return nil, nil, err
		}
		if len(cek) != 32 {
			return nil, nil, fmt.Errorf("%s dir CEK must be 32 bytes for A256GCM, got %d", label, len(cek))
		}
		return cek, nil, nil
	case "RSA-OAEP-256":
		cek = make([]byte, 32)
		if _, err := rand.Read(cek); err != nil {
			return nil, nil, fmt.Errorf("%s cek: %w", label, err)
		}
		pemStr, err := jwtKeyPEM(key, label)
		if err != nil {
			return nil, nil, err
		}
		pub, err := parsePublicKeyPEM(pemStr, label)
		if err != nil {
			return nil, nil, err
		}
		rsaPub, ok := pub.(*rsa.PublicKey)
		if !ok {
			return nil, nil, fmt.Errorf("%s %s requires an RSA public key", label, alg)
		}
		encryptedKey, err = rsa.EncryptOAEP(sha256.New(), rand.Reader, rsaPub, cek, nil)
		if err != nil {
			return nil, nil, fmt.Errorf("%s rsa-oaep: %w", label, err)
		}
		return cek, encryptedKey, nil
	}
	return nil, nil, fmt.Errorf("%s unsupported alg %q (supported: dir, RSA-OAEP-256)", label, alg)
}

// jweUnwrapKey is the verify-side companion of jweWrapKey.
func jweUnwrapKey(alg string, key runtime.Value, encryptedKey []byte, label string) ([]byte, error) {
	switch alg {
	case "dir":
		cek, err := jwtKeyBytes(key, label)
		if err != nil {
			return nil, err
		}
		if len(cek) != 32 {
			return nil, fmt.Errorf("%s dir CEK must be 32 bytes for A256GCM, got %d", label, len(cek))
		}
		if len(encryptedKey) != 0 {
			return nil, fmt.Errorf("%s dir tokens must have empty encryptedKey segment", label)
		}
		return cek, nil
	case "RSA-OAEP-256":
		pemStr, err := jwtKeyPEM(key, label)
		if err != nil {
			return nil, err
		}
		priv, err := parsePrivKeyPEM(pemStr, label)
		if err != nil {
			return nil, err
		}
		rsaPriv, ok := priv.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("%s %s requires an RSA private key", label, alg)
		}
		cek, err := rsa.DecryptOAEP(sha256.New(), rand.Reader, rsaPriv, encryptedKey, nil)
		if err != nil {
			return nil, fmt.Errorf("%s rsa-oaep: %w", label, err)
		}
		return cek, nil
	}
	return nil, fmt.Errorf("%s unsupported alg %q", label, alg)
}
