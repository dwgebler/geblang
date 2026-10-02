package native

import (
	"fmt"

	"geblang/internal/runtime"
)

type Function func(args []runtime.Value) (runtime.Value, error)

type Registry struct {
	functions map[string]Function
	// patches overlays functions during a test (test.mock); CallKey checks it first, cleared between @test methods.
	patches map[string]Function
	// convCtx is this backend's serialize/deserialize context for the conversion natives; set once at construction, read per call (no process-global).
	convCtx ConversionContext
}

// SetConversionContext installs this backend's serialize/deserialize callbacks for the conversion natives.
func (r *Registry) SetConversionContext(ctx ConversionContext) { r.convCtx = ctx }

func NewRegistry() *Registry {
	return &Registry{functions: map[string]Function{}, patches: map[string]Function{}}
}

func Key(module, name string) string {
	return module + "." + name
}

func (r *Registry) Register(module, name string, fn Function) {
	r.functions[Key(module, name)] = fn
}

func (r *Registry) Call(module, name string, args []runtime.Value) (runtime.Value, error) {
	if patch, ok := r.patches[Key(module, name)]; ok {
		return patch(args)
	}
	fn, ok := r.functions[Key(module, name)]
	if !ok {
		return nil, fmt.Errorf("unsupported native call %s.%s", module, name)
	}
	return fn(args)
}

func (r *Registry) Has(module, name string) bool {
	_, ok := r.functions[Key(module, name)]
	return ok
}

// Keys returns the "module.name" key of every registered function.
// Used by guard tests to verify the registry stays a subset of the
// engine's known module surface (so the VM fast path cannot recognise a
// builtin the analyzer and dir do not).
func (r *Registry) Keys() []string {
	keys := make([]string, 0, len(r.functions))
	for k := range r.functions {
		keys = append(keys, k)
	}
	return keys
}

// Returns nil when a test patch shadows the key; the caller must
// fall back to Call/CallKey so patches still win.
func (r *Registry) LookupKey(key string) Function {
	if _, patched := r.patches[key]; patched {
		return nil
	}
	// Conversion natives carry this backend's context; the cached wrapper reads r.convCtx at call time, so the VM fast path stays per-backend.
	if isConversionKey(key) {
		return func(args []runtime.Value) (runtime.Value, error) { return callConversion(r.convCtx, key, args) }
	}
	return r.functions[key]
}

func (r *Registry) CallKey(key string, args []runtime.Value) (runtime.Value, error) {
	if patch, ok := r.patches[key]; ok {
		return patch(args)
	}
	if isConversionKey(key) {
		return callConversion(r.convCtx, key, args)
	}
	fn, ok := r.functions[key]
	if !ok {
		return nil, fmt.Errorf("unsupported native call %s", key)
	}
	return fn(args)
}

// Patch installs an override that shadows the registered function
// for the given module.name key. Used by test.mock; the engine
// pairs it with Snapshot/Restore so patches roll back at @test
// method boundaries.
func (r *Registry) Patch(module, name string, fn Function) {
	r.patches[Key(module, name)] = fn
}

// Unpatch removes a single override, falling back to the
// originally-registered function for subsequent calls.
func (r *Registry) Unpatch(module, name string) {
	delete(r.patches, Key(module, name))
}

// Snapshot returns a copy of the current patch map. The engine
// captures one snapshot before each @test method and Restore()s
// it after, so tests don't leak mocks into the next case.
func (r *Registry) Snapshot() map[string]Function {
	out := make(map[string]Function, len(r.patches))
	for k, v := range r.patches {
		out[k] = v
	}
	return out
}

// Restore replaces the active patch map with the given snapshot.
// Pass an empty / nil map to clear all patches.
func (r *Registry) Restore(snapshot map[string]Function) {
	if snapshot == nil {
		r.patches = map[string]Function{}
		return
	}
	r.patches = make(map[string]Function, len(snapshot))
	for k, v := range snapshot {
		r.patches[k] = v
	}
}

func IsPureBuiltin(module, name string) bool {
	functions, ok := pureBuiltins[module]
	if !ok {
		return false
	}
	_, ok = functions[name]
	return ok
}

func IsPureBuiltinModule(module string) bool {
	_, ok := pureBuiltins[module]
	return ok
}

