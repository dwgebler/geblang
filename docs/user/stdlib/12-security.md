# Security

## Crypt

```gb
import crypt;
```

The `crypt` module provides hashing, message authentication, password hashing,
JWT generation and verification, and asymmetric key and certificate helpers.
All functions are pure Go - no external tools or system libraries are required.

### Hashes

All hash functions accept a `string` and return a lowercase hex-encoded `string`.

```gb
import crypt;

io.println(crypt.sha256("hello"));
# 2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824

io.println(crypt.sha512("hello"));
io.println(crypt.sha3_256("hello"));
io.println(crypt.blake2b("hello"));
io.println(crypt.sha1("hello"));    # legacy - avoid for security use
io.println(crypt.md5("hello"));     # legacy - avoid for security use
```

`crypt.crc32(text)` returns an `int` (not hex) - it is a checksum, not a
cryptographic hash:

```gb
io.println(crypt.crc32("hello"));   # 907060870
```

**Choosing a hash algorithm:**

| Algorithm | Output | Use when |
|---|---|---|
| `sha256` | 64 hex chars | General-purpose signatures, content addressing |
| `sha512` | 128 hex chars | Larger security margin needed |
| `sha3_256` | 64 hex chars | Post-SHA-2 hardening, NIST standard |
| `blake2b` | 64 hex chars | High-speed hashing, file integrity |
| `sha1` | 40 hex chars | Legacy compatibility only |
| `md5` | 32 hex chars | Legacy checksums only |
| `crc32` | int | Non-cryptographic checksums |

### HMAC

`crypt.hmacSha256(secret, message)` computes an HMAC using SHA-256. Both
arguments are strings; the result is a lowercase hex string:

```gb
let sig = crypt.hmacSha256("my-secret-key", "the message");
io.println(sig);
# e.g. 4b2c3d...

# Verify by recomputing and comparing with constant-time equality
let ok = secrets.constantTimeEqual(sig, crypt.hmacSha256("my-secret-key", "the message"));
io.println(ok);   # true
```

HMAC-SHA256 is the standard algorithm for webhook signature verification,
API request signing, and message integrity checks.

### Password hashing

Never store passwords as plain hashes. Use a dedicated password hashing
function that incorporates a salt and a cost factor.

#### PHP-compatible unified API

`crypt.passwordHash(password, opts?)` and `crypt.passwordVerify(password, hash)`
produce and accept hashes that round-trip with PHP's
`password_hash` / `password_verify`. The hash strings are also accepted by
Node `bcryptjs`, Python `passlib`, and any other PHC-string-compatible library.

```gb
/* Defaults: bcrypt at cost 10, $2y$ prefix (PHP's default). */
let hash = crypt.passwordHash("hunter2");
io.println(hash);                       /* $2y$10$... */

/* Verify a hash produced by PHP, geblang, or any other library: */
io.println(crypt.passwordVerify("hunter2", hash));   /* true */
```

`opts.algorithm` picks between `"bcrypt"` (default), `"argon2id"`, and
`"argon2i"`. `opts.cost` tunes bcrypt; `opts.memory` / `opts.time` /
`opts.parallelism` / `opts.keyLength` / `opts.saltLength` tune the argon2
variants (same shape as `crypt.argon2idHash`).

```gb
let bcrypt12 = crypt.passwordHash("hunter2", {"algorithm": "bcrypt", "cost": 12});
let argon    = crypt.passwordHash("hunter2", {"algorithm": "argon2id", "memory": 131072});
```

`passwordVerify` auto-detects the algorithm from the hash prefix
(`$2a$`, `$2b$`, `$2y$` for bcrypt; `$argon2id$`, `$argon2i$` for argon2)
and returns `false` for any unknown or malformed input rather than throwing.

#### Argon2id (preferred)

`crypt.argon2idHash(password)` hashes a password using Argon2id and returns a
self-contained encoded string (PHC format) that includes the salt and parameters:

```gb
let hash = crypt.argon2idHash("hunter2");
io.println(hash);
# $argon2id$v=19$m=65536,t=3,p=4$<salt>$<hash>
```

`crypt.argon2idVerify(password, hash)` verifies a password against the stored
hash. Returns `true` on match:

```gb
let ok = crypt.argon2idVerify("hunter2", hash);
io.println(ok);   # true
io.println(crypt.argon2idVerify("wrong", hash));   # false
```

Default parameters: `memory=65536 KiB`, `time=3`, `parallelism=4`,
`keyLength=32`, `saltLength=16`. These can be tuned via an options dict:

```gb
let hash = crypt.argon2idHash("hunter2", {
    "memory":      131072,   # KiB (128 MiB)
    "time":        4,        # iterations (1-32)
    "parallelism": 2,        # threads (1-255)
    "keyLength":   32,       # output bytes (16-1024)
    "saltLength":  32        # salt bytes (8-1024)
});
```

Increase `memory` and `time` to make brute-force attacks more expensive. A
good starting point for interactive logins is `memory=65536, time=3`; for
high-security offline storage, use `memory=256000, time=4` or higher.

#### Bcrypt

`crypt.bcryptHash(password)` hashes a password with bcrypt at cost 10 (the
default). `crypt.bcryptVerify(password, hash)` verifies it:

```gb
let hash = crypt.bcryptHash("hunter2");
io.println(crypt.bcryptVerify("hunter2", hash));   # true

# Custom cost (4-31; higher = slower)
let strongHash = crypt.bcryptHash("hunter2", 12);
```

Bcrypt is well-supported but limited to 72-byte passwords and has lower
memory-hardness than Argon2id. Prefer Argon2id for new code. Note that
`crypt.bcryptHash` emits Go's `$2a$` prefix; use `crypt.passwordHash`
(above) when you need PHP-style `$2y$` output for cross-language interop.

### JWT - unified sign and verify

`crypt.jwtSign(payload, key, opts?)` and `crypt.jwtVerify(token, key, opts?)`
handle every supported algorithm through a single pair. The third `opts`
argument is optional; when omitted, signing defaults to HS256 and verify
trusts whatever algorithm the token header claims.

Supported algorithms: `HS256`, `HS384`, `HS512`, `RS256`, `RS384`, `RS512`,
`ES256`, `ES384`, `ES512`, `EdDSA` (Ed25519), plus `none` (only when
explicitly opted in - see "The `none` algorithm" below). The default
sign and verify policy never accepts `none` unless the caller passes
it inside `opts.allowedAlgs`.

The key shape depends on the algorithm:

| Algorithm family | Key for sign | Key for verify |
|------------------|--------------|----------------|
| HS256 / HS384 / HS512 | shared secret (string or bytes) | same shared secret |
| RS256 / RS384 / RS512 | RSA private key PEM | RSA public key PEM (or certificate PEM) |
| ES256 / ES384 / ES512 | EC private key PEM (P-256 / P-384 / P-521 respectively) | EC public key PEM (or certificate PEM) |
| EdDSA | Ed25519 private key PEM | Ed25519 public key PEM |

#### Symmetric example (HS256)

```gb
let token = crypt.jwtSign({
    "sub":  "user-42",
    "role": "admin",
    "exp":  datetime.nowUnix() + 3600
}, "my-signing-secret");

let payload = crypt.jwtVerify(token, "my-signing-secret");
if (payload != null) {
    io.println(payload["sub"]);   # user-42
}
```

#### Asymmetric example (RS256 or ES256)

```gb
import crypt;

let priv = crypt.generateEcKey("P-256");
let pub  = crypt.publicKey(priv);

let token = crypt.jwtSign({"sub": "user-42"}, priv, {"alg": "ES256"});
let claims = crypt.jwtVerify(token, pub);
```

#### Algorithm-confusion defence

When `opts.allowedAlgs` is not given, the verify policy is pinned to
the key type: a raw secret verifies only the HS family, an RSA public
key only RS256/384/512, an EC public key only ES256/384/512, and an
Ed25519 key only EdDSA (`none` is always excluded). A token whose
header names an algorithm outside the key's family fails verification,
which closes the classic forgery where an HS token is minted using a
verifier's public PEM text as the HMAC secret. To narrow the policy
further (for example to a single algorithm), pass `opts.allowedAlgs`
and the dispatcher rejects anything outside the allow-list before
loading the key:

```gb
let claims = crypt.jwtVerify(token, pubPem, {"allowedAlgs": ["ES256"]});
```

The same `allowedAlgs` field is enforced on the sign side, so a
shared "this service only uses ES256" constant can flow through both
calls and a typo on either side raises immediately instead of
producing tokens the matching verifier rejects.

#### JWK and JWKS

`crypt.jwk(pem, opts)` builds the RFC 7517 public JWK for a public,
certificate, or private PEM (private keys contribute their public
half). `kid` defaults to the RFC 7638 thumbprint; `alg` defaults per
key type (RS256 / ES256-512 by curve / EdDSA); `use` defaults to
`"sig"`. `crypt.jwks(keys)` assembles the `{"keys": [...]}` document
from PEM strings, `{pem, kid, alg}` dicts, or ready JWK dicts:

```gb
let set = crypt.jwks([
    {"pem": crypt.publicKey(currentKey), "kid": "2026-06"},
    {"pem": crypt.publicKey(previousKey), "kid": "2026-01"},
]);
```

`crypt.jwtVerify` accepts a JWKS (or single JWK) dict as its key:
the verification key is selected by the token header's `kid` (a
kid-less token verifies only against a single-key set), and the
algorithm is pinned to the matched key's `alg` (or its key-type
family), so key rotation and alg pinning come together:

```gb
let token = crypt.jwtSign(payload, currentKey, {"alg": "RS256", "kid": "2026-06"});
let claims = crypt.jwtVerify(token, set);
```

`crypt.jwtSign` writes `opts.kid` into the token header for this
purpose. Symmetric `oct` JWKs (`k` member) verify the HS family.

`jwtVerify` returns `null` for any invalid token - bad format, disallowed
algorithm, wrong key, or corrupted signature.

#### The `none` algorithm

A JWT signed with `alg: "none"` carries no signature and exists in the
spec only for unsigned tokens. Accepting one is equivalent to trusting
arbitrary input, so the default policy on both sides rejects it:

```gb
crypt.jwtSign({"u": "x"}, "shh", {"alg": "none"});
# throws: alg "none" rejected by default; pass opts.allowedAlgs
# containing "none" to opt in
```

If you genuinely need unsigned tokens (test fixtures, signed-elsewhere
formats), opt in explicitly:

```gb
let unsigned = crypt.jwtSign(claims, "", {
    "alg":         "none",
    "allowedAlgs": ["none"]
});

let payload = crypt.jwtVerify(unsigned, "", {"allowedAlgs": ["none"]});
```

A verifier that should accept both signed and unsigned tokens lists
each algorithm it tolerates:

```gb
crypt.jwtVerify(token, secret, {"allowedAlgs": ["HS256", "none"]});
```

Expiry checking is not automatic; verify `exp` yourself:

```gb
let payload = crypt.jwtVerify(token, secret, {"allowedAlgs": ["HS256"]});
if (payload == null) {
    # reject
} else if (payload["exp"] < datetime.nowUnix()) {
    # expired
} else {
    # valid
}
```

`crypt.jwtDecode(token)` decodes a JWT without verifying the signature. Use
only for debugging or for inspecting the header:

```gb
let parts = crypt.jwtDecode(token);
io.println(parts["header"]);    # {"alg": "HS256", "typ": "JWT"}
io.println(parts["payload"]);   # {"sub": "user-42", ...}
```

Never trust the payload from `jwtDecode` for access control - always use
`jwtVerify` instead.

#### Deprecated per-algorithm helpers

The earlier per-algorithm API remains as a shim. Prefer the unified surface:

| Deprecated | Replacement |
|------------|-------------|
| `crypt.jwtSignRS256(payload, priv)` | `crypt.jwtSign(payload, priv, {"alg": "RS256"})` |
| `crypt.jwtVerifyRS256(token, pub)` | `crypt.jwtVerify(token, pub, {"allowedAlgs": ["RS256"]})` |
| `crypt.jwtSignES256(payload, priv)` | `crypt.jwtSign(payload, priv, {"alg": "ES256"})` |
| `crypt.jwtVerifyES256(token, pub)` | `crypt.jwtVerify(token, pub, {"allowedAlgs": ["ES256"]})` |

### Key generation

#### RSA keys

`crypt.generateRsaKey(bits)` generates an RSA private key and returns it as a
PKCS#8 PEM string. The default bit size is 2048; valid range is 1024-8192:

```gb
let privPem = crypt.generateRsaKey();      # 2048-bit
let privPem = crypt.generateRsaKey(4096);  # 4096-bit
```

#### EC keys

`crypt.generateEcKey(curve)` generates an ECDSA private key. The default curve
is `"P-256"`; valid values are `"P-256"`, `"P-384"`, and `"P-521"`:

```gb
let privPem = crypt.generateEcKey();         # P-256
let privPem = crypt.generateEcKey("P-384");  # stronger curve
```

#### Ed25519 keys

`crypt.generateEd25519Key()` generates an Ed25519 private key:

```gb
let privPem = crypt.generateEd25519Key();
```

#### Extracting a public key

`crypt.publicKey(privatePem)` extracts the corresponding public key from any
supported private key type and returns it as a PKCS#8 PEM string:

