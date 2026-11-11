package native

import (
	"crypto/x509"
	"encoding/asn1"
	"encoding/pem"
	"fmt"
	"math/big"
	"time"

	"geblang/internal/runtime"
)

// androidAttestationOID identifies the Android Key Attestation extension.
const androidAttestationOID = "1.3.6.1.4.1.11129.2.1.17"

func registerCryptX509(r *Registry) {
	r.Register("crypt", "verifyCertChain", func(args []runtime.Value) (runtime.Value, error) {
		opts, ok := single(args).(runtime.Dict)
		if !ok {
			return nil, fmt.Errorf("crypt.verifyCertChain expects an options dict")
		}
		leafPEM := dictString(opts, "leaf")
		if leafPEM == "" {
			return nil, fmt.Errorf("crypt.verifyCertChain: leaf is required")
		}
		leaf, err := parseCertPEM(leafPEM, "leaf")
		if err != nil {
			return nil, fmt.Errorf("crypt.verifyCertChain: %w", err)
		}
		rootPEMs, err := dictPEMList(opts, "roots")
		if err != nil {
			return nil, fmt.Errorf("crypt.verifyCertChain: %w", err)
		}
		if len(rootPEMs) == 0 {
			return nil, fmt.Errorf("crypt.verifyCertChain: roots is required")
		}
		roots := x509.NewCertPool()
		for _, p := range rootPEMs {
			c, err := parseCertPEM(p, "root")
			if err != nil {
				return nil, fmt.Errorf("crypt.verifyCertChain: %w", err)
			}
			roots.AddCert(c)
		}
		interPEMs, err := dictPEMList(opts, "intermediates")
		if err != nil {
			return nil, fmt.Errorf("crypt.verifyCertChain: %w", err)
		}
		intermediates := x509.NewCertPool()
		for _, p := range interPEMs {
			c, err := parseCertPEM(p, "intermediate")
			if err != nil {
				return nil, fmt.Errorf("crypt.verifyCertChain: %w", err)
			}
			intermediates.AddCert(c)
		}
		// skipExpiry verifies as of the leaf's issuance so a past-notAfter chain still checks signatures and trust.
		verifyTime := time.Now()
		if dictBool(opts, "skipExpiry") {
			verifyTime = leaf.NotBefore
		} else if ts := dictString(opts, "time"); ts != "" {
			t, perr := time.Parse(time.RFC3339, ts)
			if perr != nil {
				return nil, fmt.Errorf("crypt.verifyCertChain: invalid time: %w", perr)
			}
			verifyTime = t
		}
		chains, err := leaf.Verify(x509.VerifyOptions{
			Roots:         roots,
			Intermediates: intermediates,
			CurrentTime:   verifyTime,
			KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
		})
		if err != nil {
			return nil, fmt.Errorf("crypt.verifyCertChain: %w", err)
		}
		elems := make([]runtime.Value, 0, len(chains[0]))
		for _, c := range chains[0] {
			elems = append(elems, pkixNameToDict(c.Subject))
		}
		return &runtime.List{Elements: elems}, nil
	})

	r.Register("crypt", "asn1Decode", func(args []runtime.Value) (runtime.Value, error) {
		b, ok := single(args).(runtime.Bytes)
		if !ok {
			return nil, fmt.Errorf("crypt.asn1Decode expects bytes")
		}
		var raw asn1.RawValue
		if _, err := asn1.Unmarshal(b.Value, &raw); err != nil {
			return nil, fmt.Errorf("crypt.asn1Decode: %w", err)
		}
		return decodeASN1Value(raw)
	})

	r.Register("crypt", "parseAndroidAttestation", func(args []runtime.Value) (runtime.Value, error) {
		pemStr, err := singleString(args, "crypt.parseAndroidAttestation")
		if err != nil {
			return nil, err
		}
		cert, err := parseCertPEM(pemStr, "crypt.parseAndroidAttestation")
		if err != nil {
			return nil, err
		}
		var extVal []byte
		for _, ext := range cert.Extensions {
			if ext.Id.String() == androidAttestationOID {
				extVal = ext.Value
			}
		}
		if extVal == nil {
			return nil, fmt.Errorf("crypt.parseAndroidAttestation: certificate has no key attestation extension")
		}
		var kd keyDescription
		if _, err := asn1.Unmarshal(extVal, &kd); err != nil {
			return nil, fmt.Errorf("crypt.parseAndroidAttestation: %w", err)
		}
		entries := map[string]runtime.DictEntry{}
		put := func(k string, v runtime.Value) {
			key := runtime.String{Value: k}
			entries[DictKey(key)] = runtime.DictEntry{Key: key, Value: v}
		}
		put("attestationVersion", runtime.NewInt64(int64(kd.AttestationVersion)))
		put("keymasterVersion", runtime.NewInt64(int64(kd.KeymasterVersion)))
		put("attestationSecurityLevel", runtime.String{Value: securityLevelName(int(kd.AttestationSecurityLevel))})
		put("keymasterSecurityLevel", runtime.String{Value: securityLevelName(int(kd.KeymasterSecurityLevel))})
		put("attestationChallenge", runtime.Bytes{Value: append([]byte(nil), kd.AttestationChallenge...)})
		put("uniqueId", runtime.Bytes{Value: append([]byte(nil), kd.UniqueID...)})
		// origin (tag 702) lives in the TEE list, falling back to the software list.
		if origin, ok := authzListInt(kd.TeeEnforced, 702); ok {
			put("keyOrigin", runtime.NewInt64(origin.Int64()))
		} else if origin, ok := authzListInt(kd.SoftwareEnforced, 702); ok {
			put("keyOrigin", runtime.NewInt64(origin.Int64()))
		}
		return runtime.DictFromEntries(entries, nil), nil
	})
}

