package native

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"strconv"
	"sync"

	"geblang/internal/runtime"
)

// secureSession is a provably-fair RNG session bound to a server-side
// seed sourced from crypto/rand. Each draw is derived by HMAC-SHA-256
// over (clientSeed, nonce, method, argsJson). The session publishes a
// SHA-256(serverSeed) commitment up front and reveals the raw seed at
// the end so a third party can replay every draw.
type secureSession struct {
	mu         sync.Mutex
	serverSeed []byte
	clientSeed string
	nonce      int64
	commitment string
	log        []secureLogEntry
	revealed   bool
}

type secureLogEntry struct {
	Nonce  int64           `json:"nonce"`
	Method string          `json:"method"`
	Args   []runtime.Value `json:"-"`
	Output runtime.Value   `json:"-"`
}

var (
	secureSessionMu    sync.Mutex
	secureSessionStore = map[int64]*secureSession{}
	secureSessionNext  int64
)

func registerSecureRandom(r *Registry) {
	r.Register("secureRandom", "openSession", secureRandomOpenSession)
	r.Register("secureRandom", "fromSeed", secureRandomFromSeed)
	r.Register("secureRandom", "commitment", secureRandomCommitment)
	r.Register("secureRandom", "reveal", secureRandomReveal)
	r.Register("secureRandom", "auditLog", secureRandomAuditLog)
	r.Register("secureRandom", "auditLogJson", secureRandomAuditLogJson)
	r.Register("secureRandom", "bytes", sessionRandomBytes)
	r.Register("secureRandom", "uintRange", secureRandomUintRange)
	r.Register("secureRandom", "randomBytes", secureRandomRandomBytes)
	r.Register("secureRandom", "randomInt", secureRandomRandomInt)
	r.Register("secureRandom", "float", secureRandomFloat)
	r.Register("secureRandom", "bool", secureRandomBool)
	r.Register("secureRandom", "choice", secureRandomChoice)
	r.Register("secureRandom", "shuffle", secureRandomShuffle)
	r.Register("secureRandom", "weightedChoice", secureRandomWeightedChoice)
	r.Register("secureRandom", "verifyCommitment", secureRandomVerifyCommitment)
	r.Register("secureRandom", "replay", secureRandomReplay)
}

func secureRandomOpenSession(args []runtime.Value) (runtime.Value, error) {
	clientSeed := ""
	if len(args) >= 1 {
		if dict, ok := args[0].(runtime.Dict); ok {
			if v, ok := dict.GetEntry(DictKey(runtime.String{Value: "clientSeed"})); ok {
				if s, ok := v.Value.(runtime.String); ok {
					clientSeed = s.Value
				}
			}
		}
	}
	seed := make([]byte, 32)
	if _, err := rand.Read(seed); err != nil {
		return nil, fmt.Errorf("secureRandom.openSession: cannot read entropy: %w", err)
	}
	return registerSecureSession(seed, clientSeed)
}

func secureRandomFromSeed(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 || len(args) > 2 {
		return nil, fmt.Errorf("secureRandom.fromSeed expects (serverSeedHex, clientSeed?)")
	}
	seedStr, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("secureRandom.fromSeed: serverSeedHex must be string")
	}
	seed, err := hex.DecodeString(seedStr.Value)
	if err != nil {
		return nil, fmt.Errorf("secureRandom.fromSeed: %w", err)
	}
	clientSeed := ""
	if len(args) == 2 {
		if s, ok := args[1].(runtime.String); ok {
			clientSeed = s.Value
		}
	}
	return registerSecureSession(seed, clientSeed)
}

func registerSecureSession(seed []byte, clientSeed string) (runtime.Value, error) {
	hash := sha256.Sum256(seed)
	sess := &secureSession{
		serverSeed: seed,
		clientSeed: clientSeed,
		commitment: hex.EncodeToString(hash[:]),
	}
	secureSessionMu.Lock()
	secureSessionNext++
	id := secureSessionNext
	secureSessionStore[id] = sess
	secureSessionMu.Unlock()
	return runtime.NativeObject{Kind: "SecureRandomSession", ID: id}, nil
}

func lookupSecureSession(arg runtime.Value, name string) (*secureSession, error) {
	obj, ok := arg.(runtime.NativeObject)
	if !ok || obj.Kind != "SecureRandomSession" {
		return nil, fmt.Errorf("%s: first argument must be a SecureRandomSession handle", name)
	}
	secureSessionMu.Lock()
	sess, ok := secureSessionStore[obj.ID]
	secureSessionMu.Unlock()
	if !ok {
		return nil, fmt.Errorf("%s: unknown session handle", name)
	}
	return sess, nil
}