```gb
let pubPem = crypt.publicKey(privPem);
io.println(pubPem);   # -----BEGIN PUBLIC KEY-----...
```

### Certificates

#### Self-signed certificates

`crypt.generateSelfSignedCert(options)` generates a self-signed X.509
certificate and returns a dict with `"cert"` and `"key"` PEM strings:

```gb
let result = crypt.generateSelfSignedCert({
    "subject": {
        "commonName":   "localhost",
        "organization": "Acme Inc",
        "country":      "GB"
    },
    "dnsNames":    ["localhost", "api.example.com"],
    "ipAddresses": ["127.0.0.1"],
    "validDays":   365,
    "keyType":     "EC-P256"   # RSA2048, RSA4096, EC-P256, EC-P384, EC-P521, Ed25519
});

io.println(result["cert"]);   # -----BEGIN CERTIFICATE-----...
io.println(result["key"]);    # -----BEGIN PRIVATE KEY-----...
```

Pass an existing `"key"` PEM to use a pre-generated key instead of generating
one:

```gb
let key  = crypt.generateEcKey("P-256");
let cert = crypt.generateSelfSignedCert({
    "subject": {"commonName": "localhost"},
    "key":     key
});
```

#### Certificate signing requests (CSR)

`crypt.generateCsr(options)` creates a PKCS#10 CSR for submission to a CA.
The `"key"` option is required:

```gb
let key = crypt.generateEcKey("P-256");
let csr = crypt.generateCsr({
    "key": key,
    "subject": {
        "commonName":   "api.example.com",
        "organization": "Acme Inc",
        "country":      "GB",
        "state":        "London",
        "locality":     "London"
    },
    "dnsNames": ["api.example.com", "www.example.com"]
});
io.println(csr);   # -----BEGIN CERTIFICATE REQUEST-----...
```

#### Signing a CSR with a CA

`crypt.signCertificate(options)` takes a CSR, a CA certificate, and the CA's
private key, and returns the issued certificate PEM. The CSR's subject,
DNS names, IP addresses, email addresses, and URIs are copied across; the
issuer is the CA's subject.

```gb
let caKey    = crypt.generateEcKey("P-256");
let caBundle = crypt.generateSelfSignedCert({
    "subject": {"commonName": "Acme Root CA"},
    "key":     caKey
});

let leafKey = crypt.generateEcKey("P-256");
let csr = crypt.generateCsr({
    "key":      leafKey,
    "subject":  {"commonName": "api.example.com"},
    "dnsNames": ["api.example.com"]
});
let leafPem = crypt.signCertificate({
    "csr":       csr,
    "caCert":    caBundle["cert"],
    "caKey":     caKey,
    "validDays": 90
});
```

`isCA: true` issues an intermediate CA certificate (sets `BasicConstraints
isCA=true` and adds `KeyUsageCertSign`). The default is a leaf certificate
suitable for server / client authentication.

#### Parsing certificates

`crypt.parseCert(pem)` decodes an X.509 certificate PEM and returns a dict:

```gb
let info = crypt.parseCert(certPem);

io.println(info["subject"]);       # dict: commonName, organization, etc.
io.println(info["issuer"]);        # dict: same fields
io.println(info["dnsNames"]);      # list<string>
io.println(info["ipAddresses"]);   # list<string>
io.println(info["notBefore"]);     # RFC3339 string
io.println(info["notAfter"]);      # RFC3339 string
io.println(info["serialNumber"]);  # hex string
io.println(info["keyType"]);       # "RSA", "EC", or "Ed25519"
io.println(info["isCA"]);          # bool
io.println(info["publicKey"]);     # SPKI public-key PEM
io.println(info["extensions"]);    # list of {oid, critical, value (bytes)}
```

Validate a certificate's expiry:

```gb
let info = crypt.parseCert(certPem);
let expiry = datetime.parseRFC3339(info["notAfter"]);
if (expiry < datetime.nowUnix()) {
    io.println("certificate has expired");
}
```

#### Verifying a certificate chain

`crypt.verifyCertChain(options)` checks a chain's signatures up to a trusted
root and **throws** on any failure (untrusted anchor, broken signature, or
expiry), so a verified result cannot be silently ignored. Options: `leaf`
(PEM), `roots` (list of trusted-anchor PEM, required), `intermediates` (list of
PEM), `time` (RFC3339, default now), and `skipExpiry` (verify as of the leaf's
issuance). On success it returns the validated chain, leaf to root, as subject
dicts. There is no hostname check; these are not assumed to be TLS certificates.