type keyDescription struct {
	AttestationVersion       int
	AttestationSecurityLevel asn1.Enumerated
	KeymasterVersion         int
	KeymasterSecurityLevel   asn1.Enumerated
	AttestationChallenge     []byte
	UniqueID                 []byte
	SoftwareEnforced         asn1.RawValue
	TeeEnforced              asn1.RawValue
}

func securityLevelName(level int) string {
	switch level {
	case 0:
		return "Software"
	case 1:
		return "TrustedEnvironment"
	case 2:
		return "StrongBox"
	}
	return fmt.Sprintf("Unknown(%d)", level)
}

// authzListInt finds the context-tagged INTEGER field (e.g. origin = [702]) in an AuthorizationList.
func authzListInt(authz asn1.RawValue, tag int) (*big.Int, bool) {
	rest := authz.Bytes
	for len(rest) > 0 {
		var el asn1.RawValue
		var err error
		rest, err = asn1.Unmarshal(rest, &el)
		if err != nil {
			return nil, false
		}
		if el.Class == asn1.ClassContextSpecific && el.Tag == tag {
			var bi *big.Int
			if _, err := asn1.Unmarshal(el.Bytes, &bi); err == nil {
				return bi, true
			}
		}
	}
	return nil, false
}

func parseCertPEM(pemStr, label string) (*x509.Certificate, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, fmt.Errorf("%s must be a CERTIFICATE PEM", label)
	}
	return x509.ParseCertificate(block.Bytes)
}

func dictPEMList(opts runtime.Dict, key string) ([]string, error) {
	entry, ok := opts.GetEntry(DictKey(runtime.String{Value: key}))
	if !ok {
		return nil, nil
	}
	if s, ok := entry.Value.(runtime.String); ok {
		return []string{s.Value}, nil
	}
	list, ok := entry.Value.(*runtime.List)
	if !ok {
		return nil, fmt.Errorf("%s must be a PEM string or list of PEM strings", key)
	}
	out := make([]string, 0, len(list.Elements))
	for _, el := range list.Elements {
		s, ok := el.(runtime.String)
		if !ok {
			return nil, fmt.Errorf("%s must contain PEM strings", key)
		}
		out = append(out, s.Value)
	}
	return out, nil
}

func decodeASN1Sequence(data []byte) ([]runtime.Value, error) {
	out := []runtime.Value{}
	rest := data
	for len(rest) > 0 {
		var raw asn1.RawValue
		var err error
		rest, err = asn1.Unmarshal(rest, &raw)
		if err != nil {
			return nil, err
		}
		v, err := decodeASN1Value(raw)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func decodeASN1Value(raw asn1.RawValue) (runtime.Value, error) {
	if raw.Class != asn1.ClassUniversal {
		entries := map[string]runtime.DictEntry{}
		put := func(k string, v runtime.Value) {
			key := runtime.String{Value: k}
			entries[DictKey(key)] = runtime.DictEntry{Key: key, Value: v}
		}
		put("tag", runtime.NewInt64(int64(raw.Tag)))
		put("constructed", runtime.Bool{Value: raw.IsCompound})
		if raw.IsCompound {
			children, err := decodeASN1Sequence(raw.Bytes)
			if err != nil {
				return nil, err
			}
			put("value", &runtime.List{Elements: children})
		} else {
			put("value", runtime.Bytes{Value: append([]byte(nil), raw.Bytes...)})
		}
		return runtime.DictFromEntries(entries, nil), nil
	}
	switch raw.Tag {
	case asn1.TagInteger, asn1.TagEnum:
		var bi *big.Int
		if _, err := asn1.Unmarshal(raw.FullBytes, &bi); err == nil {
			return bigIntValue(bi), nil
		}
	case asn1.TagBoolean:
		return runtime.Bool{Value: len(raw.Bytes) > 0 && raw.Bytes[0] != 0}, nil
	case asn1.TagOID:
		var oid asn1.ObjectIdentifier
		if _, err := asn1.Unmarshal(raw.FullBytes, &oid); err == nil {
			return runtime.String{Value: oid.String()}, nil
		}
	case asn1.TagUTF8String, asn1.TagPrintableString, asn1.TagIA5String, asn1.TagT61String, asn1.TagGeneralString:
		return runtime.String{Value: string(raw.Bytes)}, nil
	case asn1.TagUTCTime, asn1.TagGeneralizedTime:
		return runtime.String{Value: string(raw.Bytes)}, nil
	case asn1.TagNull:
		return runtime.Null{}, nil
	case asn1.TagSequence, asn1.TagSet:
		children, err := decodeASN1Sequence(raw.Bytes)
		if err != nil {
			return nil, err
		}
		return &runtime.List{Elements: children}, nil
	}
	return runtime.Bytes{Value: append([]byte(nil), raw.Bytes...)}, nil
}

func bigIntValue(bi *big.Int) runtime.Value {
	if bi.IsInt64() {
		return runtime.SmallInt{Value: bi.Int64()}
	}
	return runtime.Int{Value: bi}
}

func single(args []runtime.Value) runtime.Value {
	if len(args) != 1 {
		return nil
	}
	return args[0]
}