func secureRandomCommitment(args []runtime.Value) (runtime.Value, error) {
	sess, err := lookupSecureSession(firstArg(args), "secureRandom.commitment")
	if err != nil {
		return nil, err
	}
	return runtime.String{Value: sess.commitment}, nil
}

func secureRandomReveal(args []runtime.Value) (runtime.Value, error) {
	sess, err := lookupSecureSession(firstArg(args), "secureRandom.reveal")
	if err != nil {
		return nil, err
	}
	sess.mu.Lock()
	sess.revealed = true
	out := hex.EncodeToString(sess.serverSeed)
	sess.mu.Unlock()
	return runtime.String{Value: out}, nil
}

func secureRandomAuditLog(args []runtime.Value) (runtime.Value, error) {
	sess, err := lookupSecureSession(firstArg(args), "secureRandom.auditLog")
	if err != nil {
		return nil, err
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	out := make([]runtime.Value, 0, len(sess.log))
	for _, entry := range sess.log {
		d := runtime.NewDict()
		d.PutEntry(DictKey(runtime.String{Value: "nonce"}), runtime.DictEntry{Key: runtime.String{Value: "nonce"}, Value: runtime.SmallInt{Value: entry.Nonce}})
		d.PutEntry(DictKey(runtime.String{Value: "method"}), runtime.DictEntry{Key: runtime.String{Value: "method"}, Value: runtime.String{Value: entry.Method}})
		d.PutEntry(DictKey(runtime.String{Value: "args"}), runtime.DictEntry{Key: runtime.String{Value: "args"}, Value: &runtime.List{Elements: entry.Args}})
		d.PutEntry(DictKey(runtime.String{Value: "output"}), runtime.DictEntry{Key: runtime.String{Value: "output"}, Value: entry.Output})
		out = append(out, d)
	}
	return &runtime.List{Elements: out}, nil
}

func secureRandomAuditLogJson(args []runtime.Value) (runtime.Value, error) {
	sess, err := lookupSecureSession(firstArg(args), "secureRandom.auditLogJson")
	if err != nil {
		return nil, err
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	rows := make([]map[string]any, 0, len(sess.log))
	for _, entry := range sess.log {
		argsJSON, err := argsToJSON(entry.Args)
		if err != nil {
			return nil, err
		}
		outputJSON, err := valueToJSONInterface(entry.Output)
		if err != nil {
			return nil, err
		}
		rows = append(rows, map[string]any{
			"nonce":  entry.Nonce,
			"method": entry.Method,
			"args":   argsJSON,
			"output": outputJSON,
		})
	}
	envelope := map[string]any{
		"commitment": sess.commitment,
		"clientSeed": sess.clientSeed,
		"draws":      rows,
	}
	if sess.revealed {
		envelope["serverSeed"] = hex.EncodeToString(sess.serverSeed)
	}
	b, err := json.Marshal(envelope)
	if err != nil {
		return nil, err
	}
	return runtime.String{Value: string(b)}, nil
}

func argsToJSON(args []runtime.Value) ([]any, error) {
	out := make([]any, 0, len(args))
	for _, a := range args {
		v, err := valueToJSONInterface(a)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func valueToJSONInterface(v runtime.Value) (any, error) {
	return ValueToJSON(v)
}

func firstArg(args []runtime.Value) runtime.Value {
	if len(args) == 0 {
		return runtime.Null{}
	}
	return args[0]
}

// deriveBytes returns n raw bytes by HMAC-SHA-256(serverSeed, message)
// chunks, concatenated. For n <= 32 only one HMAC is computed; for
// larger n the message gets a chunk-counter suffix.
func deriveBytes(serverSeed []byte, message string, n int) []byte {
	out := make([]byte, 0, n)
	chunk := 0
	for len(out) < n {
		var msg []byte
		if chunk == 0 {
			msg = []byte(message)
		} else {
			msg = []byte(message + ":chunk:" + strconv.Itoa(chunk))
		}
		mac := hmac.New(sha256.New, serverSeed)
		mac.Write(msg)
		out = append(out, mac.Sum(nil)...)
		chunk++
	}
	return out[:n]
}

func makeMessage(clientSeed string, nonce int64, method string, args []runtime.Value) (string, error) {
	argsJSON, err := json.Marshal(struct {
		ClientSeed string `json:"clientSeed"`
		Nonce      int64  `json:"nonce"`
		Method     string `json:"method"`
		Args       []any  `json:"args"`
	}{
		ClientSeed: clientSeed,
		Nonce:      nonce,
		Method:     method,
		Args:       mustValuesToJSON(args),
	})
	if err != nil {
		return "", err
	}
	return string(argsJSON), nil
}

func mustValuesToJSON(args []runtime.Value) []any {
	out := make([]any, 0, len(args))
	for _, a := range args {
		v, err := ValueToJSON(a)
		if err != nil {
			v = nil
		}
		out = append(out, v)
	}
	return out
}

func sessionRandomBytes(args []runtime.Value) (runtime.Value, error) {
	sess, err := lookupSecureSession(firstArg(args), "secureRandom.bytes")
	if err != nil {
		return nil, err
	}
	rest := args[1:]
	if len(rest) != 1 {
		return nil, fmt.Errorf("secureRandom.bytes(session, n)")
	}
	n, ok := AsInt64(rest[0])
	if !ok || n < 0 || n > 1024*1024 {
		return nil, fmt.Errorf("secureRandom.bytes: n must be a non-negative int <= 1048576")
	}
	output, err := drawAndLog(sess, "bytes", rest, func(seed []byte, nonce int64) (runtime.Value, error) {
		msg, _ := makeMessage(sess.clientSeed, nonce, "bytes", rest)
		out := deriveBytes(seed, msg, int(n))
		return runtime.Bytes{Value: out}, nil
	})
	return output, err
}

func secureRandomUintRange(args []runtime.Value) (runtime.Value, error) {
	sess, err := lookupSecureSession(firstArg(args), "secureRandom.uintRange")
	if err != nil {
		return nil, err
	}
	rest := args[1:]
	if len(rest) != 2 {
		return nil, fmt.Errorf("secureRandom.uintRange(session, lo, hi)")
	}
	lo, ok1 := AsInt64(rest[0])
	hi, ok2 := AsInt64(rest[1])
	if !ok1 || !ok2 || hi <= lo {
		return nil, fmt.Errorf("secureRandom.uintRange: hi must be > lo and both must be ints")
	}
	return drawAndLog(sess, "uintRange", rest, func(seed []byte, nonce int64) (runtime.Value, error) {
		msg, _ := makeMessage(sess.clientSeed, nonce, "uintRange", rest)
		v := uniformUintInRange(seed, msg, uint64(hi-lo))
		return runtime.SmallInt{Value: int64(v) + lo}, nil
	})
}

func isSecureSession(v runtime.Value) bool {
	obj, ok := v.(runtime.NativeObject)
	return ok && obj.Kind == "SecureRandomSession"
}

func secureRandomRandomBytes(args []runtime.Value) (runtime.Value, error) {
	if len(args) > 0 && isSecureSession(args[0]) {
		return sessionRandomBytes(args)
	}
	data, err := secureRandomBytes(args, "secureRandom.randomBytes")
	if err != nil {
		return nil, err
	}
	return runtime.Bytes{Value: data}, nil
}

func secureRandomRandomInt(args []runtime.Value) (runtime.Value, error) {
	if len(args) == 0 || !isSecureSession(args[0]) {
		return cryptoRandomInt(args, "secureRandom.randomInt")
	}
	sess, err := lookupSecureSession(args[0], "secureRandom.randomInt")
	if err != nil {
		return nil, err
	}
	rest := args[1:]
	lo, hi, err := inclusiveBounds(rest, "secureRandom.randomInt")
	if err != nil {
		return nil, err
	}
	return drawAndLog(sess, "randomInt", rest, func(seed []byte, nonce int64) (runtime.Value, error) {
		msg, _ := makeMessage(sess.clientSeed, nonce, "randomInt", rest)
		return inclusiveIntDraw(seed, msg, lo, hi), nil
	})
}

func inclusiveBounds(args []runtime.Value, label string) (int64, int64, error) {
	if len(args) != 2 {
		return 0, 0, fmt.Errorf("%s expects min and max", label)
	}
	lo, ok1 := AsInt64(args[0])
	hi, ok2 := AsInt64(args[1])
	if !ok1 || !ok2 {
		return 0, 0, fmt.Errorf("%s: min and max must be 64-bit ints in a session draw", label)
	}
	if lo > hi {
		return 0, 0, fmt.Errorf("%s min must be <= max", label)
	}
	return lo, hi, nil
}

func inclusiveIntDraw(seed []byte, msg string, lo, hi int64) runtime.Value {
	span := uint64(hi) - uint64(lo) + 1
	// span wraps to 0 only for the full int64 range, where every 64-bit draw is valid.
	if span == 0 {
		return runtime.SmallInt{Value: int64(binary.BigEndian.Uint64(deriveBytes(seed, msg, 8)))}
	}
	return runtime.SmallInt{Value: int64(uint64(lo) + uniformUintInRange(seed, msg, span))}
}

func uniformUintInRange(seed []byte, baseMsg string, span uint64) uint64 {
	// Rejection sampling to avoid modulo bias. Truncate uint64 -> [0, span).
	maxAccepted := (^uint64(0) / span) * span
	chunk := 0
	for {
		msg := baseMsg
		if chunk > 0 {
			msg = baseMsg + ":resample:" + strconv.Itoa(chunk)
		}
		bytes := deriveBytes(seed, msg, 8)
		v := binary.BigEndian.Uint64(bytes)
		if v < maxAccepted {
			return v % span
		}
		chunk++
	}
}

func secureRandomFloat(args []runtime.Value) (runtime.Value, error) {
	sess, err := lookupSecureSession(firstArg(args), "secureRandom.float")
	if err != nil {
		return nil, err
	}
	rest := args[1:]
	if len(rest) != 0 {
		return nil, fmt.Errorf("secureRandom.float(session)")
	}
	return drawAndLog(sess, "float", rest, func(seed []byte, nonce int64) (runtime.Value, error) {
		msg, _ := makeMessage(sess.clientSeed, nonce, "float", rest)
		bytes := deriveBytes(seed, msg, 8)
		v := binary.BigEndian.Uint64(bytes) >> 11
		f := float64(v) / float64(1<<53)
		return runtime.Float{Value: f}, nil
	})
}

func secureRandomBool(args []runtime.Value) (runtime.Value, error) {
	sess, err := lookupSecureSession(firstArg(args), "secureRandom.bool")
	if err != nil {
		return nil, err
	}
	rest := args[1:]
	if len(rest) != 0 {
		return nil, fmt.Errorf("secureRandom.bool(session)")
	}
	return drawAndLog(sess, "bool", rest, func(seed []byte, nonce int64) (runtime.Value, error) {
		msg, _ := makeMessage(sess.clientSeed, nonce, "bool", rest)
		b := deriveBytes(seed, msg, 1)
		return runtime.Bool{Value: b[0]&1 == 1}, nil
	})
}

func secureRandomChoice(args []runtime.Value) (runtime.Value, error) {
	sess, err := lookupSecureSession(firstArg(args), "secureRandom.choice")
	if err != nil {
		return nil, err
	}
	rest := args[1:]
	if len(rest) != 1 {
		return nil, fmt.Errorf("secureRandom.choice(session, list)")
	}
	list, ok := rest[0].(*runtime.List)
	if !ok || len(list.Elements) == 0 {
		return nil, fmt.Errorf("secureRandom.choice: argument must be a non-empty list")
	}
	return drawAndLog(sess, "choice", rest, func(seed []byte, nonce int64) (runtime.Value, error) {
		msg, _ := makeMessage(sess.clientSeed, nonce, "choice", rest)
		idx := uniformUintInRange(seed, msg, uint64(len(list.Elements)))
		return list.Elements[idx], nil
	})
}

func secureRandomShuffle(args []runtime.Value) (runtime.Value, error) {
	sess, err := lookupSecureSession(firstArg(args), "secureRandom.shuffle")
	if err != nil {
		return nil, err
	}
	rest := args[1:]
	if len(rest) != 1 {
		return nil, fmt.Errorf("secureRandom.shuffle(session, list)")
	}
	list, ok := rest[0].(*runtime.List)
	if !ok {
		return nil, fmt.Errorf("secureRandom.shuffle: argument must be a list")
	}
	return drawAndLog(sess, "shuffle", rest, func(seed []byte, nonce int64) (runtime.Value, error) {
		baseMsg, _ := makeMessage(sess.clientSeed, nonce, "shuffle", rest)
		out := make([]runtime.Value, len(list.Elements))
		copy(out, list.Elements)
		for i := len(out) - 1; i > 0; i-- {
			j := uniformUintInRange(seed, baseMsg+":swap:"+strconv.Itoa(i), uint64(i+1))
			out[i], out[j] = out[j], out[i]
		}
		return &runtime.List{Elements: out}, nil
	})
}

func secureRandomWeightedChoice(args []runtime.Value) (runtime.Value, error) {
	sess, err := lookupSecureSession(firstArg(args), "secureRandom.weightedChoice")
	if err != nil {
		return nil, err
	}
	rest := args[1:]
	if len(rest) != 2 {
		return nil, fmt.Errorf("secureRandom.weightedChoice(session, items, weights)")
	}
	items, ok1 := rest[0].(*runtime.List)
	weightsList, ok2 := rest[1].(*runtime.List)
	if !ok1 || !ok2 || len(items.Elements) == 0 || len(items.Elements) != len(weightsList.Elements) {
		return nil, fmt.Errorf("secureRandom.weightedChoice: items and weights must be non-empty same-length lists")
	}
	weights := make([]float64, len(weightsList.Elements))
	total := 0.0
	for i, w := range weightsList.Elements {
		f, err := FloatLike(w)
		if err != nil {
			return nil, fmt.Errorf("secureRandom.weightedChoice: weight at index %d must be numeric", i)
		}
		if f < 0 {
			return nil, fmt.Errorf("secureRandom.weightedChoice: weight at index %d is negative", i)
		}
		weights[i] = f
		total += f
	}
	if total == 0 {
		return nil, fmt.Errorf("secureRandom.weightedChoice: total weight is zero")
	}
	return drawAndLog(sess, "weightedChoice", rest, func(seed []byte, nonce int64) (runtime.Value, error) {
		msg, _ := makeMessage(sess.clientSeed, nonce, "weightedChoice", rest)
		bytes := deriveBytes(seed, msg, 8)
		u := binary.BigEndian.Uint64(bytes) >> 11
		t := (float64(u) / float64(1<<53)) * total
		acc := 0.0
		for i, w := range weights {
			acc += w
			if t < acc {
				return items.Elements[i], nil
			}
		}
		return items.Elements[len(items.Elements)-1], nil
	})
}

func drawAndLog(sess *secureSession, method string, args []runtime.Value, derive func(seed []byte, nonce int64) (runtime.Value, error)) (runtime.Value, error) {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if sess.revealed {
		return nil, fmt.Errorf("secureRandom.%s: session has been revealed; no further draws allowed", method)
	}
	nonce := sess.nonce
	output, err := derive(sess.serverSeed, nonce)
	if err != nil {
		return nil, err
	}
	argsCopy := make([]runtime.Value, len(args))
	copy(argsCopy, args)
	sess.log = append(sess.log, secureLogEntry{
		Nonce:  nonce,
		Method: method,
		Args:   argsCopy,
		Output: output,
	})
	sess.nonce++
	return output, nil
}

func secureRandomVerifyCommitment(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("secureRandom.verifyCommitment(commitmentHex, serverSeedHex)")
	}
	commit, ok1 := args[0].(runtime.String)
	seed, ok2 := args[1].(runtime.String)
	if !ok1 || !ok2 {
		return nil, fmt.Errorf("secureRandom.verifyCommitment: arguments must be hex strings")
	}
	seedBytes, err := hex.DecodeString(seed.Value)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(seedBytes)
	return runtime.Bool{Value: hmac.Equal([]byte(commit.Value), []byte(hex.EncodeToString(hash[:])))}, nil
}

// secureRandomReplay re-derives a draw outcome from raw inputs:
//
//	secureRandom.replay(serverSeedHex, clientSeed, nonce, method, args)
//
// Returns the recomputed output value; a verifier can compare it to
// the logged output for that draw.
func secureRandomReplay(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 5 {
		return nil, fmt.Errorf("secureRandom.replay(serverSeedHex, clientSeed, nonce, method, args)")
	}
	seedHex, ok1 := args[0].(runtime.String)
	clientSeedV, ok2 := args[1].(runtime.String)
	nonceV, ok3 := AsInt64(args[2])
	methodV, ok4 := args[3].(runtime.String)
	argsList, ok5 := args[4].(*runtime.List)
	if !ok1 || !ok2 || !ok3 || !ok4 || !ok5 {
		return nil, fmt.Errorf("secureRandom.replay: bad argument types")
	}
	seedBytes, err := hex.DecodeString(seedHex.Value)
	if err != nil {
		return nil, err
	}
	clientSeed := clientSeedV.Value
	method := methodV.Value
	msg, err := makeMessage(clientSeed, nonceV, method, argsList.Elements)
	if err != nil {
		return nil, err
	}
	switch method {
	case "bytes":
		if len(argsList.Elements) != 1 {
			return nil, fmt.Errorf("replay bytes expects [n]")
		}
		n, ok := AsInt64(argsList.Elements[0])
		if !ok || n < 0 {
			return nil, fmt.Errorf("replay bytes n invalid")
		}
		return runtime.Bytes{Value: deriveBytes(seedBytes, msg, int(n))}, nil
	case "uintRange":
		if len(argsList.Elements) != 2 {
			return nil, fmt.Errorf("replay uintRange expects [lo, hi]")
		}
		lo, _ := AsInt64(argsList.Elements[0])
		hi, _ := AsInt64(argsList.Elements[1])
		if hi <= lo {
			return nil, fmt.Errorf("replay uintRange: hi must be > lo")
		}
		v := uniformUintInRange(seedBytes, msg, uint64(hi-lo))
		return runtime.SmallInt{Value: int64(v) + lo}, nil
	case "randomInt":
		lo, hi, err := inclusiveBounds(argsList.Elements, "replay randomInt")
		if err != nil {
			return nil, err
		}
		return inclusiveIntDraw(seedBytes, msg, lo, hi), nil
	case "float":
		bytes := deriveBytes(seedBytes, msg, 8)
		u := binary.BigEndian.Uint64(bytes) >> 11
		return runtime.Float{Value: float64(u) / float64(1<<53)}, nil
	case "bool":
		b := deriveBytes(seedBytes, msg, 1)
		return runtime.Bool{Value: b[0]&1 == 1}, nil
	case "choice":
		if len(argsList.Elements) != 1 {
			return nil, fmt.Errorf("replay choice expects [list]")
		}
		list, ok := argsList.Elements[0].(*runtime.List)
		if !ok || len(list.Elements) == 0 {
			return nil, fmt.Errorf("replay choice: argument must be a non-empty list")
		}
		idx := uniformUintInRange(seedBytes, msg, uint64(len(list.Elements)))
		return list.Elements[idx], nil
	case "shuffle":
		if len(argsList.Elements) != 1 {
			return nil, fmt.Errorf("replay shuffle expects [list]")
		}
		list, ok := argsList.Elements[0].(*runtime.List)
		if !ok {
			return nil, fmt.Errorf("replay shuffle: argument must be a list")
		}
		out := make([]runtime.Value, len(list.Elements))
		copy(out, list.Elements)
		for i := len(out) - 1; i > 0; i-- {
			j := uniformUintInRange(seedBytes, msg+":swap:"+strconv.Itoa(i), uint64(i+1))
			out[i], out[j] = out[j], out[i]
		}
		return &runtime.List{Elements: out}, nil
	case "weightedChoice":
		if len(argsList.Elements) != 2 {
			return nil, fmt.Errorf("replay weightedChoice expects [items, weights]")
		}
		items, ok1 := argsList.Elements[0].(*runtime.List)
		weightsList, ok2 := argsList.Elements[1].(*runtime.List)
		if !ok1 || !ok2 || len(items.Elements) == 0 || len(items.Elements) != len(weightsList.Elements) {
			return nil, fmt.Errorf("replay weightedChoice: items and weights must be non-empty same-length lists")
		}
		total := 0.0
		weights := make([]float64, len(weightsList.Elements))
		for i, w := range weightsList.Elements {
			f, err := FloatLike(w)
			if err != nil {
				return nil, err
			}
			weights[i] = f
			total += f
		}
		bytes := deriveBytes(seedBytes, msg, 8)
		u := binary.BigEndian.Uint64(bytes) >> 11
		t := (float64(u) / float64(1<<53)) * total
		acc := 0.0
		for i, w := range weights {
			acc += w
			if t < acc {
				return items.Elements[i], nil
			}
		}
		return items.Elements[len(items.Elements)-1], nil
	}
	return nil, fmt.Errorf("replay: unknown method %q", method)
}

// Keep big.Int referenced so the import isn't dropped by goimports in
// future refactors. (big.Int is used indirectly by the FloatLike path
// for Decimal weights.)
var _ = big.NewInt