```gb
let chain = crypt.verifyCertChain({
    "leaf":          leafPem,
    "intermediates": [intermediatePem],
    "roots":         [trustedRootPem],
});
io.println(chain[0]["commonName"]);   # the leaf's subject
```

#### Decoding ASN.1 and Android Key Attestation

`crypt.asn1Decode(der)` walks a DER-encoded value into a nested structure:
sequences and sets become lists, integers become ints, octet strings and bit
strings become bytes, OIDs become dotted strings, and context-tagged values
become `{tag, constructed, value}` dicts. Read raw extensions from
`parseCert(...)["extensions"]` and decode the ones you need.

`crypt.parseAndroidAttestation(pem)` parses the Android Key Attestation
extension (OID `1.3.6.1.4.1.11129.2.1.17`) into `attestationVersion`,
`attestationSecurityLevel` / `keymasterSecurityLevel`
(`Software` / `TrustedEnvironment` / `StrongBox`), `attestationChallenge`,
`uniqueId`, and `keyOrigin`. Verify the certificate chain up to the Google
hardware-attestation root with `verifyCertChain` first; the parsed extension is
only trustworthy once the chain is verified.

#### Decoding PKCS#12 / PFX archives

`crypt.pkcs12Decode(pfx, password)` decodes a PFX byte string and returns
a dict carrying the private key, the leaf certificate, and any
intermediate CA certificates. The password defaults to an empty string;
common server PFX bundles use a non-empty password.

```gb
let pfx     = io.readBytes("server.pfx");
let bundle  = crypt.pkcs12Decode(pfx, "changeit");

io.println(bundle["key"]);              # PKCS#8 PEM
io.println(bundle["cert"]);             # CERTIFICATE PEM (or null)
io.println(bundle["caCerts"].length);   # int - intermediates
```

Encoding to PFX is not currently supported; export from a CA tool
(`openssl pkcs12 -export ...`) and decode on the Geblang side.

### Encrypted JWT (JWE)

`crypt.jweEncrypt(payload, key, opts?)` and
`crypt.jweDecrypt(token, key)` round-trip a payload through a JWE compact
token (`header.encryptedKey.iv.ciphertext.tag`). The payload may be a
string or `bytes`; `jweDecrypt` always returns `bytes`.

Supported key wrap (`opts.alg`): `dir` and `RSA-OAEP-256`.
Supported content encryption (`opts.enc`): `A256GCM` only.

#### Direct mode (shared 32-byte key)

```gb
import crypt;
import bytes;

let cek    = bytes.fromHex(crypt.randomHex(32));   # 32-byte Content Encryption Key
let token  = crypt.jweEncrypt("payload data", cek, {
    "alg": "dir",
    "enc": "A256GCM"
});
let plain  = crypt.jweDecrypt(token, cek);
io.println(bytes.toString(plain));   # payload data
```

`dir` requires exactly a 32-byte CEK (AES-256 key size); any other length
is rejected on encrypt and decrypt.

#### RSA-OAEP-256 key wrap

```gb
let priv = crypt.generateRsaKey(2048);
let pub  = crypt.publicKey(priv);

let token = crypt.jweEncrypt("payload", pub, {
    "alg": "RSA-OAEP-256",
    "enc": "A256GCM"
});
let plain = crypt.jweDecrypt(token, priv);
```

The CEK is generated automatically per token and wrapped with the supplied
RSA public key. Decryption uses the matching private key.

A tampered token (modified ciphertext, IV, or tag) fails the AES-GCM
authentication check and `jweDecrypt` throws.

### Symmetric encryption

For protecting data at rest (session cookies, encrypted files, sensitive
config) Geblang exposes two authenticated AEAD ciphers. Both accept a 32-byte
key, generate a random nonce automatically, and produce a dict containing the
nonce and ciphertext.

#### AES-256-GCM

```gb
import crypt;
import bytes;
import secrets;

let key = secrets.randomBytes(32);            # 32-byte AES-256 key
let enc = crypt.aesEncrypt(key, "secret data");

# enc is {"nonce": bytes, "ciphertext": bytes}
let plaintext = crypt.aesDecrypt(key, enc["nonce"], enc["ciphertext"]);
io.println(plaintext.toString());             # secret data
```

Both calls accept an optional associated-data argument that is
authenticated but not encrypted (good for metadata that must not be
forged):

```gb
let aad = bytes.fromString("user-42");
let enc = crypt.aesEncrypt(key, "secret", aad);
let pt  = crypt.aesDecrypt(key, enc["nonce"], enc["ciphertext"], aad);
```

