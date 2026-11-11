package evaluator

import (
	"crypto/tls"
	"testing"

	gruntime "geblang/internal/runtime"
)

func tlsOptsDict(tlsEntries map[string]gruntime.Value) gruntime.Dict {
	inner := map[string]gruntime.DictEntry{}
	for k, v := range tlsEntries {
		putDict(inner, k, v)
	}
	outer := map[string]gruntime.DictEntry{}
	putDict(outer, "tls", gruntime.DictFromEntries(inner, nil))
	return gruntime.DictFromEntries(outer, nil)
}

// TestBuildServerTLSAutoCert verifies the ACME autocert path produces a
// dynamic-certificate TLS config that advertises HTTP/2 (h2) and the
// TLS-ALPN-01 protocol. The live ACME exchange is not exercised here.
func TestBuildServerTLSAutoCert(t *testing.T) {
	opts := tlsOptsDict(map[string]gruntime.Value{
		"autoCert":         gruntime.String{Value: "example.com"},
		"autoCertCacheDir": gruntime.String{Value: t.TempDir()},
		"autoCertEmail":    gruntime.String{Value: "ops@example.com"},
	})
	cfg, certPEM, err := buildHTTPServerTLSConfig(opts, "127.0.0.1:0", "test")
	if err != nil {
		t.Fatalf("autocert config: %v", err)
	}
	if cfg == nil || cfg.GetCertificate == nil {
		t.Fatal("expected a dynamic GetCertificate from autocert")
	}
	if certPEM != nil {
		t.Errorf("autocert serves certs dynamically; certPEM should be nil")
	}
	var hasH2, hasALPN bool
	for _, p := range cfg.NextProtos {
		switch p {
		case "h2":
			hasH2 = true
		case "acme-tls/1":
			hasALPN = true
		}
	}
	if !hasH2 {
		t.Errorf("expected HTTP/2 (h2) in NextProtos, got %v", cfg.NextProtos)
	}
	if !hasALPN {
		t.Errorf("expected acme-tls/1 in NextProtos, got %v", cfg.NextProtos)
	}
}

// TestBuildServerTLSMutualAuth verifies clientCa + clientAuth wire up the
// client-certificate verification mode and CA pool.
func TestBuildServerTLSMutualAuth(t *testing.T) {
	_, caPEM, err := generateSelfSignedTLSCert([]string{"test-ca"})
	if err != nil {
		t.Fatalf("generate CA: %v", err)
	}
	for _, tc := range []struct {
		mode string
		want tls.ClientAuthType
	}{
		{"require", tls.RequireAndVerifyClientCert},
		{"optional", tls.VerifyClientCertIfGiven},
	} {
		opts := tlsOptsDict(map[string]gruntime.Value{
			"selfSigned": gruntime.Bool{Value: true},
			"clientCa":   gruntime.String{Value: string(caPEM)},
			"clientAuth": gruntime.String{Value: tc.mode},
		})
		cfg, _, err := buildHTTPServerTLSConfig(opts, "127.0.0.1:0", "test")
		if err != nil {
			t.Fatalf("mTLS config (%s): %v", tc.mode, err)
		}
		if cfg.ClientAuth != tc.want {
			t.Errorf("clientAuth %q: got ClientAuth %v, want %v", tc.mode, cfg.ClientAuth, tc.want)
		}
		if cfg.ClientCAs == nil {
			t.Errorf("clientAuth %q: expected ClientCAs pool to be set", tc.mode)
		}
	}

	// clientAuth without clientCa is an error.
	bad := tlsOptsDict(map[string]gruntime.Value{
		"selfSigned": gruntime.Bool{Value: true},
		"clientAuth": gruntime.String{Value: "require"},
	})
	if _, _, err := buildHTTPServerTLSConfig(bad, "127.0.0.1:0", "test"); err == nil {
		t.Error("expected error for clientAuth without clientCa")
	}
}