var pureBuiltins = map[string]map[string]struct{}{
	"math": {
		"abs": {}, "min": {}, "max": {}, "clamp": {}, "lerp": {}, "remap": {}, "floor": {}, "ceil": {},
		"round": {}, "sqrt": {}, "sin": {}, "cos": {}, "tan": {}, "asin": {},
		"acos": {}, "atan": {}, "atan2": {}, "log": {}, "log10": {}, "exp": {},
		"pow": {}, "pi": {}, "e": {},
		"log2": {}, "trunc": {}, "sign": {}, "cbrt": {}, "hypot": {},
		"inf": {}, "nan": {}, "isNaN": {}, "isInf": {}, "isPrime": {},
		"median": {}, "percentile": {}, "quantile": {}, "mode": {}, "mean": {}, "sum": {},
		"tau": {}, "ln2": {}, "ln10": {}, "sqrt2": {}, "phi": {},
		"maxInt": {}, "minInt": {}, "maxFloat": {}, "minFloat": {},
		"epsilon": {}, "sqrt2Pi": {}, "log2Pi": {},
		"gamma": {}, "lgamma": {}, "beta": {}, "lbeta": {},
		"erf": {}, "erfc": {}, "erfinv": {},
		"j0": {}, "j1": {}, "jn": {}, "y0": {}, "y1": {}, "yn": {},
		"factorial": {}, "comb": {}, "perm": {}, "gcd": {}, "lcm": {}, "lcomb": {},
	},
	"vecmath": {
		"score": {}, "topK": {}, "normalize": {}, "semanticSearch": {},
	},
	"transformers": {
		"tokenize": {}, "pool": {},
	},
	"hnsw": {
		"new": {}, "add": {}, "get": {}, "delete": {}, "count": {}, "clear": {}, "search": {},
	},
	"secureRandom": {
		"openSession": {}, "fromSeed": {}, "commitment": {}, "reveal": {},
		"auditLog": {}, "auditLogJson": {},
		"bytes": {}, "uintRange": {}, "randomBytes": {}, "randomInt": {}, "float": {}, "bool": {},
		"choice": {}, "shuffle": {}, "weightedChoice": {},
		"verifyCommitment": {}, "replay": {},
	},
	"msgpack": {
		"encode": {}, "decode": {}, "tryDecode": {}, "validate": {},
	},
	"unicode": {
		"normalize": {}, "isNormalized": {},
	},
	"cron": {
		"parse": {}, "isValid": {}, "nextAfter": {}, "nextN": {},
	},
	"async.sync": {
		"mutexNew": {}, "mutexLock": {}, "mutexUnlock": {}, "mutexTryLock": {},
		"rwmutexNew": {}, "rwmutexLock": {}, "rwmutexUnlock": {}, "rwmutexTryLock": {},
		"rwmutexRLock": {}, "rwmutexRUnlock": {}, "rwmutexTryRLock": {},
		"semaphoreNew": {}, "semaphoreAcquire": {}, "semaphoreRelease": {}, "semaphoreTryAcquire": {},
		"waitgroupNew": {}, "waitgroupAdd": {}, "waitgroupDone": {}, "waitgroupWait": {},
	},
	"async.atomic": {
		"intNew": {}, "intLoad": {}, "intStore": {}, "intAdd": {}, "intCompareAndSwap": {},
		"boolNew": {}, "boolLoad": {}, "boolStore": {}, "boolCompareAndSwap": {},
	},
	"async.channel": {
		"make": {}, "send": {}, "recv": {}, "tryRecv": {}, "trySend": {},
		"close": {}, "isClosed": {},
	},
	"sys": {
		"hostname": {}, "pid": {}, "goroutineId": {}, "platform": {}, "arch": {},
		"tmpdir": {}, "homedir": {}, "username": {}, "environ": {}, "osVersion": {},
	},
	"json": {
		"parse": {}, "stringify": {}, "validate": {}, "tryParse": {},
		"validateDetailed": {}, "parseAs": {},
	},
	"xml": {
		"parse": {}, "tryParse": {}, "stringify": {}, "validate": {},
		"validateDetailed": {}, "parseAs": {},
	},
	"html": {
		"parse": {},
	},
	"toml": {
		"parse": {}, "tryParse": {}, "stringify": {}, "validate": {},
		"validateDetailed": {}, "parseAs": {},
	},
	"yaml": {
		"parse": {}, "tryParse": {}, "stringify": {}, "validate": {},
		"validateDetailed": {}, "parseAs": {},
	},
	"crypt": {
		"md5": {}, "sha1": {}, "sha256": {}, "sha512": {}, "sha3_256": {},
		"blake2b": {}, "crc32": {}, "hmacSha256": {}, "hmacSha256Bytes": {}, "randomHex": {},
		"bcryptHash": {}, "bcryptVerify": {}, "argon2idHash": {}, "argon2idVerify": {},
		"passwordHash": {}, "passwordVerify": {},
		"base64Encode": {}, "base64Decode": {},
		"jwtSign": {}, "jwtVerify": {}, "jwtDecode": {}, "jwk": {}, "jwks": {},
		"generateRsaKey": {}, "generateEcKey": {}, "generateEd25519Key": {},
		"publicKey": {}, "generateSelfSignedCert": {}, "generateCsr": {}, "parseCert": {}, "signCertificate": {}, "pkcs12Decode": {},
		"verifyCertChain": {}, "asn1Decode": {}, "parseAndroidAttestation": {},
		"jweEncrypt": {}, "jweDecrypt": {},
		"jwtSignRS256": {}, "jwtVerifyRS256": {}, "jwtSignES256": {}, "jwtVerifyES256": {},
		"aesEncrypt": {}, "aesDecrypt": {},
		"chacha20Encrypt": {}, "chacha20Decrypt": {},
	},
	"datetime": {
		"nowUnix": {}, "unix": {}, "parse": {}, "format": {}, "addSeconds": {},
		"addDays": {}, "addMonths": {}, "addYears": {}, "diff": {},
		"toLocal": {}, "toUtc": {}, "now": {},
		"nowInstant": {}, "Instant": {}, "Duration": {}, "Zone": {},
		"make": {}, "formatRFC3339": {}, "formatDate": {}, "formatTime": {},
		"formatHTTP": {}, "partsInZone": {},
		"parseRFC3339": {}, "weekdayName": {}, "monthName": {},
	},
	"random": {
		"seed": {}, "next": {}, "intRange": {}, "float": {}, "bool": {},
		"choice": {}, "shuffle": {}, "Generator": {},
	},
	"time": {
		"now": {}, "elapsed": {}, "sleep": {}, "monotonic": {}, "monotonicNs": {},
		"unix": {}, "unixMilli": {}, "unixMicro": {}, "unixNano": {},
		"unixFloat": {}, "unixDecimal": {}, "elapsedFloat": {}, "humanize": {},
	},
	"collections": {
		"length": {}, "isEmpty": {}, "contains": {}, "reverse": {}, "sort": {}, "join": {},
		"map": {}, "filter": {}, "reduce": {}, "find": {}, "any": {}, "all": {},
		"flatten": {}, "unique": {}, "zip": {}, "sorted": {},
		"groupBy": {}, "chunk": {}, "partition": {},
		"findLast": {}, "containsBy": {}, "indexBy": {},
		"binarySearch": {}, "lowerBound": {}, "upperBound": {},
		"minBy": {}, "maxBy": {}, "sortBy": {}, "topBy": {}, "sumBy": {}, "averageBy": {},
		"topK": {}, "bottomK": {}, "frequencies": {}, "mode": {},
		"difference": {}, "intersection": {}, "differenceBy": {}, "intersectionBy": {}, "zipWith": {},
		"flatMap": {}, "uniqueBy": {}, "takeWhile": {}, "dropWhile": {}, "windowed": {}, "unzip": {}, "scan": {},
		"enumerate": {},
		"range":     {}, "take": {}, "lazyMap": {}, "lazyFilter": {},
		"bfs": {}, "dfs": {}, "topologicalSort": {}, "shortestPath": {},
	},
	"profiler": {
		"snapshot": {}, "delta": {}, "memory": {}, "cpu": {}, "peak": {},
	},
	"secrets": {
		"randomBytes": {}, "randomInt": {}, "randomHex": {}, "randomBase64": {},
		"constantTimeEqual": {},
	},
	"bytes": {
		"fromString": {}, "toString": {}, "fromHex": {}, "toHex": {},
		"fromBase64": {}, "toBase64": {}, "fromBase64Url": {}, "toBase64Url": {},
		"fromList": {}, "concat": {},
	},
	"string": {
		"fromCodePoint": {}, "fromCodePoints": {},
		"compare": {}, "equalsFold": {},
	},
	"strbuilder": {
		"new": {}, "append": {}, "appendLine": {}, "build": {},
		"length": {}, "clear": {}, "dispose": {},
	},
	"csv": {
		"parse": {}, "parseDict": {}, "stringify": {},
	},
	"compress": {
		"gzip": {}, "gunzip": {},
	},
	"archive": {
		"zipRead": {}, "zipWrite": {},
		"tarRead": {}, "tarWrite": {},
		"tarGzRead": {}, "tarGzWrite": {},
	},
	"binary": {
		"pack": {}, "unpack": {}, "unpackNamed": {}, "size": {},
	},
	"encoding": {
		"base64Encode": {}, "base64Decode": {},
		"base32Encode": {}, "base32Decode": {},
		"base58Encode": {}, "base58Decode": {},
		"base64UrlEncode": {}, "base64UrlDecode": {},
		"urlEncode": {}, "urlDecode": {},
		"htmlEscape": {}, "htmlUnescape": {}, "sanitizeHtml": {},
	},
	"url": {
		"URL": {}, "parse": {}, "stringify": {}, "encode": {}, "decode": {}, "joinPath": {},
	},
	"template": {
		"renderString": {}, "Template": {}, "load": {}, "Engine": {},
	},
	"uuid": {
		"v1": {}, "v4": {}, "v7": {},
		"v3": {}, "v5": {},
		"parse": {}, "isValid": {}, "nil": {},
		"toBytes": {}, "fromBytes": {},
		"namespaceDNS": {}, "namespaceURL": {}, "namespaceOID": {}, "namespaceX500": {},
		"ulid": {},
	},
	"re": {
		"match": {}, "matchAll": {}, "find": {}, "findAll": {}, "replace": {}, "split": {}, "test": {}, "compile": {},
	},
	"pcre": {
		"match": {}, "matchAll": {}, "find": {}, "findAll": {}, "replace": {}, "split": {}, "test": {}, "quote": {}, "compile": {},
	},
	"markdown": {
		"parse": {}, "renderHtml": {}, "stripText": {},
	},
	"dataframe": {
		"fromDict": {}, "fromRecords": {}, "fromCsv": {}, "fromJson": {},
		"col": {}, "lit": {}, "concat": {},
	},
	"ndarray": {
		"array": {}, "zeros": {}, "ones": {}, "full": {}, "eye": {},
		"arange": {}, "linspace": {}, "random": {}, "randn": {},
		"solve": {}, "inv": {}, "det": {},
	},
	"stats": {
		"normal": {}, "uniform": {}, "exponential": {}, "lognormal": {}, "weibull": {},
		"gamma": {}, "chiSquared": {}, "beta": {}, "studentT": {}, "f": {},
		"binomial": {}, "poisson": {},
		"tTestOneSample": {}, "tTestIndependent": {}, "tTestPaired": {},
		"chiSquareTest": {}, "chiSquareIndependence": {},
		"mannWhitneyU": {}, "ksTest": {},
		"confidenceIntervalMean": {}, "confidenceIntervalProportion": {}, "confidenceIntervalDiffMeans": {},
		"linregress": {},
		"polyfit":    {}, "polyval": {},
		"skewness": {}, "kurtosis": {}, "covariance": {}, "corrcoef": {},
		"variance": {}, "stdev": {}, "geometricMean": {}, "harmonicMean": {}, "weightedMean": {},
		"range": {}, "iqr": {}, "mad": {}, "zscores": {}, "describe": {},
	},
	"physics": {
		"c": {}, "G": {}, "planck": {}, "hbar": {}, "avogadro": {}, "boltzmann": {},
		"gasConstant": {}, "elementaryCharge": {}, "electronMass": {}, "protonMass": {},
		"stefanBoltzmann": {}, "gravity": {}, "convert": {},
	},
	"complex": {
		"of": {}, "fromPolar": {},
	},
	"geo": {
		"haversineDistance": {}, "bearing": {}, "midpoint": {}, "destination": {},
	},
	"args": {
		"parse": {}, "help": {},
	},
	"errors": {
		"new": {}, "message": {}, "class": {}, "is": {}, "wrap": {},
		"stackTrace": {}, "frames": {}, "hasStackTrace": {},
	},
	"freeze": {
		"shallow": {}, "deep": {}, "isFrozen": {},
	},
	"clone": {
		"deep": {},
	},
}