If the key, nonce, ciphertext, or associated data is altered between encrypt
and decrypt, `aesDecrypt` throws `RuntimeError: authentication failed` -
authenticity is checked alongside confidentiality, so callers do not need a
separate HMAC.

The key must be exactly 32 bytes (AES-256). Derive a key from a password with
Argon2id (and a stored salt) rather than passing the password directly. For
production secrets, prefer `secrets.randomBytes(32)` and store the key in a
dedicated secrets manager.

#### XChaCha20-Poly1305

XChaCha20-Poly1305 is the alternate modern AEAD. The 24-byte nonce can be
generated randomly without collision concerns even for very high message
volumes, which is convenient for stateless services.

```gb
let key = secrets.randomBytes(32);
let enc = crypt.chacha20Encrypt(key, "secret data");
let pt  = crypt.chacha20Decrypt(key, enc["nonce"], enc["ciphertext"]);
```

Use `aesEncrypt` when interoperating with other systems (AES-GCM is the
broader standard); use `chacha20Encrypt` when you need the larger nonce or
when the target platform lacks AES hardware acceleration.

### Miscellaneous encoding helpers

`crypt.base64Encode(text)` encodes a string to standard Base64.
`crypt.base64Decode(text)` decodes it back to a string. For binary-safe
encoding use the `bytes` module instead.

```gb
let encoded = crypt.base64Encode("hello world");
io.println(encoded);                       # aGVsbG8gd29ybGQ=
io.println(crypt.base64Decode(encoded));   # hello world
```

`crypt.randomHex(n)` generates `n` cryptographically random bytes and returns
them as a hex string of length `2n`. For secure random material, prefer
`secrets.randomHex` which is the canonical API:

```gb
let nonce = crypt.randomHex(16);   # 32-char hex string
```

---

## Secrets

```gb
import secrets;
```

The `secrets` module is the canonical place for reading secret material at
startup and generating cryptographically secure random values.

### Reading secrets

`secrets.requireEnv(name)` reads an environment variable and throws a runtime
error if it is not set. Use this for required secrets at application startup:

```gb
import secrets;

let dbUrl  = secrets.requireEnv("DATABASE_URL");
let apiKey = secrets.requireEnv("API_KEY");
```

`secrets.getEnv(name)` returns the value or `null` if the variable is not set:

```gb
let logLevel = secrets.getEnv("LOG_LEVEL") ?? "info";
```

`secrets.readFile(path)` reads a secret from a file and returns the content as
a string with trailing newlines stripped. Useful for Docker secrets and
Kubernetes secret mounts:

```gb
let cert = secrets.readFile("/run/secrets/tls.crt");
let key  = secrets.readFile("/run/secrets/tls.key");
```

Prefer `secrets.requireEnv` and `secrets.readFile` over `sys.getenv` when
accessing secrets - it signals intent clearly and makes secret access auditable.

### Secure random values

All `secrets.random*` functions read from the OS cryptographic random source
(`/dev/urandom` on Linux). They are safe for generating tokens, nonces, salts,
and session IDs.

`secrets.randomBytes(n)` returns `n` random bytes as a `bytes` value:

```gb
let salt = secrets.randomBytes(16);   # 16 random bytes
```

`secrets.randomHex(n)` returns `n` random bytes encoded as a lowercase hex
string of length `2n`:

```gb
let token    = secrets.randomHex(32);   # 64-char hex token
let csrfKey  = secrets.randomHex(16);   # 32-char key
```

`secrets.randomBase64(n)` returns `n` random bytes encoded as URL-safe Base64
(no padding):

```gb
let sessionId = secrets.randomBase64(32);   # URL-safe, ~43 chars
```

`secrets.randomInt(min, max)` returns a cryptographically random `int` in the
inclusive range `[min, max]`:

```gb
let otp = secrets.randomInt(100000, 999999);   # 6-digit OTP
let die = secrets.randomInt(1, 6);             # fair die roll
```

### `random` vs `secrets`: which one do I use?

Geblang ships two random number modules. Use the right one for the job:

| Purpose | Module | API |
|---------|--------|-----|
| Security tokens, session IDs, salts, OTPs, anything an attacker shouldn't predict | `secrets` | CSPRNG; reads from the OS entropy pool. Never seedable. |
| Simulation, sampling, shuffling, procedural generation, fuzz inputs, tests | `random` | Deterministic pseudo-random number generator. Seedable for reproducibility. |

`secrets.*` is the canonical security choice. `random.*` (documented in
[Utilities / random](20-utilities.md#random---deterministic-pseudo-random-number-generation))
is for everything else where reproducibility matters or cryptographic
guarantees do not.

### Constant-time comparison

`secrets.constantTimeEqual(a, b)` compares two strings or `bytes` values in
constant time, preventing timing-based side-channel attacks. Both arguments
must be the same type:

```gb
let submitted = request.headers["X-Webhook-Signature"];
let expected  = crypt.hmacSha256(secret, request.body);

if (secrets.constantTimeEqual(submitted, expected)) {
    # signature valid
}
```

Always use `constantTimeEqual` when comparing authentication tokens, HMAC
signatures, or any other security-sensitive value. Regular `==` comparison can
leak information about how many bytes matched.

### Complete examples

#### API key authentication middleware

```gb
import secrets;
import crypt;

const API_KEY = secrets.requireEnv("API_KEY");

func checkApiKey(string submitted): bool {
    return secrets.constantTimeEqual(submitted, API_KEY);
}
```

#### Session token generation and storage

```gb
import secrets;
import datetime;

func newSession(string userId): dict<string, string> {
    let token    = secrets.randomHex(32);
    let expireAt = datetime.nowUnix() + 86400;   # 24 hours
    # store {token: userId, expireAt: expireAt} in your session store
    return {"token": token, "expireAt": expireAt as string};
}
```

#### Webhook signature verification

```gb
import crypt;
import secrets;

const WEBHOOK_SECRET = secrets.requireEnv("WEBHOOK_SECRET");

func verifyWebhook(string body, string signature): bool {
    let expected = "sha256=" + crypt.hmacSha256(WEBHOOK_SECRET, body);
    return secrets.constantTimeEqual(signature, expected);
}
```

#### Password authentication flow

```gb
import crypt;

# On registration - store hash, not the password
func hashPassword(string password): string {
    return crypt.argon2idHash(password);
}

# On login
func checkPassword(string submitted, string stored): bool {
    return crypt.argon2idVerify(submitted, stored);
}
```

#### JWT authentication middleware (HS256)

```gb
import crypt;
import datetime;
import secrets;

const JWT_SECRET = secrets.requireEnv("JWT_SECRET");

func issueToken(string userId): string {
    return crypt.jwtSign({
        "sub": userId,
        "exp": datetime.nowUnix() + 3600
    }, JWT_SECRET);
}

func verifyToken(string token): ?string {
    let payload = crypt.jwtVerify(token, JWT_SECRET);
    if (payload == null) { return null; }
    if (payload["exp"] < datetime.nowUnix()) { return null; }
    return payload["sub"] as string;
}
```

#### TLS certificate for a local HTTPS server

```gb
import crypt;
import io;

let result = crypt.generateSelfSignedCert({
    "subject":     {"commonName": "localhost"},
    "dnsNames":    ["localhost"],
    "ipAddresses": ["127.0.0.1"],
    "validDays":   365
});

io.writeText("server.crt", result["cert"]);
io.writeText("server.key", result["key"]);
```

## Provably-fair RNG (`secureRandom`)

The `secureRandom` module (added in 1.6.0) provides a verifiable
random-number stream for use cases where an outcome has to be
auditable after the fact: gaming, lotteries, betting, public
draws, distributed leader elections, any place where "did the
house cheat?" is a real question.

It uses a commit / reveal scheme. The server generates a 32-byte
seed, publishes its SHA-256 commitment up front, draws values
from an HMAC-SHA-256 stream keyed by that seed, then reveals the
seed at the end. Anyone holding the commitment and the audit log
can independently re-derive every draw and check that the seed
matches its commitment.

For ordinary cryptographic tokens or session IDs, keep using
`secrets.*`. `secureRandom` is for the narrower case where the
caller has to prove fairness, not just unpredictability.

### Surface

| Function | Purpose |
|----------|---------|
| `secureRandom.openSession(opts = {})` | Opens a session with a fresh 32-byte server seed. `opts.clientSeed` (string) mixes a caller-supplied nonce into every draw. |
| `secureRandom.fromSeed(seedHex, clientSeed = "")` | Opens a session from a caller-supplied 64-char hex seed (useful for tests and deterministic replays). |
| `secureRandom.commitment(s)` | Returns `sha256(serverSeed)` as a 64-char hex string. Publish this before drawing. |
| `secureRandom.reveal(s)` | Returns the server seed (hex) and locks the session so no further draws are allowed. |
| `secureRandom.auditLog(s)` | Returns the per-draw records: `{nonce, method, args, output}` in draw order. |
| `secureRandom.auditLogJson(s)` | Returns the audit log + commitment + clientSeed (and serverSeed, once revealed) as a JSON envelope ready to publish. |
| `secureRandom.bytes(s, n)` | Draws `n` provably-fair random bytes. |
| `secureRandom.uintRange(s, lo, hi)` | Unbiased uniform integer in `[lo, hi)`. Uses rejection sampling. |
| `secureRandom.randomInt(min, max)` / `randomInt(s, min, max)` | Uniform integer in `[min, max]`, both ends included (1.35.2). Session optional. |
| `secureRandom.randomBytes(n)` / `randomBytes(s, n)` | `n` random bytes (1.35.2). Session optional. |
| `secureRandom.float(s)` | Uniform float in `[0, 1)`. |
| `secureRandom.bool(s)` | Fair coin flip. |
| `secureRandom.choice(s, items)` | Uniformly picks one element of `items`. |
| `secureRandom.shuffle(s, items)` | Fisher-Yates shuffled copy. |
| `secureRandom.weightedChoice(s, items, weights)` | Picks one element with probability proportional to its weight. |
| `secureRandom.verifyCommitment(commit, seedHex)` | True if `sha256(seedHex) == commit`. |
| `secureRandom.replay(seedHex, clientSeed, nonce, method, args)` | Reproduces a single draw outside a session; same inputs always yield the same output. |

### Quick draws: `randomInt` and `randomBytes`

`randomInt` and `randomBytes` are the simplest way in. Called on their
own they read the OS cryptographic random source, exactly like
`secrets.randomInt` and `secrets.randomBytes`:

```gb
import secureRandom;

let roll  = secureRandom.randomInt(1, 6);     # 1..6, both ends included
let token = secureRandom.randomBytes(16);     # 16 random bytes
```

Pass a session as the first argument and the same call becomes a
provably-fair draw: it is derived from the session seed, recorded in the
audit log, and can be re-derived with `replay`:

```gb
let s = secureRandom.openSession({"clientSeed": "player#42"});
let roll = secureRandom.randomInt(s, 1, 6);
let salt = secureRandom.randomBytes(s, 16);
```

A draw made without a session is unpredictable but leaves no audit
trail; use a session whenever the outcome has to be verifiable later.
With a session, `min` and `max` must fit in 64 bits; without one they
may be any size. `randomBytes(s, n)` is the same draw as `bytes(s, n)`
and is logged under the method name `bytes`; `randomInt(s, min, max)` is
logged as `randomInt`.

### Example: a provably-fair dice roll

```gb
import secureRandom;
import io;

# The "house" opens a session and publishes the commitment first.
let s = secureRandom.openSession({"clientSeed": "player#42"});
io.println("commitment: " + secureRandom.commitment(s));

# The house draws an outcome.
let outcome = secureRandom.uintRange(s, 1, 7);   # 1..6
io.println("you rolled: " + (outcome as string));

# At end of round (or end of day) the house reveals the seed.
let seed = secureRandom.reveal(s);
io.println("seed: " + seed);
io.println(secureRandom.auditLogJson(s));
```

### Verifying after the fact

A third party with just the commitment, the published seed, and
the audit log can re-derive every outcome:

```gb
import secureRandom;
import json;
import io;

let published = json.parse(io.readText("round-1234.json"));
if (!secureRandom.verifyCommitment(
        published["commitment"] as string,
        published["serverSeed"] as string)) {
    throw RuntimeError("server seed does not match published commitment");
}

for (draw in published["draws"]) {
    let expected = secureRandom.replay(
        published["serverSeed"] as string,
        published["clientSeed"] as string,
        draw["nonce"] as int,
        draw["method"] as string,
        draw["args"] as list<any>
    );
    if (draw["output"] != expected) {
        throw RuntimeError("draw " + (draw["nonce"] as string) + " does not replay");
    }
}
```

### Notes

- Per-draw randomness derives from
  `hmacSha256(serverSeed, json({clientSeed, nonce, method, args}))`,
  so identical inputs always produce identical outputs. The
  caller cannot influence the result without changing the input,
  and the server cannot retroactively change the result without
  invalidating the commitment.
- `uintRange` uses rejection sampling so the distribution stays
  unbiased even for ranges that are not powers of two.
- Once `reveal` has been called the session refuses further
  draws. This prevents accidentally leaking entropy from a
  revealed seed.
- For plain unpredictable randomness (session IDs, tokens, OTPs),
  use `secrets.*`. `secureRandom` is heavier and only worth the
  cost when the audit trail matters.
