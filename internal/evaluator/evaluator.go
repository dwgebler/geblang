package evaluator

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"geblang/internal/ast"
	"geblang/internal/binding"
	"geblang/internal/cdp"
	"geblang/internal/concurrent"
	"geblang/internal/desugar"
	"geblang/internal/native"
	"geblang/internal/overload"
	"geblang/internal/runtime"

	tomllib "github.com/BurntSushi/toml"
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pkg/sftp"
	amqp091 "github.com/rabbitmq/amqp091-go"
	kafkago "github.com/segmentio/kafka-go"
	"golang.org/x/crypto/ssh"
	yamllib "gopkg.in/yaml.v3"
	_ "modernc.org/sqlite"
)

type Result struct {
	ExitCode int
	Exited   bool
}

const DefaultMaxCallDepth = 10000

type Evaluator struct {
	stdout      io.Writer
	stderr      io.Writer
	stdin       io.Reader
	stdinReader *bufio.Reader
	// declAnnotation distinguishes a declaration annotation from other
	// expected-type contexts (return position, arg hints) by pointer identity.
	declAnnotation *ast.TypeRef
	// pendingEnumThis binds `this` to a non-Instance receiver (an enum
	// variant) for the next method body; consumed at call-env setup.
	pendingEnumThis runtime.Value
	// enumThisForChild carries the enum receiver into a generator/async
	// child evaluator, which runs the body on its own goroutine.
	enumThisForChild     runtime.Value
	imports              map[string]bool
	importNames          map[string]string
	currentModule        string
	modulePaths          []string
	modules              map[string]*runtime.Module
	loading              map[string]bool
	modulePrograms       map[string]*ast.Program
	manifests            map[string]*packageManifest
	typeAliases          map[string]*ast.TypeRef
	natives              *native.Registry
	builtins             map[string]map[string]builtinFunc
	testClass            *runtime.Class
	httpRequestClass     *runtime.Class
	httpResponseClass    *runtime.Class
	httpClientClass      *runtime.Class
	httpBuilderClass     *runtime.Class
	httpCookieJarClass   *runtime.Class
	httpFetchStreamClass *runtime.Class
	processClass         *runtime.Class
	processResultClass   *runtime.Class
	dbConnectionClass    *runtime.Class
	dbTransactionClass   *runtime.Class
	dbStatementClass     *runtime.Class
	dbRowsClass          *runtime.Class
	streamIfaces         map[string]*runtime.Interface
	errorClassParents    map[string]string
	// globalClasses is the cross-module class registry. Every user
	// class registers here at definition time so reflect.class(name)
	// from any module can find it.
	globalClasses map[string]*runtime.Class
	// Cross-module function registry mirroring globalClasses, so
	// reflect.function(name) resolves an imported module's export.
	globalFunctions map[string]runtime.Function
	// Identifier name -> original class name for class identifiers
	// that a class decorator rebound to a callable. Used by
	// applyCallableValue to stamp the returned instance.
	decoratedClassIdents map[string]string
	errorSentinels       map[string]*runtime.Class
	deferFrames          []*deferFrame
	yieldFrames          []*yieldFrame
	callStack            []evalFrame
	currentLine          int // most recent statement/call/throw line, for error attribution
	hofCallLine          int // call-site line of the primitive HOF dispatching a callback, so the caller frame's trace line is the HOF call site, not a stale callback line
	topLevelLine         int // line of the call that left the top-level frame
	callDepth            int
	maxCallDepth         int
	// classStack tracks the lexical class of the currently executing
	// method/constructor. `parent(...)` resolves to the parent of the top of
	// this stack rather than to `this.Class.Parent`, so that calling
	// `parent(msg)` inside e.g. AppError's body always dispatches to Error,
	// not back to AppError, regardless of the runtime class of `this`.
	classStack []*runtime.Class
	// destructibleInstances tracks instances of classes that
	// declare a `func ~ClassName()` destructor. The sweep at
	// Cleanup() time invokes their destructors in reverse-creation
	// order (LIFO). `del x` removes the corresponding entry and
	// fires the destructor immediately.
	destructibleInstances []*runtime.Instance
	args                  []string
	debug                 *debugState
	threadID              int
	debugSuppressed       int // > 0: callDebugHook/startDebugThread are no-ops
	debugEvalCancel       <-chan struct{}
	parent                *Evaluator
	AssertionsDisabled    bool
	vmDispatcher          MethodDispatcher
	dbMu                  sync.Mutex
	nextDBID              int64
	dbs                   map[int64]*sql.DB
	dbDrivers             map[int64]string
	nextTxID              int64
	txs                   map[int64]*dbTxHandle
	nextStmtID            int64
	stmts                 map[int64]*dbStmtHandle
	nextDBRowsID          int64
	dbRows                map[int64]*dbRowsHandle
	fileMu                sync.Mutex
	nextFileID            int64
	files                 map[int64]*os.File
	bufReaders            map[int64]*bufio.Reader
	bufferMu              sync.Mutex
	nextBufferID          int64
	buffers               map[int64]*bytes.Buffer
	streamMu              sync.Mutex
	nextStreamID          int64
	streams               map[int64]*ioStreamHandle
	processMu             sync.Mutex
	nextProcID            int64
	processes             map[int64]*processHandle
	logMu                 sync.Mutex
	nextLogID             int64
	loggers               map[int64]*loggerHandle
	metricsMu             sync.Mutex
	metrics               map[string]float64
	metricRegistry        map[string]*metricsEntry
	traceMu               sync.Mutex
	nextTraceID           int64
	traces                map[int64]*traceSpan
	watchMu               sync.Mutex
	nextWatchID           int64
	watches               map[int64]*watchHandle
	signalMu              sync.Mutex
	signalHandlers        map[string]*signalSubscription
	webMu                 sync.Mutex
	nextWebID             int64
	webApps               map[int64]*webApp
	wsMu                  sync.Mutex
	nextWSID              int64
	websockets            map[int64]*wsHandle
	amqpMu                sync.Mutex
	nextAmqpConnID        int64
	amqpConns             map[int64]*amqp091.Connection
	nextAmqpChanID        int64
	amqpChans             map[int64]*amqp091.Channel
	kafkaMu               sync.Mutex
	nextKafkaWriterID     int64
	kafkaWriters          map[int64]*kafkago.Writer
	nextKafkaReaderID     int64
	kafkaReaders          map[int64]*kafkaReaderHandle
	netMu                 sync.Mutex
	nextNetID             int64
	netHandles            map[int64]*netHandle
	netServerMu           sync.Mutex
	nextNetServerID       int64
	netServers            map[int64]*netServerHandle
	sshMu                 sync.Mutex
	nextSSHID             int64
	sshClients            map[int64]*sshClientHandle
	sshSessions           map[int64]*sshSessionHandle
	sshTunnels            map[int64]*sshTunnelHandle
	httpServerMu          sync.Mutex
	nextHTTPServerID      int64
	httpServers           map[int64]*httpServerHandle
	httpStreamMu          sync.Mutex
	nextHTTPStreamID      int64
	httpStreams           map[int64]*httpStreamHandle
	httpClientMu          sync.Mutex
	nextHTTPClientID      int64
	httpClientHandles     map[int64]*httpClientHandle
	httpCookieJarMu       sync.Mutex
	nextCookieJarID       int64
	httpCookieJars        map[int64]http.CookieJar
	httpFetchStreamMu     sync.Mutex
	nextFetchStreamID     int64
	httpFetchStreams      map[int64]*httpFetchStreamHandle
	httpResponseStreamMu  sync.Mutex
	nextResponseStreamID  int64
	httpResponseStreams   map[int64]*httpResponseStreamHandle
	httpStreamRespClass   *runtime.Class
	onnxMu                sync.Mutex
	nextOnnxID            int64
	onnxSessions          map[int64]*native.ONNXSession
	onnxClass             *runtime.Class
	browserMu             sync.Mutex
	nextBrowserID         int64
	nextPageID            int64
	browsers              map[int64]*cdp.Browser
	pages                 map[int64]*cdp.Page
	browserRoutes         map[string][]browserRoute
	browserClassCache     *runtime.Class
	pageClassCache        *runtime.Class
	jsonMu                sync.Mutex
	nextJSONID            int64
	jsonReaders           map[int64]*jsonStreamReader
	xmlMu                 sync.Mutex
	nextXMLID             int64
	xmlReaders            map[int64]*xmlStreamReader
	csvMu                 sync.Mutex
	nextCSVID             int64
	csvReaders            map[int64]*csvStreamReader
	yamlMu                sync.Mutex
	nextYAMLID            int64
	yamlReaders           map[int64]*yamlStreamReader
	extMu                 sync.Mutex
	nextExtID             int64
	extConns              map[int64]*extHandle
	ffi                   *ffiState
}

type builtinFunc func(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error)

// MethodDispatcher is implemented by the VM so the evaluator can call methods
// on VM-owned instances and run test classes compiled to bytecode.
type MethodDispatcher interface {
	HasInstanceMethod(instance *runtime.Instance, name string) bool
	CallInstanceMethod(instance *runtime.Instance, name string, args []runtime.Value) (runtime.Value, error)
	RunTestClass(classIndex int64, tagFilter []string) (runtime.Value, error)
	PatchNative(module, name string, fn native.Function)
	UnpatchNative(module, name string)
	NativeSnapshot() map[string]native.Function
	RestoreNatives(snapshot map[string]native.Function)
}

type httpClientHandle struct {
	client  *http.Client
	baseURL string
	headers http.Header
}

type httpFetchStreamHandle struct {
	ch    chan runtime.Value
	total int
	mu    sync.Mutex
	read  int
}

type dbRowsHandle struct {
	rows     *sql.Rows
	columns  []string
	textCols []bool
	current  runtime.Value
	// cache backs all/get/first random access; empty and unused until one
	// of those is called, so a plain next()/row() scan stays O(1) memory.
	cache      []runtime.Value
	caching    bool
	prefetched runtime.Value
	closed     bool
	exhausted  bool
}

type dbStmtHandle struct {
	stmt       *sql.Stmt
	driver     string
	paramNames []string
}

type dbTxHandle struct {
	tx     *sql.Tx
	driver string
}

type processHandle struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser
	cancel context.CancelFunc
}

type ioStreamHandle struct {
	name      string
	reader    io.Reader
	writer    io.Writer
	closer    io.Closer
	memory    *memoryStream
	restore   func()
	closed    bool
	bufReader *bufio.Reader
}

type netHandle struct {
	listener net.Listener
	conn     net.Conn
	packet   net.PacketConn
}

// netServerHandle owns an accept-loop goroutine for net.serve. The
// goroutine exits when the listener is closed; stopServerHandle
// closes the listener and waits for the goroutine to drain.
type netServerHandle struct {
	listener net.Listener
	wg       sync.WaitGroup
	stopped  bool
	pool     *concurrent.Pool
}

// sshClientHandle wraps an established ssh.Client plus a lazily
// created sftp.Client (shared across SFTP calls on the same
// connection).
type sshClientHandle struct {
	client  *ssh.Client
	sftpMu  sync.Mutex
	sftpCli *sftp.Client
	closed  bool
}

// sshSessionHandle wraps a long-running ssh.Session whose stdin /
// stdout / stderr have been wired to streamable pipes. Mirrors
// processHandle for proc.spawn.
type sshSessionHandle struct {
	session *ssh.Session
	stdin   io.WriteCloser
	stdout  io.Reader
	stderr  io.Reader
}

// sshTunnelHandle owns the accept-loop goroutine for a local or
// remote port forward.
type sshTunnelHandle struct {
	listener net.Listener
	wg       sync.WaitGroup
	stopped  bool
}

type httpServerHandle struct {
	server   *http.Server
	listener net.Listener
	done     chan error
	pool     *concurrent.Pool
	certPEM  []byte // served TLS certificate (PEM); nil for plain HTTP
}

type httpStreamHandle struct {
	writer  http.ResponseWriter
	flusher http.Flusher
	closed  bool
}

type jsonStreamReader struct {
	decoder *json.Decoder
	stack   []jsonContext
	pending runtime.Value
	done    bool
}

type jsonContext struct {
	kind      byte
	expectKey bool
}

type xmlStreamReader struct {
	decoder *xml.Decoder
	pending runtime.Value
	done    bool
	source  string
	depth   int
	roots   int
}

type csvStreamReader struct {
	reader  *csv.Reader
	pending runtime.Value
	done    bool
	row     int64
}

type yamlStreamReader struct {
	decoder *yamllib.Decoder
	pending runtime.Value
	queue   []runtime.Value
	done    bool
}

type processSpec struct {
	command string
	args    []string
	cwd     string
	env     map[string]string
	timeout time.Duration
}

type traceSpan struct {
	name         string
	start        time.Time
	end          time.Time
	ended        bool
	attrs        map[string]runtime.Value
	events       []traceEvent
	traceID      []byte // 16 bytes; empty for legacy spans created before OTLP support
	spanID       []byte // 8 bytes
	parentSpanID []byte // 8 bytes; empty for root spans
}

type traceEvent struct {
	name  string
	at    time.Time
	attrs map[string]runtime.Value
}

type webApp struct {
	routes            []webRoute
	beforeMiddlewares []runtime.Value
	middlewares       []runtime.Value
}

type webRoute struct {
	method  string
	path    string
	handler runtime.Value
}

// leveledSink receives the log level too, for sinks (syslog) whose framing depends on severity.
type leveledSink interface {
	WriteLevel(level, line string) error
}

type loggerHandle struct {
	target  string
	writer  io.Writer
	closer  io.Closer
	handler *runtime.Instance
	leveled leveledSink
}

type deferredCall struct {
	expr ast.Expression
	env  *runtime.Environment
}

type deferFrame struct {
	calls []deferredCall
}

type evalFrame struct {
	name     string
	line     int
	callLine int // source line of the call that entered this frame
	fn       *runtime.Function
	env      *runtime.Environment // environment at function entry (for per-frame variable inspection)
}

type generatorItem struct {
	value runtime.Value
	err   error
}

type yieldFrame struct {
	values []runtime.Value
	ch     chan generatorItem
	done   <-chan struct{}
}

type signal struct {
	value    runtime.Value
	thrown   *runtime.Error
	exited   bool
	exitCode int
	kind     string
}

type thrownError struct {
	value runtime.Error
}

// destructorFailure recovers the typed throw from a destructor error; non-throw faults class as RuntimeError.
func destructorFailure(err error) runtime.Error {
	var thrown thrownError
	if errors.As(err, &thrown) {
		return thrown.value
	}
	return runtime.NewRecoverableError(err)
}

// uncaughtFromError converts a top-level thrownError exit to the canonical contract; other errors pass through.
func uncaughtFromError(err error) error {
	var thrown thrownError
	if errors.As(err, &thrown) {
		return uncaughtFromThrown(thrown.value)
	}
	return err
}

func uncaughtFromThrown(v runtime.Error) *runtime.UncaughtError {
	return &runtime.UncaughtError{
		Class:        v.Class,
		Message:      v.Message,
		ErrorLine:    v.ErrorLine,
		Frames:       v.TraceFrames,
		TopLevelLine: v.TopLevelLine,
	}
}

func (e thrownError) Error() string {
	return e.value.Inspect()
}

// ErrorClass exposes the carried Geblang error class so the VM /
// other native call sites can preserve the typed throw across the
// native-to-script boundary via runtime.TypedError.
func (e thrownError) ErrorClass() string {
	return e.value.Class
}

func New(stdout io.Writer) *Evaluator {
	return NewWithArgs(stdout, nil)
}

func NewWithArgs(stdout io.Writer, args []string) *Evaluator {
	return NewWithArgsAndModulePaths(stdout, args, nil)
}

func newEvaluatorCore(stdout io.Writer, args []string, modulePaths []string) *Evaluator {
	if stdout == nil {
		stdout = io.Discard
	}
	e := &Evaluator{stdout: stdout, stderr: os.Stderr, stdin: os.Stdin, imports: map[string]bool{}, importNames: map[string]string{}, modulePaths: append([]string(nil), modulePaths...), modules: map[string]*runtime.Module{}, loading: map[string]bool{}, modulePrograms: map[string]*ast.Program{}, manifests: map[string]*packageManifest{}, typeAliases: map[string]*ast.TypeRef{}, maxCallDepth: DefaultMaxCallDepth, args: append([]string(nil), args...), dbs: map[int64]*sql.DB{}, dbDrivers: map[int64]string{}, txs: map[int64]*dbTxHandle{}, stmts: map[int64]*dbStmtHandle{}, dbRows: map[int64]*dbRowsHandle{}, files: map[int64]*os.File{}, bufReaders: map[int64]*bufio.Reader{}, buffers: map[int64]*bytes.Buffer{}, streams: map[int64]*ioStreamHandle{}, processes: map[int64]*processHandle{}, loggers: map[int64]*loggerHandle{}, metrics: map[string]float64{}, metricRegistry: map[string]*metricsEntry{}, traces: map[int64]*traceSpan{}, watches: map[int64]*watchHandle{}, webApps: map[int64]*webApp{}, websockets: map[int64]*wsHandle{}, amqpConns: map[int64]*amqp091.Connection{}, amqpChans: map[int64]*amqp091.Channel{}, kafkaWriters: map[int64]*kafkago.Writer{}, kafkaReaders: map[int64]*kafkaReaderHandle{}, netHandles: map[int64]*netHandle{}, netServers: map[int64]*netServerHandle{}, sshClients: map[int64]*sshClientHandle{}, sshSessions: map[int64]*sshSessionHandle{}, sshTunnels: map[int64]*sshTunnelHandle{}, httpServers: map[int64]*httpServerHandle{}, httpStreams: map[int64]*httpStreamHandle{}, httpClientHandles: map[int64]*httpClientHandle{}, httpCookieJars: map[int64]http.CookieJar{}, httpFetchStreams: map[int64]*httpFetchStreamHandle{}, httpResponseStreams: map[int64]*httpResponseStreamHandle{}, onnxSessions: map[int64]*native.ONNXSession{}, browsers: map[int64]*cdp.Browser{}, pages: map[int64]*cdp.Page{}, jsonReaders: map[int64]*jsonStreamReader{}, xmlReaders: map[int64]*xmlStreamReader{}, csvReaders: map[int64]*csvStreamReader{}, yamlReaders: map[int64]*yamlStreamReader{}, extConns: map[int64]*extHandle{}, ffi: newFFIState(), natives: native.NewBuiltinRegistry(), errorClassParents: map[string]string{}, errorSentinels: map[string]*runtime.Class{}, globalClasses: map[string]*runtime.Class{}, globalFunctions: map[string]runtime.Function{}, decoratedClassIdents: map[string]string{}}
	e.builtins = e.builtinModules()
	e.natives.SetConversionContext(native.ConversionContext{InstanceInvoker: e.isolatedInstanceInvoker, ClassDeserializer: e.isolatedClassDeserializer})
	return e
}

// NewWithArgsAndModulePaths builds a root evaluator; newEvaluatorCore installs its serialize/deserialize context on its own native registry, so concurrent or independent evaluators never cross-dispatch.
func NewWithArgsAndModulePaths(stdout io.Writer, args []string, modulePaths []string) *Evaluator {
	return newEvaluatorCore(stdout, args, modulePaths)
}

// isolatedInstanceInvoker dispatches a native instance callback (e.g. __serialize) on a fresh child evaluator so concurrent calls never share the root's mutable call state. lookupMethod is read-only, so the existence check avoids a child clone for instances without the method.
func (e *Evaluator) isolatedInstanceInvoker(instance *runtime.Instance, method string, args []runtime.Value) (runtime.Value, bool, error) {
	if instance == nil || instance.Class == nil {
		return nil, false, nil
	}
	if _, ok := lookupMethod(instance.Class, method); !ok {
		return nil, false, nil
	}
	return e.childForCallback().invokeInstanceMethod(instance, method, args)
}

// isolatedClassDeserializer deserializes on a fresh child evaluator for the same reason.
func (e *Evaluator) isolatedClassDeserializer(classValue runtime.Value, value runtime.Value) (runtime.Value, error) {
	return e.childForCallback().deserializeIntoClass(classValue, value)
}

// deserializeIntoClass implements native.ClassDeserializer for
// the tree-walking evaluator. Tries static __deserialize__ first;
// falls back to positional constructor calls with dict keys
// matched against constructor parameter names.
func evalFieldTypeInfos(class *runtime.Class) []native.FieldTypeInfo {
	infos := make([]native.FieldTypeInfo, len(class.Fields))
	for i, field := range class.Fields {
		t := ""
		if field.Type != nil {
			t = field.Type.String()
		}
		infos[i] = native.FieldTypeInfo{Name: field.Name, Type: t}
	}
	return infos
}

// resolveDeserializeClass resolves a field's declared class name to a
// class value for nested parseAs via the cross-module class registry.
func (e *Evaluator) resolveDeserializeClass(name string) (runtime.Value, bool) {
	// Walk the parent chain: a per-call callback child has empty globalClasses and resolves nested classes against the root that declared them.
	for cur := e; cur != nil; cur = cur.parent {
		if class, ok := cur.globalClasses[name]; ok {
			return class, true
		}
	}
	return nil, false
}

func (e *Evaluator) deserializeIntoClass(classValue runtime.Value, value runtime.Value) (runtime.Value, error) {
	class, ok := classValue.(*runtime.Class)
	if !ok {
		return nil, fmt.Errorf("deserialize: expected class, got %s", classValue.TypeName())
	}
	if method, ok := lookupStaticDunderEval(class, "__deserialize", "__deserialize__"); ok {
		return e.applyFunction(method, []runtime.Value{value})
	}
	dict, ok := value.(runtime.Dict)
	if !ok {
		return nil, fmt.Errorf("deserialize %s: expected dict, got %s", class.Name, value.TypeName())
	}
	hydrated, herr := native.HydrateNestedClassFields(evalFieldTypeInfos(class), dict, e.resolveDeserializeClass, e.deserializeIntoClass)
	if herr != nil {
		return nil, herr
	}
	dict = hydrated
	if len(class.Constructors) == 0 {
		// Classes without an explicit constructor get their fields
		// populated directly from the dict, matching the implicit
		// "data class" shape. Missing keys leave the default; extra
		// keys are ignored.
		v, err := e.instantiateClass(class, nil)
		if err != nil {
			return nil, err
		}
		instance, ok := v.(*runtime.Instance)
		if !ok {
			return v, nil
		}
		for _, field := range class.Fields {
			key := runtime.String{Value: field.Name}
			entry, hit := dict.GetEntry(native.DictKey(key))
			if hit {
				instance.Fields[field.Name] = entry.Value
			}
		}
		return instance, nil
	}
	ctor := class.Constructors[0]
	args := make([]runtime.Value, 0, len(ctor.Parameters))
	for _, param := range ctor.Parameters {
		paramName := ""
		if param.Name != nil {
			paramName = param.Name.Value
		}
		key := runtime.String{Value: paramName}
		entry, ok := dict.GetEntry(native.DictKey(key))
		if ok {
			args = append(args, entry.Value)
			continue
		}
		if param.Default != nil {
			defaultVal, err := e.evalExpression(param.Default, runtime.NewEnclosedEnvironment(class.Env))
			if err != nil {
				return nil, fmt.Errorf("deserialize %s: default for %s: %w", class.Name, paramName, err)
			}
			args = append(args, defaultVal)
			continue
		}
		return nil, fmt.Errorf("deserialize %s: missing field %q", class.Name, paramName)
	}
	return e.instantiateClass(class, args)
}

// invokeInstanceMethod implements native.InstanceInvoker for the
// tree-walking evaluator. Returns (result, true, nil) when the
// method exists and was called, (nil, false, nil) when the class
// has no such method, (nil, false, err) on call error.
func (e *Evaluator) invokeInstanceMethod(instance *runtime.Instance, method string, args []runtime.Value) (runtime.Value, bool, error) {
	if instance == nil || instance.Class == nil {
		return nil, false, nil
	}
	fn, ok := lookupMethod(instance.Class, method)
	if !ok {
		return nil, false, nil
	}
	result, err := e.applyFunctionWithThis(fn, args, instance)
	if err != nil {
		return nil, false, err
	}
	return result, true, nil
}

// displayString renders a value for interpolation/println via __string when
// defined (VM-owned instances dispatch through the bridge), else Inspect.
func (e *Evaluator) displayString(value runtime.Value) (string, error) {
	instance, ok := value.(*runtime.Instance)
	if !ok {
		return value.Inspect(), nil
	}
	if result, handled, err := e.invokeInstanceMethod(instance, "__string", nil); err != nil {
		return "", err
	} else if handled {
		if err := checkCastDunderReturn("string", result); err != nil {
			return "", thrownError{value: e.withTrace(runtime.Error{Class: "RuntimeError", Message: err.Error()})}
		}
		return result.(runtime.String).Value, nil
	}
	if e.vmDispatcher != nil && e.vmDispatcher.HasInstanceMethod(instance, "__string") {
		result, err := e.vmDispatcher.CallInstanceMethod(instance, "__string", nil)
		if err != nil {
			return "", err
		}
		if err := checkCastDunderReturn("string", result); err != nil {
			return "", thrownError{value: e.withTrace(runtime.Error{Class: "RuntimeError", Message: err.Error()})}
		}
		return result.(runtime.String).Value, nil
	}
	return value.Inspect(), nil
}

func (e *Evaluator) SetMaxCallDepth(limit int) {
	e.maxCallDepth = limit
}

func (e *Evaluator) SetMethodDispatcher(d MethodDispatcher) {
	e.vmDispatcher = d
}

// NativeSnapshot / RestoreNatives expose the bridge registry's test.mock patches so the VM test runner can roll them back per @test method (stateful natives dispatch through this evaluator).
func (e *Evaluator) NativeSnapshot() map[string]native.Function {
	return e.natives.Snapshot()
}

func (e *Evaluator) RestoreNatives(snapshot map[string]native.Function) {
	e.natives.Restore(snapshot)
}

func (e *Evaluator) HandleDirectPrint() bool {
	return true
}

// nativeBuiltinValue wraps a pure native builtin (canonical.name) as a
// first-class callable value, or returns false if no such native exists.
// Gated on the pure-native registry so the VM resolves the identical set.
func (e *Evaluator) nativeBuiltinValue(canonical, name string) (runtime.Value, bool) {
	if e.natives.LookupKey(native.Key(canonical, name)) == nil {
		return nil, false
	}
	return e.wrapBuiltinAsFunction(canonical, name, e.registryBuiltin(canonical, name)), true
}

func (e *Evaluator) registryBuiltin(module, name string) builtinFunc {
	key := native.Key(module, name)
	return func(_ *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
		return e.natives.CallKey(key, args)
	}
}

func (e *Evaluator) CallBuiltin(module, name string, args []runtime.Value, argNames []string) (runtime.Value, error) {
	if e.httpRequestClass == nil || e.httpResponseClass == nil {
		if err := e.installBuiltinTypes(runtime.NewEnvironment()); err != nil {
			return nil, err
		}
	}
	functions, ok := e.builtins[module]
	if !ok {
		return nil, fmt.Errorf("unknown builtin module %s", module)
	}
	function, ok := functions[name]
	if !ok {
		return nil, fmt.Errorf("unknown builtin function %s.%s", module, name)
	}
	call := &ast.CallExpression{
		Callee: &ast.SelectorExpression{
			Object: &ast.Identifier{Value: module},
			Name:   &ast.Identifier{Value: name},
		},
		Arguments: make([]ast.CallArgument, len(args)),
	}
	for i := range args {
		call.Arguments[i].Value = &ast.Literal{Value: nil}
		if i < len(argNames) && argNames[i] != "" {
			call.Arguments[i].Name = &ast.Identifier{Value: argNames[i]}
		}
	}
	return function(call, args)
}

func (e *Evaluator) childForCallback() *Evaluator {
	child := newEvaluatorCore(e.stdout, e.args, e.modulePaths)
	child.stderr = e.stderr
	child.stdin = e.stdin
	child.stdinReader = e.stdinReader
	child.parent = e
	child.maxCallDepth = e.maxCallDepth
	child.imports = maps.Clone(e.imports)
	child.importNames = maps.Clone(e.importNames)
	child.modulePrograms = maps.Clone(e.modulePrograms)
	child.manifests = maps.Clone(e.manifests)
	child.typeAliases = maps.Clone(e.typeAliases)
	child.testClass = e.testClass
	child.httpRequestClass = e.httpRequestClass
	child.httpResponseClass = e.httpResponseClass
	child.httpClientClass = e.httpClientClass
	child.httpBuilderClass = e.httpBuilderClass
	child.httpCookieJarClass = e.httpCookieJarClass
	child.httpFetchStreamClass = e.httpFetchStreamClass
	child.processClass = e.processClass
	child.processResultClass = e.processResultClass
	child.dbConnectionClass = e.dbConnectionClass
	child.dbTransactionClass = e.dbTransactionClass
	child.dbStatementClass = e.dbStatementClass
	child.dbRowsClass = e.dbRowsClass
	child.streamIfaces = maps.Clone(e.streamIfaces)
	// App-global handles (web apps, db connections, loggers, http clients,
	// cookie jars) are created on the parent at setup and resolved here via the
	// parent chain during request handling. Continue the parent's id counters
	// so a handle the child creates can never shadow a parent handle's id.
	child.nextWebID = e.nextWebID
	child.nextDBID = e.nextDBID
	child.nextLogID = e.nextLogID
	child.nextHTTPClientID = e.nextHTTPClientID
	child.nextCookieJarID = e.nextCookieJarID
	child.debug = e.debug
	child.threadID = e.threadID // goroutine spawns override this with a fresh id
	child.debugSuppressed = e.debugSuppressed
	child.debugEvalCancel = e.debugEvalCancel
	return child
}

// Handle lookups for app-global native state walk the parent chain so a handler
// running in a callback child evaluator (e.g. an HTTP request) resolves
// connections/apps/loggers/clients created on the parent at setup. Per-request
// handles created in the child shadow the parent (checked first).
func (e *Evaluator) lookupWebApp(id int64) (*webApp, bool) {
	for ev := e; ev != nil; ev = ev.parent {
		if v, ok := ev.webApps[id]; ok {
			return v, true
		}
	}
	return nil, false
}

func (e *Evaluator) lookupHTTPClient(id int64) (*httpClientHandle, bool) {
	for ev := e; ev != nil; ev = ev.parent {
		if v, ok := ev.httpClientHandles[id]; ok {
			return v, true
		}
	}
	return nil, false
}

func (e *Evaluator) lookupCookieJar(id int64) (http.CookieJar, bool) {
	for ev := e; ev != nil; ev = ev.parent {
		if v, ok := ev.httpCookieJars[id]; ok {
			return v, true
		}
	}
	return nil, false
}

func (e *Evaluator) Eval(program *ast.Program) (result Result, err error) {
	defer func() {
		cleanupErr := e.Cleanup()
		if err == nil && cleanupErr != nil {
			err = cleanupErr
		}
	}()
	if err := desugar.Dataclasses(program); err != nil {
		return Result{}, err
	}
	if err := desugar.Memoize(program); err != nil {
		return Result{}, err
	}
	env := runtime.NewEnvironment()
	if err := e.installBuiltinTypes(env); err != nil {
		return Result{}, err
	}
	e.pushDeferFrame()
	sig, err := e.evalTopLevelStatements(program.Statements, env)
	if err != nil {
		return Result{}, uncaughtFromError(err)
	}
	sig, err = e.runAndPopDefers(sig)
	if err != nil {
		return Result{}, uncaughtFromError(err)
	}
	if sig.exited {
		return Result{ExitCode: sig.exitCode, Exited: true}, nil
	}
	if sig.kind == "throw" && sig.thrown != nil {
		return Result{}, uncaughtFromThrown(*sig.thrown)
	}
	if sig.kind == "debugCancelled" {
		return Result{}, ErrDebugEvaluationCancelled
	}
	return Result{ExitCode: 0}, nil
}

// runDestructorSweep invokes the destructor of every tracked
// instance that hasn't already been destroyed via `del`. Instances
// are visited in reverse-creation order (LIFO) so younger objects -
// which may depend on older ones - clean up first. A destructor
// itself may instantiate new destructor-bearing objects; this is
// handled by draining the registry repeatedly, up to maxDepth
// rounds, to guard against accidental destructor recursion.
func (e *Evaluator) runDestructorSweep() {
	const maxDepth = 4
	for depth := 0; depth < maxDepth; depth++ {
		if len(e.destructibleInstances) == 0 {
			return
		}
		batch := e.destructibleInstances
		e.destructibleInstances = nil
		for i := len(batch) - 1; i >= 0; i-- {
			inst := batch[i]
			if inst == nil || inst.Destroyed || inst.Class == nil || inst.Class.Destructor == nil {
				continue
			}
			inst.Destroyed = true
			if _, err := e.applyFunctionWithThis(*inst.Class.Destructor, nil, inst); err != nil {
				fmt.Fprintln(e.stderr, runtime.RenderDestructorFailure(inst.Class.Name, destructorFailure(err)))
			}
		}
	}
}

func (e *Evaluator) Cleanup() error {
	var firstErr error
	record := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}

	// User-defined destructors run before native handle pools are
	// drained so destructors can still call io.close / db.close etc.
	e.runDestructorSweep()

	e.fileMu.Lock()
	files := e.files
	e.files = map[int64]*os.File{}
	e.fileMu.Unlock()
	for _, file := range files {
		record(file.Close())
	}

	e.streamMu.Lock()
	streams := e.streams
	e.streams = map[int64]*ioStreamHandle{}
	e.streamMu.Unlock()
	for _, stream := range streams {
		record(closeIOStreamHandle(stream))
	}

	e.watchMu.Lock()
	watches := e.watches
	e.watches = map[int64]*watchHandle{}
	e.watchMu.Unlock()
	for _, watcher := range watches {
		record(stopWatchHandle(watcher))
	}

	e.signalMu.Lock()
	signalSubs := e.signalHandlers
	e.signalHandlers = nil
	e.signalMu.Unlock()
	for _, sub := range signalSubs {
		sub.stop()
	}

	e.netServerMu.Lock()
	netServers := e.netServers
	e.netServers = map[int64]*netServerHandle{}
	e.netServerMu.Unlock()
	for _, server := range netServers {
		record(stopNetServerHandle(server))
	}

	e.sshMu.Lock()
	sshTunnels := e.sshTunnels
	sshSessions := e.sshSessions
	sshClients := e.sshClients
	e.sshTunnels = map[int64]*sshTunnelHandle{}
	e.sshSessions = map[int64]*sshSessionHandle{}
	e.sshClients = map[int64]*sshClientHandle{}
	e.sshMu.Unlock()
	for _, tunnel := range sshTunnels {
		record(stopSSHTunnelHandle(tunnel))
	}
	for _, session := range sshSessions {
		_ = session.session.Close()
	}
	for _, client := range sshClients {
		record(closeSSHClientHandle(client))
	}

	e.dbMu.Lock()
	dbRows := e.dbRows
	stmts := e.stmts
	txs := e.txs
	dbs := e.dbs
	e.dbRows = map[int64]*dbRowsHandle{}
	e.stmts = map[int64]*dbStmtHandle{}
	e.txs = map[int64]*dbTxHandle{}
	e.dbs = map[int64]*sql.DB{}
	e.dbDrivers = map[int64]string{}
	e.dbMu.Unlock()
	for _, rows := range dbRows {
		if !rows.closed {
			record(rows.rows.Close())
		}
	}
	for _, stmt := range stmts {
		record(stmt.stmt.Close())
	}
	for _, tx := range txs {
		record(tx.tx.Rollback())
	}
	for _, db := range dbs {
		record(db.Close())
	}

	e.netMu.Lock()
	netHandles := e.netHandles
	e.netHandles = map[int64]*netHandle{}
	e.netMu.Unlock()
	for _, handle := range netHandles {
		record(closeNetHandle(handle))
	}

	e.httpServerMu.Lock()
	httpServers := e.httpServers
	e.httpServers = map[int64]*httpServerHandle{}
	e.httpServerMu.Unlock()
	for _, handle := range httpServers {
		record(closeHTTPServerHandle(handle))
	}

	e.httpStreamMu.Lock()
	e.httpStreams = map[int64]*httpStreamHandle{}
	e.httpStreamMu.Unlock()

	e.httpResponseStreamMu.Lock()
	respStreams := e.httpResponseStreams
	e.httpResponseStreams = map[int64]*httpResponseStreamHandle{}
	e.httpResponseStreamMu.Unlock()
	for _, h := range respStreams {
		h.closeBody()
	}

	e.httpFetchStreamMu.Lock()
	e.httpFetchStreams = map[int64]*httpFetchStreamHandle{}
	e.httpFetchStreamMu.Unlock()

	e.httpClientMu.Lock()
	clientHandles := e.httpClientHandles
	e.httpClientHandles = map[int64]*httpClientHandle{}
	e.httpClientMu.Unlock()
	for _, h := range clientHandles {
		if h.client != nil {
			h.client.CloseIdleConnections()
		}
	}

	e.browserMu.Lock()
	browsers := e.browsers
	e.browsers = map[int64]*cdp.Browser{}
	e.pages = map[int64]*cdp.Page{}
	e.browserRoutes = nil
	e.browserMu.Unlock()
	for _, b := range browsers {
		b.Close()
	}

	e.wsMu.Lock()
	websockets := e.websockets
	e.websockets = map[int64]*wsHandle{}
	e.wsMu.Unlock()
	for _, h := range websockets {
		conn := h.conn
		record(conn.Close())
	}

	e.logMu.Lock()
	loggers := e.loggers
	e.loggers = map[int64]*loggerHandle{}
	e.logMu.Unlock()
	for _, logger := range loggers {
		if logger.closer != nil {
			record(logger.closer.Close())
		}
	}

	e.processMu.Lock()
	processes := e.processes
	e.processes = map[int64]*processHandle{}
	e.processMu.Unlock()
	for _, process := range processes {
		cleanupProcess(process)
	}

	e.traceMu.Lock()
	e.traces = map[int64]*traceSpan{}
	e.traceMu.Unlock()

	e.jsonMu.Lock()
	e.jsonReaders = map[int64]*jsonStreamReader{}
	e.jsonMu.Unlock()

	e.xmlMu.Lock()
	e.xmlReaders = map[int64]*xmlStreamReader{}
	e.xmlMu.Unlock()

	e.csvMu.Lock()
	e.csvReaders = map[int64]*csvStreamReader{}
	e.csvMu.Unlock()

	e.yamlMu.Lock()
	e.yamlReaders = map[int64]*yamlStreamReader{}
	e.yamlMu.Unlock()

	e.extMu.Lock()
	extConns := e.extConns
	e.extConns = map[int64]*extHandle{}
	e.extMu.Unlock()
	for _, h := range extConns {
		closeExtHandle(h)
	}

	return firstErr
}

func (e *Evaluator) evalStatements(stmts []ast.Statement, env *runtime.Environment) (signal, error) {
	for _, stmt := range stmts {
		if sig, stop := e.evalStatementInList(stmt, env); stop {
			return sig, nil
		}
	}
	return signal{}, nil
}

func (e *Evaluator) evalStatementInList(stmt ast.Statement, env *runtime.Environment) (signal, bool) {
	if e.debugEvaluationCancelled() {
		return signal{kind: "debugCancelled"}, true
	}
	e.callDebugHook(stmt, env, "step")
	sig, err := e.evalStatement(stmt, env)
	if err != nil {
		if errors.Is(err, ErrDebugEvaluationCancelled) {
			return signal{kind: "debugCancelled"}, true
		}
		var thrown thrownError
		if errors.As(err, &thrown) {
			errValue := e.withTrace(thrown.value)
			return signal{kind: "throw", thrown: &errValue}, true
		}
		errValue := e.withTrace(runtime.NewRecoverableError(err))
		return signal{kind: "throw", thrown: &errValue}, true
	}
	if sig.kind != "" || sig.exited {
		return sig, true
	}
	return signal{}, false
}

// evalTopLevelStatements runs declarations (in source order) before imperative
// statements so top-level code can call a function declared later, matching the
// VM's compile-time hoisting.
func (e *Evaluator) evalTopLevelStatements(stmts []ast.Statement, env *runtime.Environment) (signal, error) {
	for _, stmt := range stmts {
		if !isHoistedDeclaration(stmt) {
			continue
		}
		if sig, stop := e.evalStatementInList(stmt, env); stop {
			return sig, nil
		}
		// Only top-level functions are reflectable by bare name (matches
		// the VM); nested declarations never reach this pass.
		e.registerTopLevelFunction(stmt, env)
	}
	for _, stmt := range stmts {
		if isHoistedDeclaration(stmt) {
			continue
		}
		if sig, stop := e.evalStatementInList(stmt, env); stop {
			return sig, nil
		}
	}
	return signal{}, nil
}

func isHoistedDeclaration(stmt ast.Statement) bool {
	switch s := stmt.(type) {
	case *ast.ModuleStatement, *ast.ImportStatement, *ast.FromImportStatement,
		*ast.TypeAliasStatement, *ast.FunctionStatement:
		return true
	case *ast.ExportStatement:
		return isHoistedDeclaration(s.Statement)
	}
	return false
}

func (e *Evaluator) evalStatement(stmt ast.Statement, env *runtime.Environment) (signal, error) {
	if line := statementLine(stmt); line > 0 {
		e.currentLine = line
	}
	switch stmt := stmt.(type) {
	case *ast.ModuleStatement:
		return signal{}, nil
	case *ast.ImportStatement:
		return e.evalImportStatement(stmt, env)
	case *ast.FromImportStatement:
		return e.evalFromImportStatement(stmt, env)
	case *ast.ExportStatement:
		return e.evalStatement(stmt.Statement, env)
	case *ast.InitStatement:
		return e.evalBlock(stmt.Body, env)
	case *ast.TypeAliasStatement:
		e.typeAliases[strings.ToLower(stmt.Name.Value)] = e.resolveTypeRef(stmt.Type)
		return signal{}, nil
	case *ast.DeclarationStatement:
		value := runtime.Value(runtime.Null{})
		expectedType := e.resolveTypeRef(stmt.Type)
		if stmt.Value != nil {
			prevDecl := e.declAnnotation
			e.declAnnotation = expectedType
			evaluated, err := e.evalExpressionWithExpectedType(stmt.Value, env, expectedType)
			e.declAnnotation = prevDecl
			if err != nil {
				return signal{}, err
			}
			value = evaluated
			if exit, ok := value.(exitValue); ok {
				return signal{exited: true, exitCode: exit.code}, nil
			}
			// Only enforce generic collection element types on declarations (e.g. list<int>, int[]).
			// Scalar type coercions (float/decimal, int widening, etc.) are handled elsewhere.
			if expectedType != nil && expectedType.Operator == "" {
				isListAlias := expectedType.ListAlias && len(expectedType.Arguments) == 0
				hasGenericArgs := len(expectedType.Arguments) > 0
				if hasGenericArgs || isListAlias {
					typeName := strings.ToLower(simpleTypeName(expectedType.Name))
					if typeName == "list" || expectedType.ListAlias || typeName == "set" || typeName == "dict" {
						if !matchValueToTypeRef(nil, value, expectedType) {
							suffix := collectionMismatchSuffix(value, expectedType)
							// When a suffix identifies the specific bad element, use the base type
							// name to avoid a misleading "list<int> to list<int>" message.
							gotName := descriptiveTypeName(value)
							if suffix != "" {
								gotName = value.TypeName()
							}
							return signal{}, fmt.Errorf("type error: cannot assign %s to %s%s", gotName, expectedType.String(), suffix)
						}
						// Attach the reified element-type tag so subsequent
						// reflect.typeBindings() and `instanceof list<T>`
						// checks see the declared bindings rather than a
						// bare `list`. Untagged collections retain their
						// nil ElementTypes.
						value = tagCollectionWithTypeRef(value, expectedType)
					}
				}
			}
		}
		return signal{}, env.Define(stmt.Name.Value, value, stmt.Kind == "const" || stmt.Kind == "static const")
	case *ast.DestructuringStatement:
		return e.evalDestructuringStatement(stmt, env)
	case *ast.FunctionStatement:
		if stmt.Static {
			return signal{}, fmt.Errorf("static functions are parsed but not evaluated yet")
		}
		fn := runtime.Function{Name: stmt.Name.Value, Doc: stmt.Doc, TypeParameters: typeParameterNames(stmt.Generics), TypeParamConstraints: typeParamConstraints(stmt.Generics), Parameters: e.resolveParameters(stmt.Parameters), ReturnType: e.resolveTypeRef(stmt.ReturnType), Body: stmt.Body, Env: env, Decorators: stmt.Decorators, Target: "function", Async: stmt.Async, IsGenerator: blockContainsYield(stmt.Body), DefinitionModule: e.currentModule, DefinitionLine: stmt.Token.Line, DefinitionColumn: stmt.Token.Column}
		decorated, err := e.applyCallableFunctionDecorators(fn, stmt.Decorators, env)
		if err != nil {
			return signal{}, err
		}
		fn = decorated
		return signal{}, env.DefineFunction(stmt.Name.Value, fn)
	case *ast.ExpressionStatement:
		if stmt.Expression == nil {
			return signal{}, nil
		}
		value, err := e.evalExpression(stmt.Expression, env)
		if err != nil {
			return signal{}, err
		}
		if exit, ok := value.(exitValue); ok {
			return signal{exited: true, exitCode: exit.code}, nil
		}
		return signal{}, nil
	case *ast.IfStatement:
		condition, err := e.evalBoolCondition(stmt.Condition, env)
		if err != nil {
			return signal{}, err
		}
		if condition {
			return e.evalBlock(stmt.Consequence, env)
		}
		for _, elseif := range stmt.ElseIfs {
			condition, err := e.evalBoolCondition(elseif.Condition, env)
			if err != nil {
				return signal{}, err
			}
			if condition {
				return e.evalBlock(elseif.Body, env)
			}
		}
		if stmt.Alternative != nil {
			return e.evalBlock(stmt.Alternative, env)
		}
		return signal{}, nil
	case *ast.WhileStatement:
		for {
			if e.debugEvaluationCancelled() {
				return signal{kind: "debugCancelled"}, nil
			}
			condition, err := e.evalBoolCondition(stmt.Condition, env)
			if err != nil {
				return signal{}, err
			}
			if !condition {
				return signal{}, nil
			}
			sig, err := e.evalBlock(stmt.Body, env)
			if err != nil {
				return signal{}, err
			}
			switch sig.kind {
			case "break":
				return signal{}, nil
			case "continue":
				continue
			case "":
			default:
				return sig, nil
			}
			if sig.exited {
				return sig, nil
			}
		}
	case *ast.ForStatement:
		return e.evalForStatement(stmt, env)
	case *ast.SimpleStatement:
		switch stmt.Kind {
		case "break", "continue":
			return signal{kind: stmt.Kind}, nil
		case "defer":
			frozen, err := e.freezeDeferredCall(stmt.Value, env)
			if err != nil {
				return signal{}, err
			}
			e.registerDefer(frozen, env)
			return signal{}, nil
		case "throw":
			if stmt.Token.Line > 0 {
				e.currentLine = stmt.Token.Line
			}
			value, err := e.evalExpression(stmt.Value, env)
			if err != nil {
				return signal{}, err
			}
			errValue, ok := value.(runtime.Error)
			if !ok {
				return signal{}, fmt.Errorf("throw expects Error, got %s", value.TypeName())
			}
			errValue = e.withTrace(errValue)
			return signal{kind: "throw", thrown: &errValue}, nil
		default:
			return signal{}, fmt.Errorf("unsupported simple statement %q", stmt.Kind)
		}
	case *ast.ReturnStatement:
		value := runtime.Value(runtime.Null{})
		if stmt.Value != nil {
			var expected *ast.TypeRef
			if fn := e.currentFunction(); fn != nil {
				expected = fn.ReturnType
			}
			evaluated, err := e.evalExpressionWithExpectedType(stmt.Value, env, expected)
			if err != nil {
				return signal{}, err
			}
			value = evaluated
		}
		return signal{kind: "return", value: value}, nil
	case *ast.YieldStatement:
		value := runtime.Value(runtime.Null{})
		if stmt.Value != nil {
			evaluated, err := e.evalExpression(stmt.Value, env)
			if err != nil {
				return signal{}, err
			}
			value = evaluated
		}
		closed, err := e.appendYield(value)
		if err != nil {
			return signal{}, err
		}
		if closed {
			return signal{kind: "generatorClosed"}, nil
		}
		return signal{}, nil
	case *ast.TryStatement:
		return e.evalTryStatement(stmt, env)
	case *ast.WithStatement:
		return e.evalWithStatement(stmt, env)
	case *ast.DelStatement:
		return signal{}, e.evalDelStatement(stmt, env)
	case *ast.MatchStatement:
		return e.evalMatchStatement(stmt, env)
	case *ast.SelectStatement:
		return e.evalSelectStatement(stmt, env)
	case *ast.ClassStatement:
		class, err := e.buildClass(stmt, env)
		if err != nil {
			return signal{}, err
		}
		callableDecorators := make([]ast.Decorator, 0, len(stmt.Decorators))
		for _, dec := range stmt.Decorators {
			if dec.Name.Value == "immutable" && len(dec.Arguments) == 0 {
				class.Immutable = true
			} else {
				callableDecorators = append(callableDecorators, dec)
			}
		}
		decorated, err := e.applyCallableClassDecorators(class, callableDecorators, env)
		if err != nil {
			return signal{}, err
		}
		if decoratedClass, ok := decorated.(*runtime.Class); ok {
			class = decoratedClass
		} else {
			e.decoratedClassIdents[stmt.Name.Value] = class.Name
		}
		e.registerGlobalClass(class)
		return signal{}, env.Define(stmt.Name.Value, decorated, true)
	case *ast.InterfaceStatement:
		iface, err := e.buildInterface(stmt, env)
		if err != nil {
			return signal{}, err
		}
		return signal{}, env.Define(stmt.Name.Value, iface, true)
	case *ast.EnumStatement:
		enum, err := e.buildEnum(stmt, env)
		if err != nil {
			return signal{}, err
		}
		return signal{}, env.Define(stmt.Name.Value, enum, true)
	default:
		return signal{}, fmt.Errorf("unsupported statement %T", stmt)
	}
}

func (e *Evaluator) evalBlock(block *ast.BlockStatement, outer *runtime.Environment) (signal, error) {
	if block == nil {
		return signal{}, nil
	}
	if !block.DeclaresBindings() {
		// Nothing in the block can bind a name in its own scope, so an
		// enclosed environment would only add an empty chain hop.
		return e.evalStatements(block.Statements, outer)
	}
	return e.evalStatements(block.Statements, runtime.NewEnclosedEnvironment(outer))
}

func (e *Evaluator) evalExpression(expr ast.Expression, env *runtime.Environment) (runtime.Value, error) {
	return e.evalExpressionWithExpectedType(expr, env, nil)
}

func (e *Evaluator) evalExpressionWithExpectedType(expr ast.Expression, env *runtime.Environment, expected *ast.TypeRef) (runtime.Value, error) {
	switch expr := expr.(type) {
	case *ast.PrecomputedValue:
		if v, ok := expr.Value.(runtime.Value); ok {
			return v, nil
		}
		return runtime.Null{}, nil
	case *ast.Identifier:
		if value, ok := env.Get(expr.Value); ok {
			return value, nil
		}
		if runtime.IsBuiltinTypeName(expr.Value) {
			return runtime.Type{Name: strings.ToLower(expr.Value)}, nil
		}
		return nil, fmt.Errorf("%q is not declared", expr.Value)
	case *ast.IntegerLiteral:
		parsed, err := expr.ParsedValue()
		if err != nil {
			return nil, err
		}
		value := runtime.Int{Value: parsed}
		if expected != nil && expected.Operator == "" {
			switch expected.Name {
			case "decimal":
				return intToDecimal(value), nil
			case "float":
				f, _ := new(big.Rat).SetInt(value.Value).Float64()
				return runtime.Float{Value: f}, nil
			}
		}
		return value, nil
	case *ast.DecimalLiteral:
		parsed, err := expr.ParsedValue()
		if err != nil {
			return nil, err
		}
		return runtime.Decimal{Value: parsed}, nil
	case *ast.FloatLiteral:
		parsed, err := expr.ParsedValue()
		if err != nil {
			return nil, err
		}
		return runtime.Float{Value: parsed}, nil
	case *ast.StringLiteral:
		return runtime.String{Value: expr.Value}, nil
	case *ast.InterpolatedString:
		var sb strings.Builder
		for _, part := range expr.Parts {
			val, err := e.evalExpression(part, env)
			if err != nil {
				return nil, err
			}
			if s, ok := val.(runtime.String); ok {
				sb.WriteString(s.Value)
			} else {
				text, err := e.displayString(val)
				if err != nil {
					return nil, err
				}
				sb.WriteString(text)
			}
		}
		return runtime.String{Value: sb.String()}, nil
	case *ast.FormattedInterpolation:
		val, err := e.evalExpression(expr.Value, env)
		if err != nil {
			return nil, err
		}
		formatted, err := native.FormatValueWithSpec(val, expr.Spec)
		if err != nil {
			return nil, err
		}
		return runtime.String{Value: formatted}, nil
	case *ast.Literal:
		switch v := expr.Value.(type) {
		case bool:
			return runtime.Bool{Value: v}, nil
		case nil:
			return runtime.Null{}, nil
		default:
			return nil, fmt.Errorf("unsupported literal %v", expr.Value)
		}
	case *ast.ListLiteral:
		elements := make([]runtime.Value, 0, len(expr.Elements))
		for _, element := range expr.Elements {
			if spread, ok := element.(*ast.SpreadExpression); ok {
				value, err := e.evalExpression(spread.Value, env)
				if err != nil {
					return nil, err
				}
				list, ok := value.(*runtime.List)
				if !ok {
					return nil, fmt.Errorf("spread element must be a list")
				}
				elements = append(elements, list.Elements...)
				continue
			}
			value, err := e.evalExpression(element, env)
			if err != nil {
				return nil, err
			}
			elements = append(elements, value)
		}
		return &runtime.List{Elements: elements}, nil
	case *ast.DictLiteral:
		d := runtime.NewDict()
		for _, entry := range expr.Entries {
			if entry.Spread {
				src, err := e.evalExpression(entry.Value, env)
				if err != nil {
					return nil, err
				}
				srcDict, ok := src.(runtime.Dict)
				if !ok {
					return nil, fmt.Errorf("dict literal spread source must be dict, got %s", src.TypeName())
				}
				srcDict.ForEachEntry(func(k string, srcEntry runtime.DictEntry) bool {
					d.PutEntry(k, runtime.DictEntry{Key: srcEntry.Key, Value: srcEntry.Value})
					return true
				})
				continue
			}
			key, err := e.evalExpression(entry.Key, env)
			if err != nil {
				return nil, err
			}
			value, err := e.evalExpression(entry.Value, env)
			if err != nil {
				return nil, err
			}
			d.PutEntry(dictKey(key), runtime.DictEntry{Key: key, Value: value})
		}
		return d, nil
	case *ast.SetLiteral:
		elements := map[string]runtime.SetEntry{}
		for _, element := range expr.Elements {
			if spread, ok := element.(*ast.SpreadExpression); ok {
				src, err := e.evalExpression(spread.Value, env)
				if err != nil {
					return nil, err
				}
				switch s := src.(type) {
				case runtime.Set:
					for k, entry := range s.Elements {
						elements[k] = entry
					}
				case *runtime.List:
					for _, v := range s.Elements {
						elements[dictKey(v)] = runtime.SetEntry{Value: v}
					}
				default:
					return nil, fmt.Errorf("set literal spread source must be set or list, got %s", src.TypeName())
				}
				continue
			}
			value, err := e.evalExpression(element, env)
			if err != nil {
				return nil, err
			}
			elements[dictKey(value)] = runtime.SetEntry{Value: value}
		}
		return runtime.Set{Elements: elements}, nil
	case *ast.RangeExpression:
		return e.evalRangeExpression(expr, env)
	case *ast.PrefixExpression:
		right, err := e.evalExpression(expr.Right, env)
		if err != nil {
			return nil, err
		}
		return e.evalPrefix(expr.Operator, right)
	case *ast.PostfixExpression:
		return e.evalIncrement(expr.Left, expr.Operator, env)
	case *ast.InfixExpression:
		if expr.Operator == "instanceof" {
			left, err := e.evalExpression(expr.Left, env)
			if err != nil {
				return nil, err
			}
			typeName, err := typeNameFromExpression(expr.Right)
			if err != nil {
				return nil, err
			}
			fromTypeParam := false
			if bound, ok := env.GetTypeBinding(typeName); ok {
				typeName = bound
				fromTypeParam = true
			} else if base, args, isGeneric := splitGenericTypeName(typeName); isGeneric {
				// Resolve type-param names in the argument list so `x instanceof Box<T>` works in a generic frame.
				resolvedAny := false
				for i, a := range args {
					if b, ok2 := env.GetTypeBinding(strings.TrimSpace(a)); ok2 {
						args[i] = b
						resolvedAny = true
					}
				}
				if resolvedAny {
					typeName = base + "<" + strings.Join(args, ",") + ">"
				}
			}
			// `instanceof T` (bound type param) stays name-based to match the VM, which resolves T on the legacy path.
			if !fromTypeParam {
				if result, handled := e.instanceofExact(left, typeName, env); handled {
					return runtime.Bool{Value: result}, nil
				}
			}
			return runtime.Bool{Value: e.valueMatchesType(left, typeName)}, nil
		}
		if expr.Operator == "&&" || expr.Operator == "||" {
			left, err := e.evalExpression(expr.Left, env)
			if err != nil {
				return nil, err
			}
			leftBool, ok := left.(runtime.Bool)
			if !ok {
				return nil, fmt.Errorf("%s expects bool operands", expr.Operator)
			}
			if expr.Operator == "&&" && !leftBool.Value {
				return runtime.Bool{Value: false}, nil
			}
			if expr.Operator == "||" && leftBool.Value {
				return runtime.Bool{Value: true}, nil
			}
			right, err := e.evalExpression(expr.Right, env)
			if err != nil {
				return nil, err
			}
			rightBool, ok := right.(runtime.Bool)
			if !ok {
				return nil, fmt.Errorf("%s expects bool operands", expr.Operator)
			}
			return runtime.Bool{Value: rightBool.Value}, nil
		}
		if expr.Operator == "??" {
			left, err := e.evalExpression(expr.Left, env)
			if err != nil {
				return nil, err
			}
			if _, isNull := left.(runtime.Null); !isNull {
				return left, nil
			}
			return e.evalExpression(expr.Right, env)
		}
		left, err := e.evalExpression(expr.Left, env)
		if err != nil {
			return nil, err
		}
		right, err := e.evalExpression(expr.Right, env)
		if err != nil {
			return nil, err
		}
		return e.evalInfix(expr.Operator, left, right)
	case *ast.AssignmentExpression:
		value, err := e.evalExpression(expr.Value, env)
		if err != nil {
			return nil, err
		}
		switch left := expr.Left.(type) {
		case *ast.Identifier:
			if err := env.Assign(left.Value, value); err != nil {
				return nil, err
			}
		case *ast.IndexExpression:
			if err := e.assignIndex(left, value, env); err != nil {
				return nil, err
			}
		case *ast.SelectorExpression:
			if err := e.assignSelector(left, value, env); err != nil {
				return nil, err
			}
		case *ast.ListLiteral:
			list, ok := value.(*runtime.List)
			if !ok {
				return nil, fmt.Errorf("cannot list-destructure %s", value.TypeName())
			}
			if len(list.Elements) < len(left.Elements) {
				return nil, fmt.Errorf("list has %d elements, destructuring expects %d", len(list.Elements), len(left.Elements))
			}
			for i, element := range left.Elements {
				ident, ok := element.(*ast.Identifier)
				if !ok {
					return nil, fmt.Errorf("list destructuring target must be identifier")
				}
				if err := env.Assign(ident.Value, list.Elements[i]); err != nil {
					return nil, err
				}
			}
		default:
			return nil, fmt.Errorf("unsupported assignment target %T", expr.Left)
		}
		return value, nil
	case *ast.CallExpression:
		return e.evalCallWithExpectedType(expr, env, expected)
	case *ast.IndexExpression:
		return e.evalIndexExpression(expr, env)
	case *ast.SelectorExpression:
		return e.evalSelectorExpression(expr, env)
	case *ast.CastExpression:
		value, err := e.evalExpression(expr.Value, env)
		if err != nil {
			return nil, err
		}
		target := e.resolveTypeRef(expr.Type)
		// Nullable cast: `null as ?T` is null; `value as ?T` for
		// non-null falls through to the underlying type's cast logic.
		// Must check before the class-chain match below, otherwise
		// null/T mismatches reject what the user explicitly opted into
		// by writing the `?` prefix.
		if target.Nullable {
			if _, isNull := value.(runtime.Null); isNull {
				return runtime.Null{}, nil
			}
		}
		// Class / interface / parent-chain widening: a value whose
		// class chain contains the target (with the module prefix
		// stripped) is already an instance of the target, so cast
		// is a no-op. Falls through to castValue for primitive
		// conversions when the chain doesn't match.
		if e.valueMatchesType(value, target.Name) {
			return value, nil
		}
		if instance, ok := value.(*runtime.Instance); ok {
			if dunder := castDunderName(target.Name); dunder != "" {
				if result, handled, err := e.invokeInstanceMethod(instance, dunder, nil); err != nil {
					return nil, err
				} else if handled {
					if err := checkCastDunderReturn(target.Name, result); err != nil {
						return nil, thrownError{value: e.withTrace(runtime.Error{Class: "RuntimeError", Message: err.Error()})}
					}
					return result, nil
				}
			}
		}
		return castValue(value, target.Name)
	case *ast.FunctionLiteral:
		// Capture the enclosing scope's type bindings so an inner `T x`
		// parameter or `instanceof T` check can resolve against the
		// outer generic frame's concrete bindings. snapshotTypeBindings
		// walks the env chain so all reachable bindings come along.
		captured := snapshotEnvTypeBindings(env)
		return runtime.Function{Parameters: expr.Parameters, ReturnType: e.resolveTypeRef(expr.ReturnType), Body: expr.Body, Env: env, Async: expr.Async, IsGenerator: blockContainsYield(expr.Body), TypeBindings: captured}, nil
	case *ast.AwaitExpression:
		value, err := e.evalExpression(expr.Value, env)
		if err != nil {
			return nil, err
		}
		return awaitValue(value)
	case *ast.MatchExpression:
		return e.evalMatchExpression(expr, env)
	case *ast.PipeExpression:
		call, ok := ast.LowerPipe(expr)
		if !ok {
			return nil, fmt.Errorf("`|>` right side must be a call, identifier, or selector")
		}
		return e.evalExpression(call, env)
	case *ast.PartialExpression:
		return e.evalExpression(ast.LowerPartial(expr), env)
	case *ast.ListComprehension:
		acc := []runtime.Value{}
		if err := e.walkComprehensionClauses(expr.Clauses, 0, env, func(itEnv *runtime.Environment) error {
			v, err := e.evalExpression(expr.Body, itEnv)
			if err != nil {
				return err
			}
			acc = append(acc, v)
			return nil
		}); err != nil {
			return nil, err
		}
		return &runtime.List{Elements: acc}, nil
	case *ast.SetComprehension:
		elements := map[string]runtime.SetEntry{}
		if err := e.walkComprehensionClauses(expr.Clauses, 0, env, func(itEnv *runtime.Environment) error {
			v, err := e.evalExpression(expr.Body, itEnv)
			if err != nil {
				return err
			}
			elements[dictKey(v)] = runtime.SetEntry{Value: v}
			return nil
		}); err != nil {
			return nil, err
		}
		return runtime.Set{Elements: elements}, nil
	case *ast.DictComprehension:
		out := runtime.NewDict()
		if err := e.walkComprehensionClauses(expr.Clauses, 0, env, func(itEnv *runtime.Environment) error {
			key, err := e.evalExpression(expr.KeyBody, itEnv)
			if err != nil {
				return err
			}
			val, err := e.evalExpression(expr.ValueBody, itEnv)
			if err != nil {
				return err
			}
			out.PutEntry(dictKey(key), runtime.DictEntry{Key: key, Value: val})
			return nil
		}); err != nil {
			return nil, err
		}
		return out, nil
	case *ast.TernaryExpression:
		b, err := e.evalBoolCondition(expr.Condition, env)
		if err != nil {
			return nil, err
		}
		if b {
			return e.evalExpression(expr.ThenExpr, env)
		}
		return e.evalExpression(expr.ElseExpr, env)
	default:
		return nil, fmt.Errorf("unsupported expression %T", expr)
	}
}

func (e *Evaluator) evalSelectStatement(stmt *ast.SelectStatement, env *runtime.Environment) (signal, error) {
	handles := make([]*native.ChannelHandle, 0, len(stmt.Cases))
	kinds := make([]string, 0, len(stmt.Cases))
	sendValues := make([]runtime.Value, 0, len(stmt.Cases))
	for _, c := range stmt.Cases {
		chanValue, err := e.evalExpression(c.Channel, env)
		if err != nil {
			return signal{}, err
		}
		handle, err := selectChannelHandle(chanValue)
		if err != nil {
			return signal{}, err
		}
		handles = append(handles, handle)
		kinds = append(kinds, c.Kind)
		if c.Kind == "send" {
			sendValue, err := e.evalExpression(c.Value, env)
			if err != nil {
				return signal{}, err
			}
			sendValues = append(sendValues, sendValue)
		} else {
			sendValues = append(sendValues, nil)
		}
	}
	chosen, recvValue, err := native.SelectChannels(handles, kinds, sendValues, stmt.Default != nil)
	if err != nil {
		return signal{}, fmt.Errorf("select: %s", err.Error())
	}
	if chosen == -1 {
		return e.evalBlock(stmt.Default, env)
	}
	caseEnv := runtime.NewEnclosedEnvironment(env)
	c := stmt.Cases[chosen]
	if c.Kind == "recv" && c.Binding != "" {
		if err := caseEnv.Define(c.Binding, recvValue, true); err != nil {
			return signal{}, err
		}
	}
	return e.evalBlock(c.Body, caseEnv)
}

func selectChannelHandle(v runtime.Value) (*native.ChannelHandle, error) {
	instance, ok := v.(*runtime.Instance)
	if !ok {
		return nil, fmt.Errorf("select case channel must be a Channel instance")
	}
	handleValue, ok := instance.Fields["_h"]
	if !ok {
		return nil, fmt.Errorf("select case channel is not a Channel (no _h field)")
	}
	h, ok := native.ChannelHandleFromValue(handleValue)
	if !ok {
		return nil, fmt.Errorf("select case channel handle is invalid")
	}
	return h, nil
}

func (e *Evaluator) evalMatchStatement(stmt *ast.MatchStatement, env *runtime.Environment) (signal, error) {
	value, err := e.evalExpression(stmt.Expr, env)
	if err != nil {
		return signal{}, err
	}
	for _, c := range stmt.Cases {
		caseEnv, ok, err := e.matchCase(value, c, env)
		if err != nil {
			return signal{}, err
		}
		if !ok {
			continue
		}
		if c.Body == nil {
			// Arrow-bodied arm in statement position: run the action,
			// discard the value.
			if c.Value != nil {
				if _, err := e.evalExpression(c.Value, caseEnv); err != nil {
					return signal{}, err
				}
			}
			return signal{}, nil
		}
		return e.evalBlock(c.Body, caseEnv)
	}
	hint := matchExhaustivenessHint(stmt.Cases)
	msg := fmt.Sprintf("%s; got %s (type: %s)", hint, value.Inspect(), value.TypeName())
	errValue := e.withTrace(runtime.Error{Class: "MatchError", Message: msg})
	return signal{kind: "throw", thrown: &errValue}, nil
}

func (e *Evaluator) evalMatchExpression(expr *ast.MatchExpression, env *runtime.Environment) (runtime.Value, error) {
	value, err := e.evalExpression(expr.Expr, env)
	if err != nil {
		return nil, err
	}
	for _, c := range expr.Cases {
		caseEnv, ok, err := e.matchCase(value, c, env)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		if c.Value == nil {
			return runtime.Null{}, nil
		}
		return e.evalExpression(c.Value, caseEnv)
	}
	hint := matchExhaustivenessHint(expr.Cases)
	msg := fmt.Sprintf("%s; got %s (type: %s)", hint, value.Inspect(), value.TypeName())
	return nil, thrownError{value: runtime.Error{Class: "MatchError", Message: msg}}
}

func matchExhaustivenessHint(cases []ast.MatchCase) string {
	for _, mc := range cases {
		if mc.Default && mc.Guard == nil {
			return "no match case matched"
		}
	}
	return "no match case matched (add a 'default:' case to handle all values)"
}

func (e *Evaluator) matchCase(value runtime.Value, c ast.MatchCase, env *runtime.Environment) (*runtime.Environment, bool, error) {
	caseEnv := runtime.NewEnclosedEnvironment(env)
	matched := false
	switch {
	case c.Default:
		matched = true
	case c.EnumVariant != nil:
		ev := c.EnumVariant
		enumVal, ok := value.(runtime.EnumVariant)
		if ok && strings.EqualFold(enumVal.Enum.Name, ev.Enum.Value) && strings.EqualFold(enumVal.Variant, ev.Variant.Value) {
			matched = true
			for i, param := range ev.Params {
				if i >= len(enumVal.Fields) {
					break
				}
				if err := caseEnv.Define(param.Name.Value, enumVal.Fields[i], false); err != nil {
					return nil, false, err
				}
			}
		}
	case c.Type != nil:
		matched = valueMatchesType(value, c.Type.String())
		if matched && c.Name != nil {
			if err := caseEnv.Define(c.Name.Value, value, false); err != nil {
				return nil, false, err
			}
		}
	case c.ListPattern != nil:
		list, ok := value.(*runtime.List)
		if !ok {
			matched = false
			break
		}
		if len(list.Elements) != len(c.ListPattern.Bindings) {
			matched = false
			break
		}
		matched = true
		for i, binding := range c.ListPattern.Bindings {
			elem := list.Elements[i]
			if binding.Literal != nil {
				pattern, err := e.evalExpression(binding.Literal, env)
				if err != nil {
					return nil, false, err
				}
				equal, err := e.evalInfix("==", elem, pattern)
				if err != nil {
					return nil, false, err
				}
				if !equal.(runtime.Bool).Value {
					matched = false
					break
				}
				continue
			}
			if binding.Type != nil && !e.valueMatchesType(elem, binding.Type.Name) {
				matched = false
				break
			}
			if binding.Name != nil && binding.Name.Value != "_" {
				if err := caseEnv.Define(binding.Name.Value, elem, false); err != nil {
					return nil, false, err
				}
			}
		}
	case c.Pattern != nil:
		pattern, err := e.evalExpression(c.Pattern, env)
		if err != nil {
			return nil, false, err
		}
		equal, err := e.evalInfix("==", value, pattern)
		if err != nil {
			return nil, false, err
		}
		matched = equal.(runtime.Bool).Value
	}
	if !matched && len(c.Alternates) > 0 {
		for _, alt := range c.Alternates {
			altMatched, err := e.matchOrAlternate(value, alt, env)
			if err != nil {
				return nil, false, err
			}
			if altMatched {
				matched = true
				break
			}
		}
	}
	if !matched {
		return nil, false, nil
	}
	if c.Guard != nil {
		guard, err := e.evalBoolCondition(c.Guard, caseEnv)
		if err != nil {
			return nil, false, err
		}
		if !guard {
			return nil, false, nil
		}
	}
	return caseEnv, true, nil
}

// matchOrAlternate tests a single or-pattern alternate against a value.
// Bare type names (e.g. `int`, stored as an Identifier) test the value's
// type; everything else is evaluated and compared via `==`. Alternates
// never bind, so no env mutation happens here.
func (e *Evaluator) matchOrAlternate(value runtime.Value, alt ast.Expression, env *runtime.Environment) (bool, error) {
	if ident, ok := alt.(*ast.Identifier); ok && isBuiltinTypeNameForMatch(ident.Value) {
		return e.valueMatchesType(value, ident.Value), nil
	}
	patternVal, err := e.evalExpression(alt, env)
	if err != nil {
		return false, err
	}
	eq, err := e.evalInfix("==", value, patternVal)
	if err != nil {
		return false, err
	}
	if b, ok := eq.(runtime.Bool); ok {
		return b.Value, nil
	}
	return false, nil
}

func isBuiltinTypeNameForMatch(name string) bool {
	switch name {
	case "string", "int", "float", "decimal", "bool", "bytes", "list", "dict", "set", "range", "null":
		return true
	}
	return false
}

func syntheticCall(name string) *ast.CallExpression {
	return &ast.CallExpression{Callee: &ast.Identifier{Value: name}}
}

func currentInstance(env *runtime.Environment) (*runtime.Instance, error) {
	value, ok := env.Get("this")
	if !ok {
		return nil, fmt.Errorf("parent is only available inside methods")
	}
	instance, ok := value.(*runtime.Instance)
	if !ok {
		return nil, fmt.Errorf("this is not an instance")
	}
	return instance, nil
}

func blockContainsYield(block *ast.BlockStatement) bool {
	if block == nil {
		return false
	}
	for _, stmt := range block.Statements {
		if statementContainsYield(stmt) {
			return true
		}
	}
	return false
}

func statementContainsYield(stmt ast.Statement) bool {
	switch stmt := stmt.(type) {
	case *ast.YieldStatement:
		return true
	case *ast.IfStatement:
		if blockContainsYield(stmt.Consequence) || blockContainsYield(stmt.Alternative) {
			return true
		}
		for _, elseif := range stmt.ElseIfs {
			if blockContainsYield(elseif.Body) {
				return true
			}
		}
	case *ast.WhileStatement:
		return blockContainsYield(stmt.Body)
	case *ast.ForStatement:
		return blockContainsYield(stmt.Body)
	case *ast.TryStatement:
		if blockContainsYield(stmt.Body) || blockContainsYield(stmt.Finally) {
			return true
		}
		for _, catch := range stmt.Catches {
			if blockContainsYield(catch.Body) {
				return true
			}
		}
	case *ast.MatchStatement:
		for _, matchCase := range stmt.Cases {
			if blockContainsYield(matchCase.Body) {
				return true
			}
		}
	}
	return false
}

func (e *Evaluator) evalForStatement(stmt *ast.ForStatement, env *runtime.Environment) (signal, error) {
	loopEnv := runtime.NewEnclosedEnvironment(env)
	if stmt.Iterable != nil {
		return e.evalForInStatement(stmt, loopEnv)
	}
	if stmt.Init != nil {
		if _, err := e.evalStatement(stmt.Init, loopEnv); err != nil {
			return signal{}, err
		}
	}
	for {
		if e.debugEvaluationCancelled() {
			return signal{kind: "debugCancelled"}, nil
		}
		if stmt.Condition != nil {
			condition, err := e.evalBoolCondition(stmt.Condition, loopEnv)
			if err != nil {
				return signal{}, err
			}
			if !condition {
				return signal{}, nil
			}
		}
		sig, err := e.evalBlock(stmt.Body, loopEnv)
		if err != nil {
			return signal{}, err
		}
		switch sig.kind {
		case "break":
			return signal{}, nil
		case "continue", "":
		default:
			return sig, nil
		}
		if sig.exited {
			return sig, nil
		}
		if stmt.Update != nil {
			if _, err := e.evalStatement(stmt.Update, loopEnv); err != nil {
				return signal{}, err
			}
		}
	}
}

func (e *Evaluator) evalTryStatement(stmt *ast.TryStatement, env *runtime.Environment) (signal, error) {
	sig, err := e.evalBlock(stmt.Body, env)
	if err != nil {
		return signal{}, err
	}
	if sig.kind == "throw" && sig.thrown != nil && !sig.thrown.IsFatal() {
		handled := false
		for _, clause := range stmt.Catches {
			if !e.catchMatches(clause, *sig.thrown) {
				continue
			}
			catchEnv := runtime.NewEnclosedEnvironment(env)
			if clause.Name != nil {
				if err := catchEnv.Define(clause.Name.Value, *sig.thrown, false); err != nil {
					return signal{}, err
				}
			}
			sig, err = e.evalBlock(clause.Body, catchEnv)
			if err != nil {
				return signal{}, err
			}
			handled = true
			break
		}
		if !handled {
			// Preserve the original throw if no catch matches.
			sig = signal{kind: "throw", thrown: sig.thrown}
		}
	}
	if stmt.Finally != nil {
		finallySig, err := e.evalBlock(stmt.Finally, env)
		if err != nil {
			return signal{}, err
		}
		if finallySig.kind != "" || finallySig.exited {
			return finallySig, nil
		}
	}
	return sig, nil
}

// evalWithStatement implements `with (expr) { ... }` and
// `with (name = expr) { ... }`. The bound value's `__enter__()`
// (if defined) supplies the binding; otherwise the binding is the
// expression itself. At any block exit - normal completion,
// exception, return, break, or continue - the runtime invokes
// `__exit__()` when present, else the class destructor
// (`~ClassName`) when present, else nothing. Errors from cleanup
// shadow a clean exit but defer to an in-flight exception.
func (e *Evaluator) evalWithStatement(stmt *ast.WithStatement, env *runtime.Environment) (signal, error) {
	resource, err := e.evalExpression(stmt.Value, env)
	if err != nil {
		return signal{}, err
	}
	bound := resource
	if instance, ok := resource.(*runtime.Instance); ok {
		if method, ok := lookupDunderEval(instance.Class, "__enter", "__enter__"); ok {
			value, err := e.applyFunctionWithThis(method, nil, instance)
			if err != nil {
				return signal{}, err
			}
			bound = value
		}
	}
	scope := runtime.NewEnclosedEnvironment(env)
	if stmt.Name != nil {
		if err := scope.Define(stmt.Name.Value, bound, false); err != nil {
			return signal{}, err
		}
	}
	sig, bodyErr := e.evalBlock(stmt.Body, scope)
	cleanupErr := e.runWithCleanup(resource)
	// __exit raising replaces any body outcome (Python __exit__ semantics), matching the VM.
	if cleanupErr != nil {
		return signal{}, cleanupErr
	}
	if bodyErr != nil {
		return signal{}, bodyErr
	}
	return sig, nil
}

// evalDelStatement implements `del x`. Looks up the binding,
// invokes the destructor (when the bound value is a class
// instance whose class declares one and hasn't already been
// destroyed), removes the entry from `e.destructibleInstances`,
// and removes the binding from the surrounding environment.
func (e *Evaluator) evalDelStatement(stmt *ast.DelStatement, env *runtime.Environment) error {
	if stmt.Target == nil {
		return fmt.Errorf("del requires an identifier")
	}
	name := stmt.Target.Value
	value, ok := env.Get(name)
	if !ok {
		return fmt.Errorf("del: unknown identifier %q", name)
	}
	if instance, ok := value.(*runtime.Instance); ok && instance != nil && !instance.Destroyed && instance.Class != nil && instance.Class.Destructor != nil {
		// Mark destroyed before the call so a destructor that
		// somehow triggers `del this` doesn't recurse.
		instance.Destroyed = true
		// Unregister from the lifetime list before invoking so the
		// program-exit sweep doesn't visit it again.
		for i, tracked := range e.destructibleInstances {
			if tracked == instance {
				e.destructibleInstances = append(e.destructibleInstances[:i], e.destructibleInstances[i+1:]...)
				break
			}
		}
		if _, err := e.applyFunctionWithThis(*instance.Class.Destructor, nil, instance); err != nil {
			return err
		}
	}
	env.Delete(name)
	return nil
}

// runWithCleanup invokes __exit__ when the resource is a class
// instance whose class defines that magic method. Otherwise it is a
// no-op: the destructor (if any) belongs to the object's lifetime,
// not to the with-block's scope, and fires later via the program-
// exit sweep or an explicit `del`.
func (e *Evaluator) runWithCleanup(resource runtime.Value) error {
	instance, ok := resource.(*runtime.Instance)
	if !ok {
		return nil
	}
	if method, ok := lookupDunderEval(instance.Class, "__exit", "__exit__"); ok {
		_, err := e.applyFunctionWithThis(method, nil, instance)
		return err
	}
	return nil
}

func lookupDunderEval(class *runtime.Class, canonical, legacy string) (runtime.Function, bool) {
	if m, ok := lookupMethod(class, canonical); ok {
		return m, true
	}
	return lookupMethod(class, legacy)
}

func lookupStaticDunderEval(class *runtime.Class, canonical, legacy string) (runtime.Function, bool) {
	if m, ok := lookupStaticMethod(class, canonical); ok {
		return m, true
	}
	return lookupStaticMethod(class, legacy)
}

func catchMatches(clause ast.CatchClause, err runtime.Error) bool {
	if clause.Type == nil {
		return true
	}
	return errorTypeMatches(err.Class, clause.Type.Name)
}

func errorTypeMatches(class string, target string) bool {
	if target == "" || target == "Error" {
		return true
	}
	for current := class; current != ""; current = errorParent(current) {
		if current == target {
			return true
		}
	}
	return false
}

func errorParent(class string) string {
	switch class {
	case "TimeoutError", "TlsError":
		return "IOError"
	case "RuntimeError", "TypeError", "ValueError", "IOError", "ParseError", "MatchError", "ImmutableError", "PermissionError", "AssertionError":
		return "Error"
	default:
		return ""
	}
}

func (e *Evaluator) errorParent(class string) string {
	if parent, ok := e.errorClassParents[class]; ok {
		return parent
	}
	switch class {
	case "TimeoutError", "TlsError":
		return "IOError"
	case "RuntimeError", "TypeError", "ValueError", "IOError", "ParseError", "MatchError", "ImmutableError", "PermissionError", "AssertionError":
		return "Error"
	default:
		return ""
	}
}

func (e *Evaluator) errorTypeMatches(class string, target string) bool {
	if target == "" || target == "Error" {
		// FatalError is its own tier, not an Error.
		return class != "FatalError"
	}
	for current := class; current != ""; current = e.errorParent(current) {
		if current == target {
			return true
		}
	}
	return false
}

func (e *Evaluator) catchMatches(clause ast.CatchClause, err runtime.Error) bool {
	if clause.Type == nil {
		return true
	}
	// Strip an optional module prefix - `catch (errors.HttpException e)`
	// matches the bare "HttpException" the parent chain records.
	return e.errorTypeMatches(err.Class, simpleTypeName(clause.Type.Name))
}

func (e *Evaluator) evalForInStatement(stmt *ast.ForStatement, loopEnv *runtime.Environment) (signal, error) {
	iterable, err := e.evalExpression(stmt.Iterable, loopEnv)
	if err != nil {
		return signal{}, err
	}
	names := stmt.VarNames
	if len(names) == 0 && stmt.VarName != nil {
		names = []*ast.Identifier{stmt.VarName}
	}
	if len(names) == 0 {
		return signal{}, fmt.Errorf("for-in loop has no loop variable")
	}
	for _, name := range names {
		if err := loopEnv.Define(name.Value, runtime.Null{}, false); err != nil {
			return signal{}, err
		}
	}
	if r, ok := iterable.(runtime.Range); ok {
		current := new(big.Int).Set(r.Start)
		step := new(big.Int).Set(r.Step)
		for {
			cmp := current.Cmp(r.End)
			if step.Sign() > 0 {
				if r.Exclusive && cmp >= 0 {
					break
				}
				if !r.Exclusive && cmp > 0 {
					break
				}
			} else {
				if r.Exclusive && cmp <= 0 {
					break
				}
				if !r.Exclusive && cmp < 0 {
					break
				}
			}
			sig, err := e.evalForInIteration(stmt, loopEnv, names, runtime.Int{Value: new(big.Int).Set(current)})
			if err != nil {
				return signal{}, err
			}
			switch sig.kind {
			case "break":
				return signal{}, nil
			case "continue", "":
			default:
				return sig, nil
			}
			if sig.exited {
				return sig, nil
			}
			current.Add(current, step)
		}
		return signal{}, nil
	}
	if generator, ok := iterable.(*runtime.Generator); ok {
		defer generator.Close()
		for {
			value, ok, err := generator.Next()
			if err != nil {
				return signal{}, err
			}
			if !ok {
				break
			}
			sig, err := e.evalForInIteration(stmt, loopEnv, names, value)
			if err != nil {
				return signal{}, err
			}
			switch sig.kind {
			case "break":
				return signal{}, nil
			case "continue", "":
			default:
				return sig, nil
			}
			if sig.exited {
				return sig, nil
			}
		}
		return signal{}, nil
	}
	if instance, ok := iterable.(*runtime.Instance); ok {
		iter, passthrough, iterableOk, err := e.resolveUserIterator(instance)
		if err != nil {
			return signal{}, err
		}
		if iterableOk {
			if passthrough != nil {
				iterable = passthrough
				goto dispatchPassthrough
			}
			for {
				if doneFn, ok := lookupMethod(iter.Class, "__done"); ok {
					doneResult, err := e.applyFunctionWithThis(doneFn, nil, iter)
					if err != nil {
						return signal{}, err
					}
					doneBool, ok := doneResult.(runtime.Bool)
					if !ok {
						return signal{}, fmt.Errorf("%s.__done must return bool, got %s", iter.Class.Name, doneResult.TypeName())
					}
					if doneBool.Value {
						break
					}
				}
				nextFn, ok := lookupMethod(iter.Class, "__next")
				if !ok {
					return signal{}, fmt.Errorf("%s is not an iterator: define __next()", iter.Class.Name)
				}
				value, err := e.applyFunctionWithThis(nextFn, nil, iter)
				if err != nil {
					return signal{}, err
				}
				sig, err := e.evalForInIteration(stmt, loopEnv, names, value)
				if err != nil {
					return signal{}, err
				}
				switch sig.kind {
				case "break":
					return signal{}, nil
				case "continue", "":
				default:
					return sig, nil
				}
				if sig.exited {
					return sig, nil
				}
			}
			return signal{}, nil
		}
	}
dispatchPassthrough:
	if generator, ok := iterable.(*runtime.Generator); ok {
		defer generator.Close()
		for {
			value, ok, err := generator.Next()
			if err != nil {
				return signal{}, err
			}
			if !ok {
				break
			}
			sig, err := e.evalForInIteration(stmt, loopEnv, names, value)
			if err != nil {
				return signal{}, err
			}
			switch sig.kind {
			case "break":
				return signal{}, nil
			case "continue", "":
			default:
				return sig, nil
			}
			if sig.exited {
				return sig, nil
			}
		}
		return signal{}, nil
	}
	values, err := e.iterableValues(iterable)
	if err != nil {
		return signal{}, err
	}
	for _, value := range values {
		sig, err := e.evalForInIteration(stmt, loopEnv, names, value)
		if err != nil {
			return signal{}, err
		}
		switch sig.kind {
		case "break":
			return signal{}, nil
		case "continue", "":
		default:
			return sig, nil
		}
		if sig.exited {
			return sig, nil
		}
	}
	return signal{}, nil
}

func (e *Evaluator) evalForInIteration(stmt *ast.ForStatement, loopEnv *runtime.Environment, names []*ast.Identifier, value runtime.Value) (signal, error) {
	if len(names) == 1 {
		if err := loopEnv.Assign(names[0].Value, value); err != nil {
			return signal{}, err
		}
	} else {
		list, ok := value.(*runtime.List)
		if !ok || len(list.Elements) != len(names) {
			return signal{}, fmt.Errorf("cannot destructure %s into %d loop variables", value.TypeName(), len(names))
		}
		for i, name := range names {
			if err := loopEnv.Assign(name.Value, list.Elements[i]); err != nil {
				return signal{}, err
			}
		}
	}
	return e.evalBlock(stmt.Body, loopEnv)
}

func (e *Evaluator) walkComprehensionClauses(clauses []ast.ComprehensionClause, idx int, env *runtime.Environment, body func(*runtime.Environment) error) error {
	if idx >= len(clauses) {
		return body(env)
	}
	switch c := clauses[idx].(type) {
	case *ast.ComprehensionIf:
		v, err := e.evalExpression(c.Filter, env)
		if err != nil {
			return err
		}
		if !isTruthy(v) {
			return nil
		}
		return e.walkComprehensionClauses(clauses, idx+1, env, body)
	case *ast.ComprehensionFor:
		return e.iterateComprehensionFor(c, env, func(itEnv *runtime.Environment) error {
			return e.walkComprehensionClauses(clauses, idx+1, itEnv, body)
		})
	}
	return fmt.Errorf("unknown comprehension clause %T", clauses[idx])
}

func (e *Evaluator) iterateComprehensionFor(c *ast.ComprehensionFor, env *runtime.Environment, body func(*runtime.Environment) error) error {
	iterable, err := e.evalExpression(c.Iterable, env)
	if err != nil {
		return err
	}
	names := c.VarNames
	if len(names) == 0 && c.VarName != nil {
		names = []*ast.Identifier{c.VarName}
	}
	if len(names) == 0 {
		return fmt.Errorf("comprehension `for` has no loop variable")
	}
	step := func(value runtime.Value) error {
		itEnv := runtime.NewEnclosedEnvironment(env)
		if len(names) == 1 {
			if err := itEnv.Define(names[0].Value, value, false); err != nil {
				return err
			}
		} else {
			list, ok := value.(*runtime.List)
			if !ok || len(list.Elements) != len(names) {
				return fmt.Errorf("cannot destructure %s into %d comprehension variables", value.TypeName(), len(names))
			}
			for i, name := range names {
				if err := itEnv.Define(name.Value, list.Elements[i], false); err != nil {
					return err
				}
			}
		}
		return body(itEnv)
	}
	switch it := iterable.(type) {
	case *runtime.List:
		for _, el := range it.Elements {
			if err := step(el); err != nil {
				return err
			}
		}
		return nil
	case runtime.Set:
		ordered := orderedSetValues(it)
		for _, v := range ordered {
			if err := step(v); err != nil {
				return err
			}
		}
		return nil
	case runtime.Dict:
		for _, pair := range runtime.DictPairs(it) {
			if err := step(pair); err != nil {
				return err
			}
		}
		return nil
	case runtime.String:
		for _, c := range runtime.StringChars(it) {
			if err := step(c); err != nil {
				return err
			}
		}
		return nil
	case runtime.Range:
		current := new(big.Int).Set(it.Start)
		step := new(big.Int).Set(it.Step)
		fn := func() error {
			for {
				cmp := current.Cmp(it.End)
				if step.Sign() > 0 {
					if it.Exclusive && cmp >= 0 {
						return nil
					}
					if !it.Exclusive && cmp > 0 {
						return nil
					}
				} else {
					if it.Exclusive && cmp <= 0 {
						return nil
					}
					if !it.Exclusive && cmp < 0 {
						return nil
					}
				}
				if err := body(envWithSingleBinding(env, names[0].Value, runtime.Int{Value: new(big.Int).Set(current)})); err != nil {
					return err
				}
				current.Add(current, step)
			}
		}
		return fn()
	case *runtime.Generator:
		defer it.Close()
		for {
			v, ok, err := it.Next()
			if err != nil {
				return err
			}
			if !ok {
				return nil
			}
			if err := step(v); err != nil {
				return err
			}
		}
	}
	return fmt.Errorf("comprehension cannot iterate over %s", iterable.TypeName())
}

func envWithSingleBinding(parent *runtime.Environment, name string, value runtime.Value) *runtime.Environment {
	itEnv := runtime.NewEnclosedEnvironment(parent)
	_ = itEnv.Define(name, value, false)
	return itEnv
}

func (e *Evaluator) evalDestructuringStatement(stmt *ast.DestructuringStatement, env *runtime.Environment) (signal, error) {
	val, err := e.evalExpression(stmt.Value, env)
	if err != nil {
		return signal{}, err
	}
	if stmt.IsList {
		list, ok := val.(*runtime.List)
		if !ok {
			return signal{}, fmt.Errorf("cannot list-destructure %s", val.TypeName())
		}
		if len(list.Elements) < len(stmt.Names) {
			return signal{}, fmt.Errorf("list has %d elements, destructuring expects %d", len(list.Elements), len(stmt.Names))
		}
		for i, name := range stmt.Names {
			if err := bindDestructuredName(env, name.Value, list.Elements[i], stmt.Define); err != nil {
				return signal{}, err
			}
		}
	} else {
		dict, ok := val.(runtime.Dict)
		if !ok {
			return signal{}, fmt.Errorf("cannot dict-destructure %s", val.TypeName())
		}
		for i, name := range stmt.Names {
			key := name.Value
			if i < len(stmt.Keys) && stmt.Keys[i] != "" {
				key = stmt.Keys[i]
			}
			v, ok := dictField(dict, key)
			if !ok {
				v = runtime.Null{}
			}
			if err := bindDestructuredName(env, name.Value, v, stmt.Define); err != nil {
				return signal{}, err
			}
		}
	}
	return signal{}, nil
}

func bindDestructuredName(env *runtime.Environment, name string, value runtime.Value, define bool) error {
	if define {
		return env.Define(name, value, false)
	}
	return env.Assign(name, value)
}

// resolveUserIterator returns the iterator backing a `for (x in obj)`
// loop where obj is a user-defined class instance. Calls obj.__iter()
// when defined and returns:
//   - (*runtime.Instance, true, nil) when the result is itself an
//     iterator instance (has __next or is `this` for self-iterating
//     classes).
//   - (any other runtime.Value via the "passthrough" path, true, nil)
//     by setting the value into `passthrough` so the caller falls back
//     to the standard iteration dispatcher on List / Generator / etc.
//   - (nil, false, nil) when the instance implements neither protocol
//     side (caller surfaces the existing "not iterable" error).
func (e *Evaluator) resolveUserIterator(instance *runtime.Instance) (*runtime.Instance, runtime.Value, bool, error) {
	if instance == nil || instance.Class == nil {
		return nil, nil, false, nil
	}
	// Follow the __iter chain: __iter may return another object that itself
	// needs resolving (e.g. a Socket whose __iter returns an IOStream whose
	// __iter returns a generator). The VM resolves this recursively; match it.
	// Bounded so a pathological self-cycle cannot spin forever.
	current := instance
	for depth := 0; depth <= 100; depth++ {
		iterFn, ok := lookupMethod(current.Class, "__iter")
		if !ok {
			if _, hasNext := lookupMethod(current.Class, "__next"); hasNext {
				return current, nil, true, nil
			}
			if current == instance {
				return nil, nil, false, nil
			}
			return nil, current, true, nil
		}
		result, err := e.applyFunctionWithThis(iterFn, nil, current)
		if err != nil {
			return nil, nil, true, err
		}
		inst, ok := result.(*runtime.Instance)
		if !ok {
			return nil, result, true, nil
		}
		if inst == current {
			if _, hasNext := lookupMethod(current.Class, "__next"); hasNext {
				return current, nil, true, nil
			}
			return nil, inst, true, nil
		}
		if _, hasNext := lookupMethod(inst.Class, "__next"); hasNext {
			return inst, nil, true, nil
		}
		current = inst
	}
	return nil, nil, true, fmt.Errorf("__iter chain too deep")
}

// sortElements stably sorts elements in place, via the optional
// less/comparator callback in args.
func (e *Evaluator) sortElements(elements []runtime.Value, args []runtime.Value) error {
	var sortErr error
	sort.SliceStable(elements, func(i, j int) bool {
		if sortErr != nil {
			return false
		}
		if len(args) == 1 {
			result, err := e.callValue(args[0], []runtime.Value{elements[i], elements[j]})
			if err != nil {
				sortErr = err
				return false
			}
			less, err := native.SortLess(result)
			if err != nil {
				sortErr = err
				return false
			}
			return less
		}
		cmp, err := e.compareValues(elements[i], elements[j])
		if err != nil {
			sortErr = err
			return false
		}
		return cmp < 0
	})
	return sortErr
}

func (e *Evaluator) iterableValues(value runtime.Value) ([]runtime.Value, error) {
	switch value := value.(type) {
	case *runtime.List:
		return value.Elements, nil
	case *runtime.Generator:
		values := []runtime.Value{}
		for {
			next, ok, err := value.Next()
			if err != nil {
				return nil, err
			}
			if !ok {
				return values, nil
			}
			values = append(values, next)
		}
	case runtime.Dict:
		return runtime.DictPairs(value), nil
	case runtime.Set:
		return orderedSetValues(value), nil
	case runtime.String:
		return runtime.StringChars(value), nil
	default:
		return nil, fmt.Errorf("%s is not iterable", value.TypeName())
	}
}

func (e *Evaluator) evalBoolCondition(expr ast.Expression, env *runtime.Environment) (bool, error) {
	value, err := e.evalExpression(expr, env)
	if err != nil {
		return false, err
	}
	boolValue, ok := value.(runtime.Bool)
	if !ok {
		return false, fmt.Errorf("condition must be bool, got %s", value.TypeName())
	}
	return boolValue.Value, nil
}

// applyCallableValue dispatches a CallExpression whose callee has
// already been evaluated to a runtime value. Used both for the normal
// "callee is an expression" path and for the parenthesized-selector
// form `(obj.fn)(args)` that must invoke obj.fn's value rather than
// dispatch as a method call.
func (e *Evaluator) applyCallableValue(callee runtime.Value, call *ast.CallExpression, env *runtime.Environment, expected *ast.TypeRef) (runtime.Value, error) {
	result, err := e.applyCallableValueRaw(callee, call, env, expected)
	if err == nil {
		e.stampDecoratedClassResult(call.Callee, result)
	}
	return result, err
}

func (e *Evaluator) applyCallableValueRaw(callee runtime.Value, call *ast.CallExpression, env *runtime.Environment, expected *ast.TypeRef) (runtime.Value, error) {
	if fn, ok := callee.(runtime.Function); ok {
		args, err := e.evalFunctionCallArguments(fn, call, env)
		if err != nil {
			return nil, err
		}
		if call.Token.Line > 0 {
			e.currentLine = call.Token.Line
		}
		if fn.ForwardThis {
			if value, ok := env.Get("this"); ok {
				if instance, ok := value.(*runtime.Instance); ok {
					return e.applyFunctionWithThis(fn, args, instance)
				}
			}
		}
		return e.applyFunctionWithTypeArgs(fn, args, call.TypeArguments)
	}
	if overloaded, ok := callee.(runtime.OverloadedFunction); ok {
		return e.applyOverloadedFunction(overloaded.Name, overloaded.Overloads, call, env, nil, expected)
	}
	if class, ok := callee.(*runtime.Class); ok {
		return e.instantiateClassFromCall(class, call, env, expected)
	}
	if instance, ok := callee.(*runtime.Instance); ok {
		method, ok := lookupMethod(instance.Class, "__invoke")
		if !ok {
			return nil, fmt.Errorf("%s is not callable", call.Callee.String())
		}
		args, err := e.evalFunctionCallArguments(method, call, env)
		if err != nil {
			return nil, err
		}
		if call.Token.Line > 0 {
			e.currentLine = call.Token.Line
		}
		return e.applyFunctionWithThis(method, args, instance)
	}
	return nil, fmt.Errorf("%s is not callable", call.Callee.String())
}

func (e *Evaluator) stampDecoratedClassResult(callee ast.Expression, result runtime.Value) {
	ident, ok := callee.(*ast.Identifier)
	if !ok {
		return
	}
	className, ok := e.decoratedClassIdents[ident.Value]
	if !ok {
		return
	}
	instance, ok := result.(*runtime.Instance)
	if !ok || instance == nil || instance.Class == nil {
		return
	}
	if instance.Class.Name == className {
		return
	}
	instance.ExtraTypeNames = append(instance.ExtraTypeNames, className)
}

func (e *Evaluator) evalCallWithExpectedType(call *ast.CallExpression, env *runtime.Environment, expected *ast.TypeRef) (runtime.Value, error) {
	if call.Token.Line > 0 {
		e.currentLine = call.Token.Line
	}
	if ident, ok := call.Callee.(*ast.Identifier); ok && ident.Value == "parent" {
		this, err := currentInstance(env)
		if err != nil {
			return nil, err
		}
		// Resolve the lexical class (the class whose method body is currently
		// executing) rather than the runtime class of `this`. Falling back to
		// `this.Class` is only correct when the method body is directly that
		// of the runtime class - otherwise parent() inside an inherited
		// constructor would re-target the same class it lives in.
		lexicalClass := this.Class
		if len(e.classStack) > 0 {
			lexicalClass = e.classStack[len(e.classStack)-1]
		}
		parentClass := lexicalClass.Parent
		if parentClass == nil || len(parentClass.Constructors) == 0 {
			// A builtin error parent has an implicit message constructor: capture the first string arg.
			if parentClass != nil && isBuiltinErrorClass(parentClass.Name) {
				if len(call.Arguments) > 0 {
					args, err := e.evalCallArguments(call, env)
					if err != nil {
						return nil, err
					}
					for _, arg := range args {
						if s, ok := arg.(runtime.String); ok {
							this.Fields["__parentMsg"] = s
							break
						}
					}
				}
				return runtime.Null{}, nil
			}
			if parentClass != nil && len(call.Arguments) > 0 {
				return nil, fmt.Errorf("%s constructor expects no arguments", parentClass.Name)
			}
			return runtime.Null{}, nil
		}
		_, err = e.applyOverloadedFunction(parentClass.Name, parentClass.Constructors, call, env, this, nil)
		if err != nil {
			return nil, err
		}
		for _, f := range parentClass.ImmutableFields {
			this.LockField(f)
		}
		return runtime.Null{}, nil
	}
	if ident, ok := call.Callee.(*ast.Identifier); ok && ident.Value == "typeof" {
		args, err := e.evalCallArguments(call, env)
		if err != nil {
			return nil, err
		}
		if len(args) != 1 {
			return nil, fmt.Errorf("typeof expects exactly one argument")
		}
		switch evalTypeofArg := args[0].(type) {
		case *runtime.Class:
			return runtime.Type{Name: evalTypeofArg.Name}, nil
		case runtime.BytecodeClass:
			return runtime.Type{Name: evalTypeofArg.Name}, nil
		default:
			return runtime.Type{Name: args[0].TypeName()}, nil
		}
	}
	if ident, ok := call.Callee.(*ast.Identifier); ok && strings.EqualFold(ident.Value, "dump") {
		return e.evalDumpCall(call, env)
	}
	if ident, ok := call.Callee.(*ast.Identifier); ok && strings.EqualFold(ident.Value, "dir") {
		return e.evalDirCall(call, env)
	}
	if ident, ok := call.Callee.(*ast.Identifier); ok && ident.Value == "range" {
		return e.evalRangeBuiltin(call, env)
	}
	if ident, ok := call.Callee.(*ast.Identifier); ok && ident.Value == "zrange" {
		return e.evalZRangeBuiltin(call, env)
	}
	if ident, ok := call.Callee.(*ast.Identifier); ok && isBuiltinErrorClass(ident.Value) {
		args, err := e.evalCallArguments(call, env)
		if err != nil {
			return nil, err
		}
		return newErrorValue(ident.Value, args)
	}
	if ident, ok := call.Callee.(*ast.Identifier); ok && ident.Value == "assert" {
		return e.evalAssertCall(call, env)
	}
	// A parenthesized selector like `(this.fn)(args)` is a
	// "call the value" expression, not a method call. Bypass the
	// module / method dispatch and evaluate the selector as a value.
	if selector, ok := call.Callee.(*ast.SelectorExpression); ok && selector.Parenthesized {
		callee, err := e.evalExpression(call.Callee, env)
		if err != nil {
			return nil, err
		}
		return e.applyCallableValue(callee, call, env, expected)
	}
	module, name, ok := selectorName(call.Callee)
	/* Local shadowing wins over module-call dispatch: if the
	 * receiver of an `X.Y(...)` call resolves in the current
	 * scope to a non-module value (a list, a class instance, a
	 * closure, ...), treat it as a method call on that value
	 * rather than as a module-export lookup. Important because
	 * the evaluator's `imports` table is process-wide - a sibling
	 * module's `import errors as errors` would otherwise cause
	 * `let errors = []` in our function to dispatch to the
	 * unrelated module's namespace. */
	if ok {
		if value, found := env.Get(module); found {
			if _, isModule := value.(*runtime.Module); !isModule {
				if selector, ok := call.Callee.(*ast.SelectorExpression); ok {
					if v, handled, err := e.evalParentMethodCall(selector, call, env); handled {
						return v, err
					}
					receiver, err := e.evalExpression(selector.Object, env)
					if err != nil {
						return nil, err
					}
					if selector.Optional {
						if _, isNull := receiver.(runtime.Null); isNull {
							return runtime.Null{}, nil
						}
					}
					return e.evalMethodCallExpression(receiver, selector.Name.Value, call, env)
				}
			}
		}
	}
	if !ok {
		if selector, ok := call.Callee.(*ast.SelectorExpression); ok && !selector.Parenthesized {
			if value, handled, err := e.evalParentMethodCall(selector, call, env); handled {
				return value, err
			}
			receiver, err := e.evalExpression(selector.Object, env)
			if err != nil {
				return nil, err
			}
			if selector.Optional {
				if _, isNull := receiver.(runtime.Null); isNull {
					return runtime.Null{}, nil
				}
			}
			return e.evalMethodCallExpression(receiver, selector.Name.Value, call, env)
		}
		callee, err := e.evalExpression(call.Callee, env)
		if err != nil {
			return nil, err
		}
		return e.applyCallableValue(callee, call, env, expected)
	}
	if strings.EqualFold(module, "reflect") && (name == "function" || name == "class" || name == "module") {
		return e.evalReflectLookupCall(call, env, name)
	}
	if strings.EqualFold(module, "reflect") && name == "classes" {
		return e.evalReflectClassesCall(call, env)
	}
	// reflect is fully ambient on the VM (compiler special form); an
	// unshadowed bare `reflect.X(...)` dispatches without an import here too.
	ambientReflect := strings.EqualFold(module, "reflect")
	if !e.imports[module] && !ambientReflect {
		if selector, ok := call.Callee.(*ast.SelectorExpression); ok {
			if value, handled, err := e.evalParentMethodCall(selector, call, env); handled {
				return value, err
			}
			receiver, err := e.evalExpression(selector.Object, env)
			if err != nil {
				return nil, err
			}
			if selector.Optional {
				if _, isNull := receiver.(runtime.Null); isNull {
					return runtime.Null{}, nil
				}
			}
			return e.evalMethodCallExpression(receiver, selector.Name.Value, call, env)
		}
		return nil, fmt.Errorf("module %q has not been imported", module)
	}

	// An env-local Module binding is authoritative; do not consult the global
	// last-write-wins importNames alias map when one is present in scope.
	canonical, hasImportName := e.importNames[module]
	if envValue, ok := env.Get(module); ok {
		if mod, ok := envValue.(*runtime.Module); ok {
			canonical = mod.Canonical
			hasImportName = true
		}
	}
	functions, ok := map[string]builtinFunc(nil), false
	if hasImportName {
		functions, ok = e.builtins[canonical]
	} else {
		functions, ok = e.builtins[module]
	}
	if !ok {
		return e.callModuleExport(module, name, call, env, expected)
	}
	function, ok := functions[name]
	if !ok {
		if e.moduleExportExists(module, name, env) {
			return e.callModuleExport(module, name, call, env, expected)
		}
		return nil, fmt.Errorf("unknown function %s.%s", module, name)
	}
	effModule := module
	if hasImportName {
		effModule = canonical
	}
	args, err := e.evalNativeCallArguments(call, env, effModule, name)
	if err != nil {
		return nil, err
	}
	return function(call, args)
}

// evalNativeCallArguments evaluates a native call's arguments, binding named arguments to the native's declared parameter order; falls back to positional evaluation when there are no names, a spread is present, or the native has no signature.
func (e *Evaluator) evalNativeCallArguments(call *ast.CallExpression, env *runtime.Environment, module, function string) ([]runtime.Value, error) {
	hasNamed, hasSpread := false, false
	for _, arg := range call.Arguments {
		if arg.Name != nil {
			hasNamed = true
		}
		if arg.Spread {
			hasSpread = true
		}
	}
	if !hasNamed || hasSpread {
		return e.evalCallArguments(call, env)
	}
	sig, ok := native.NativeSignature(module, function)
	if !ok {
		return e.evalCallArguments(call, env)
	}
	values := make([]runtime.Value, len(call.Arguments))
	bargs := make([]binding.Arg, len(call.Arguments))
	for i, arg := range call.Arguments {
		v, err := e.evalExpression(arg.Value, env)
		if err != nil {
			return nil, err
		}
		values[i] = v
		if arg.Name != nil {
			bargs[i].Name = arg.Name.Value
		}
	}
	result, err := binding.Order(sig, bargs)
	if err != nil {
		return nil, err
	}
	indices, err := binding.NativeArgOrder(sig, result)
	if err != nil {
		return nil, err
	}
	ordered := make([]runtime.Value, len(indices))
	for k, idx := range indices {
		ordered[k] = values[idx]
	}
	return ordered, nil
}

func (e *Evaluator) moduleExportExists(module, name string, env *runtime.Environment) bool {
	moduleValue, ok := env.Get(module)
	if !ok {
		return false
	}
	userModule, ok := moduleValue.(*runtime.Module)
	if !ok {
		return false
	}
	_, ok = userModule.Exports[name]
	return ok
}

func (e *Evaluator) callModuleExport(module, name string, call *ast.CallExpression, env *runtime.Environment, expected *ast.TypeRef) (runtime.Value, error) {
	moduleValue, valueOK := env.Get(module)
	if !valueOK {
		return nil, fmt.Errorf("unknown module %s", module)
	}
	userModule, moduleOK := moduleValue.(*runtime.Module)
	if !moduleOK {
		return nil, fmt.Errorf("%s is not a module", module)
	}
	value, exportOK := userModule.Exports[name]
	if !exportOK {
		return nil, fmt.Errorf("module %s has no export %s", module, name)
	}
	if fn, ok := value.(runtime.Function); ok {
		args, err := e.evalFunctionCallArguments(fn, call, env)
		if err != nil {
			return nil, err
		}
		if call.Token.Line > 0 {
			e.currentLine = call.Token.Line
		}
		return e.applyFunction(fn, args)
	}
	if overloaded, ok := value.(runtime.OverloadedFunction); ok {
		return e.applyOverloadedFunction(module+"."+name, overloaded.Overloads, call, env, nil, expected)
	}
	if class, ok := value.(*runtime.Class); ok {
		return e.instantiateClassFromCall(class, call, env, expected)
	}
	return nil, fmt.Errorf("%s.%s is not callable", module, name)
}

func (e *Evaluator) evalSelectorExpression(expr *ast.SelectorExpression, env *runtime.Environment) (runtime.Value, error) {
	if expr.Name.Value == "type" && !expr.Optional {
		obj, err := e.evalExpression(expr.Object, env)
		if err != nil {
			return nil, err
		}
		switch v := obj.(type) {
		case runtime.Type:
			return v, nil
		case *runtime.Class:
			return runtime.Type{Name: v.Name}, nil
		case runtime.BytecodeClass:
			return runtime.Type{Name: v.Name}, nil
		default:
			return runtime.Type{Name: obj.TypeName()}, nil
		}
	}
	object, err := e.evalExpression(expr.Object, env)
	if err != nil {
		return nil, err
	}
	if expr.Optional {
		if _, isNull := object.(runtime.Null); isNull {
			return runtime.Null{}, nil
		}
	}
	if instance, ok := object.(*runtime.Instance); ok {
		if value, ok := instance.GetField(expr.Name.Value); ok {
			return value, nil
		}
		if method, ok := lookupMethod(instance.Class, expr.Name.Value); ok {
			bound := method
			bound.Env = bindThis(method.Env, instance)
			// this is captured on bound.Env; don't also forward the caller's this when this value is invoked inside another method.
			bound.ForwardThis = false
			return bound, nil
		}
		if method, ok := lookupMethod(instance.Class, "__get"); ok {
			return e.applyFunctionWithThis(method, []runtime.Value{runtime.String{Value: expr.Name.Value}}, instance)
		}
		return nil, fmt.Errorf("unknown field %s.%s", instance.Class.Name, expr.Name.Value)
	}
	if class, ok := object.(*runtime.Class); ok {
		if value, ok := lookupStaticValue(class, expr.Name.Value); ok {
			return value, nil
		}
		if method, ok := lookupStaticMethod(class, expr.Name.Value); ok {
			return method, nil
		}
		if method, ok := lookupStaticMethod(class, "__getStatic"); ok {
			return e.applyFunction(method, []runtime.Value{runtime.String{Value: expr.Name.Value}})
		}
		return nil, fmt.Errorf("unknown static member %s.%s", class.Name, expr.Name.Value)
	}
	if module, ok := object.(*runtime.Module); ok {
		if value, ok := module.Exports[expr.Name.Value]; ok {
			return value, nil
		}
		// A native module's functions are not in Exports; expose a pure one as
		// a first-class callable value (math.abs without calling it), matching
		// the bytecode VM's OpNativeValue.
		canonical := module.Canonical
		if canonical == "" {
			canonical = module.Name
		}
		if native.IsPureBuiltin(canonical, expr.Name.Value) {
			return e.wrapBuiltinAsFunction(canonical, expr.Name.Value, e.registryBuiltin(canonical, expr.Name.Value)), nil
		}
		return nil, fmt.Errorf("module %s has no export %s", module.Name, expr.Name.Value)
	}
	if typeVal, ok := object.(runtime.Type); ok {
		// Builtin type statics as first-class values (no import):
		// string.compare, bytes.fromString, ...
		if v, ok := e.nativeBuiltinValue(typeVal.Name, expr.Name.Value); ok {
			return v, nil
		}
		return nil, fmt.Errorf("%s has no static member %s", typeVal.Name, expr.Name.Value)
	}
	if errValue, ok := object.(runtime.Error); ok {
		switch expr.Name.Value {
		case "class", "name":
			return runtime.String{Value: errValue.Class}, nil
		case "message":
			return runtime.String{Value: errValue.Message}, nil
		default:
			if errValue.Fields != nil {
				if v, ok := errValue.Fields[expr.Name.Value]; ok {
					return v, nil
				}
			}
			return nil, fmt.Errorf("%s has no field %s", errValue.Class, expr.Name.Value)
		}
	}
	if task, ok := object.(*runtime.Task); ok {
		switch expr.Name.Value {
		case "done":
			return runtime.Bool{Value: task.Done()}, nil
		case "cancelled":
			return runtime.Bool{Value: task.Cancelled()}, nil
		}
		return nil, fmt.Errorf("Task has no field %s", expr.Name.Value)
	}
	if enum, ok := object.(*runtime.EnumDef); ok {
		return enumVariantValue(enum, expr.Name.Value)
	}
	if ev, ok := object.(runtime.EnumVariant); ok {
		return enumVariantField(ev, expr.Name.Value)
	}
	if r, ok := object.(runtime.Range); ok {
		switch expr.Name.Value {
		case "start":
			return runtime.Int{Value: new(big.Int).Set(r.Start)}, nil
		case "end":
			return runtime.Int{Value: new(big.Int).Set(r.End)}, nil
		case "step":
			return runtime.Int{Value: new(big.Int).Set(r.Step)}, nil
		case "length":
			return runtime.Int{Value: r.Length()}, nil
		case "first":
			if r.Length().Sign() == 0 {
				return runtime.Null{}, nil
			}
			return runtime.Int{Value: new(big.Int).Set(r.Start)}, nil
		case "last":
			n := r.Length()
			if n.Sign() == 0 {
				return runtime.Null{}, nil
			}
			last := new(big.Int).Mul(r.Step, new(big.Int).Sub(n, big.NewInt(1)))
			last.Add(last, r.Start)
			return runtime.Int{Value: last}, nil
		}
		return nil, fmt.Errorf("range has no field %s", expr.Name.Value)
	}
	if expr.Name.Value == "length" {
		switch v := object.(type) {
		case runtime.String:
			return runtime.SmallInt{Value: int64(len([]rune(v.Value)))}, nil
		case runtime.Bytes:
			return runtime.SmallInt{Value: int64(len(v.Value))}, nil
		case *runtime.List:
			return runtime.SmallInt{Value: int64(len(v.Elements))}, nil
		case runtime.Dict:
			return runtime.SmallInt{Value: int64(v.Len())}, nil
		case runtime.Set:
			return runtime.SmallInt{Value: int64(len(v.Elements))}, nil
		}
	}
	return nil, fmt.Errorf("unsupported selector expression %s", expr.String())
}

func randomBytes(n int) []byte {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		// Fall back to a low-quality timestamp seed - never blocks the
		// VM on entropy starvation. Tracing IDs are not security-
		// sensitive; collision risk is negligible for typical loads.
		for i := range buf {
			buf[i] = byte(time.Now().UnixNano() >> uint(i*8))
		}
	}
	return buf
}

func copyDict(value runtime.Dict) runtime.Dict {
	entries := map[string]runtime.DictEntry{}
	value.ForEachEntry(func(key string, entry runtime.DictEntry) bool {
		entries[key] = entry
		return true
	})
	return runtime.Dict{Entries: entries}
}

func stringWithOptionalUTF8Encoding(call *ast.CallExpression, args []runtime.Value) (string, error) {
	if len(args) != 1 && len(args) != 2 {
		return "", fmt.Errorf("%s expects string and optional encoding", call.Callee.String())
	}
	text, ok := args[0].(runtime.String)
	if !ok {
		return "", fmt.Errorf("%s text must be string", call.Callee.String())
	}
	if len(args) == 2 {
		encoding, ok := args[1].(runtime.String)
		if !ok {
			return "", fmt.Errorf("%s encoding must be string", call.Callee.String())
		}
		if !isUTF8Encoding(encoding.Value) {
			return "", fmt.Errorf("%s currently supports only utf-8 encoding", call.Callee.String())
		}
	}
	return text.Value, nil
}

func bytesWithOptionalUTF8Encoding(call *ast.CallExpression, args []runtime.Value) ([]byte, error) {
	if len(args) != 1 && len(args) != 2 {
		return nil, fmt.Errorf("%s expects bytes and optional encoding", call.Callee.String())
	}
	data, ok := args[0].(runtime.Bytes)
	if !ok {
		return nil, fmt.Errorf("%s data must be bytes", call.Callee.String())
	}
	if len(args) == 2 {
		encoding, ok := args[1].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("%s encoding must be string", call.Callee.String())
		}
		if !isUTF8Encoding(encoding.Value) {
			return nil, fmt.Errorf("%s currently supports only utf-8 encoding", call.Callee.String())
		}
	}
	return data.Value, nil
}

func isUTF8Encoding(value string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(value, "_", "-"))
	return normalized == "utf-8" || normalized == "utf8"
}

func singleBytesValue(call *ast.CallExpression, args []runtime.Value) ([]byte, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects exactly one argument", call.Callee.String())
	}
	value, ok := args[0].(runtime.Bytes)
	if !ok {
		return nil, fmt.Errorf("%s expects bytes", call.Callee.String())
	}
	return value.Value, nil
}

func int64Argument(call *ast.CallExpression, value runtime.Value, name string) (int64, error) {
	n, err := rawInt64(value, name)
	if err != nil {
		return 0, fmt.Errorf("%s %w", call.Callee.String(), err)
	}
	return n, nil
}

func rawInt64(value runtime.Value, name string) (int64, error) {
	n, ok := native.AsInt64(value)
	if !ok {
		return 0, fmt.Errorf("%s must be int", name)
	}
	return n, nil
}

func bytesFromStringOrBytes(call *ast.CallExpression, value runtime.Value, name string) ([]byte, error) {
	switch value := value.(type) {
	case runtime.String:
		return []byte(value.Value), nil
	case runtime.Bytes:
		return value.Value, nil
	default:
		return nil, fmt.Errorf("%s %s must be string or bytes", call.Callee.String(), name)
	}
}

func putDict(entries map[string]runtime.DictEntry, key string, value runtime.Value) {
	keyValue := runtime.String{Value: key}
	entries[dictKey(keyValue)] = runtime.DictEntry{Key: keyValue, Value: value}
}

// putDictV mutates a Dict through its accessor, so it works for both
// literal map-state dicts and inline-store dicts.
func putDictV(d runtime.Dict, key string, value runtime.Value) {
	keyValue := runtime.String{Value: key}
	d.PutEntry(dictKey(keyValue), runtime.DictEntry{Key: keyValue, Value: value})
}

func fieldsFromDict(d runtime.Dict) map[string]runtime.Value {
	fields := map[string]runtime.Value{}
	d.ForEachEntry(func(_ string, entry runtime.DictEntry) bool {
		if key, ok := entry.Key.(runtime.String); ok {
			fields[key.Value] = entry.Value
		}
		return true
	})
	return fields
}

func dictField(dict runtime.Dict, key string) (runtime.Value, bool) {
	entry, ok := dict.GetEntry(dictKey(runtime.String{Value: key}))
	if ok {
		return entry.Value, true
	}
	var found runtime.Value
	hit := false
	dict.ForEachEntry(func(_ string, entry runtime.DictEntry) bool {
		if stringKey, ok := entry.Key.(runtime.String); ok && stringKey.Value == key {
			found = entry.Value
			hit = true
			return false
		}
		return true
	})
	return found, hit
}

func dictStringField(dict runtime.Dict, key string) (string, bool) {
	value, ok := dictField(dict, key)
	if !ok {
		return "", false
	}
	text, ok := value.(runtime.String)
	if !ok {
		return "", false
	}
	return text.Value, true
}

func firstDictStringField(dict runtime.Dict, keys ...string) (string, bool) {
	for _, key := range keys {
		if value, ok := dictStringField(dict, key); ok {
			return value, true
		}
	}
	return "", false
}

func dictBoolField(dict runtime.Dict, key string) (bool, bool) {
	value, ok := dictField(dict, key)
	if !ok {
		return false, false
	}
	boolValue, ok := value.(runtime.Bool)
	if !ok {
		return false, false
	}
	return boolValue.Value, true
}

func runtimeInt64(value runtime.Value, name string) (int64, error) {
	intValue, ok := value.(runtime.Int)
	if !ok || !intValue.Value.IsInt64() {
		return 0, fmt.Errorf("%s must be int", name)
	}
	return intValue.Value.Int64(), nil
}

func handleID(value runtime.Value, label string) (int64, error) {
	handle, ok := value.(runtime.Int)
	if !ok || !handle.Value.IsInt64() {
		return 0, fmt.Errorf("%s handle must be int", label)
	}
	return handle.Value.Int64(), nil
}

func toInt64(v runtime.Value) (int64, bool) {
	switch x := v.(type) {
	case runtime.SmallInt:
		return x.Value, true
	case runtime.Int:
		if x.Value.IsInt64() {
			return x.Value.Int64(), true
		}
	}
	return 0, false
}

// NativeObjectMethod is the shared NativeObject dispatch the VM routes to for backend parity.
func (e *Evaluator) NativeObjectMethod(nativeObject runtime.NativeObject, name string, args []runtime.Value) (runtime.Value, error) {
	switch nativeObject.Kind {
	case "IOBuffer":
		return e.ioBufferMethod(nativeObject, name, args)
	case "IOStream", "IOCapture":
		return e.ioStreamMethod(nativeObject, name, args)
	case "JsonReader":
		return e.jsonReaderMethod(nativeObject, name, args)
	case "XmlReader":
		return e.xmlReaderMethod(nativeObject, name, args)
	case "CsvReader":
		return e.csvReaderMethod(nativeObject, name, args)
	case "YamlReader":
		return e.yamlReaderMethod(nativeObject, name, args)
	default:
		return nil, fmt.Errorf("%s has no method %s", nativeObject.TypeName(), name)
	}
}

func jsonToValue(value any) (runtime.Value, error) {
	switch value := value.(type) {
	case nil:
		return runtime.Null{}, nil
	case bool:
		return runtime.Bool{Value: value}, nil
	case string:
		return runtime.String{Value: value}, nil
	case json.Number:
		text := value.String()
		if strings.ContainsAny(text, ".eE") {
			decimal, err := runtime.NewDecimalLiteral(text)
			if err != nil {
				return nil, err
			}
			return decimal, nil
		}
		return runtime.NewIntLiteral(text)
	case []any:
		elements := make([]runtime.Value, 0, len(value))
		for _, item := range value {
			converted, err := jsonToValue(item)
			if err != nil {
				return nil, err
			}
			elements = append(elements, converted)
		}
		return &runtime.List{Elements: elements}, nil
	case map[string]any:
		entries := map[string]runtime.DictEntry{}
		for key, item := range value {
			converted, err := jsonToValue(item)
			if err != nil {
				return nil, err
			}
			keyValue := runtime.String{Value: key}
			entries[dictKey(keyValue)] = runtime.DictEntry{Key: keyValue, Value: converted}
		}
		return runtime.Dict{Entries: entries}, nil
	default:
		return nil, fmt.Errorf("unsupported JSON value %T", value)
	}
}

func valueToJSON(value runtime.Value) (any, error) {
	switch value := value.(type) {
	case runtime.Null:
		return nil, nil
	case runtime.Bool:
		return value.Value, nil
	case runtime.SmallInt:
		return value.Value, nil
	case runtime.Int:
		if value.Value.IsInt64() {
			return value.Value.Int64(), nil
		}
		return value.Value.String(), nil
	case runtime.Decimal:
		return json.Number(value.Value.FloatString(10)), nil
	case runtime.Float:
		return value.Value, nil
	case runtime.String:
		return value.Value, nil
	case runtime.Bytes:
		return base64.StdEncoding.EncodeToString(value.Value), nil
	case *runtime.List:
		items := make([]any, 0, len(value.Elements))
		for _, item := range value.Elements {
			converted, err := valueToJSON(item)
			if err != nil {
				return nil, err
			}
			items = append(items, converted)
		}
		return items, nil
	case runtime.Dict:
		items := map[string]any{}
		for _, __dk := range value.EntryKeys() {
			entry, _ := value.GetEntry(__dk)
			key, ok := entry.Key.(runtime.String)
			if !ok {
				return nil, fmt.Errorf("json.stringify only supports dicts with string keys")
			}
			converted, err := valueToJSON(entry.Value)
			if err != nil {
				return nil, err
			}
			items[key.Value] = converted
		}
		return items, nil
	default:
		return nil, fmt.Errorf("json.stringify does not support %s", value.TypeName())
	}
}

func valueToYAML(value runtime.Value) (any, error) {
	switch value := value.(type) {
	case runtime.Null:
		return nil, nil
	case runtime.Bool:
		return value.Value, nil
	case runtime.SmallInt:
		return value.Value, nil
	case runtime.Int:
		if value.Value.IsInt64() {
			return value.Value.Int64(), nil
		}
		return value.Value.String(), nil
	case runtime.Decimal:
		return value.Value.FloatString(10), nil
	case runtime.Float:
		return value.Value, nil
	case runtime.String:
		return value.Value, nil
	case runtime.Bytes:
		return base64.StdEncoding.EncodeToString(value.Value), nil
	case *runtime.List:
		items := make([]any, 0, len(value.Elements))
		for _, item := range value.Elements {
			converted, err := valueToYAML(item)
			if err != nil {
				return nil, err
			}
			items = append(items, converted)
		}
		return items, nil
	case runtime.Dict:
		items := map[string]any{}
		for _, __dk := range value.EntryKeys() {
			entry, _ := value.GetEntry(__dk)
			key, ok := entry.Key.(runtime.String)
			if !ok {
				return nil, fmt.Errorf("yaml.stringify only supports dicts with string keys")
			}
			converted, err := valueToYAML(entry.Value)
			if err != nil {
				return nil, err
			}
			items[key.Value] = converted
		}
		return items, nil
	default:
		return nil, fmt.Errorf("yaml.stringify does not support %s", value.TypeName())
	}
}

func tomlParseError(err error, text string) native.ParseError {
	var parseErr tomllib.ParseError
	if errors.As(err, &parseErr) {
		message := parseErr.Message
		if message == "" {
			message = parseErr.Error()
		}
		offset := int64(-1)
		if parseErr.Position.Start >= 0 {
			offset = int64(parseErr.Position.Start + 1)
		}
		result := native.NewParseError(message, text, offset)
		if parseErr.Position.Line > 0 {
			result.Line = int64(parseErr.Position.Line)
		}
		return result
	}
	return native.NewParseError(err.Error(), text, -1)
}

func yamlTextParseError(err error, text string) native.ParseError {
	parseErr := native.NewParseError(err.Error(), text, -1)
	if yamlErr, ok := err.(*yamllib.TypeError); ok && len(yamlErr.Errors) > 0 {
		parseErr.Message = strings.Join(yamlErr.Errors, "; ")
	}
	if line := native.ParseLineNumberFromMessage(parseErr.Message); line > 0 {
		parseErr.Line = line
	}
	return parseErr
}

// The library formats positional errors as "line N: ..."; update if that changes.
func nativeToValue(value any) (runtime.Value, error) {
	switch value := value.(type) {
	case nil:
		return runtime.Null{}, nil
	case bool:
		return runtime.Bool{Value: value}, nil
	case int:
		return runtime.NewInt64(int64(value)), nil
	case int64:
		return runtime.NewInt64(value), nil
	case float64:
		return runtime.Float{Value: value}, nil
	case string:
		return runtime.String{Value: value}, nil
	case []byte:
		return runtime.Bytes{Value: value}, nil
	case time.Time:
		return runtime.String{Value: value.UTC().Format(time.RFC3339Nano)}, nil
	case []any:
		elements := make([]runtime.Value, 0, len(value))
		for _, item := range value {
			converted, err := nativeToValue(item)
			if err != nil {
				return nil, err
			}
			elements = append(elements, converted)
		}
		return &runtime.List{Elements: elements}, nil
	case map[string]any:
		entries := map[string]runtime.DictEntry{}
		for key, item := range value {
			converted, err := nativeToValue(item)
			if err != nil {
				return nil, err
			}
			keyValue := runtime.String{Value: key}
			entries[dictKey(keyValue)] = runtime.DictEntry{Key: keyValue, Value: converted}
		}
		return runtime.Dict{Entries: entries}, nil
	case map[any]any:
		entries := map[string]runtime.DictEntry{}
		for key, item := range value {
			keyText, ok := key.(string)
			if !ok {
				return nil, fmt.Errorf("only string map keys are supported")
			}
			converted, err := nativeToValue(item)
			if err != nil {
				return nil, err
			}
			keyValue := runtime.String{Value: keyText}
			entries[dictKey(keyValue)] = runtime.DictEntry{Key: keyValue, Value: converted}
		}
		return runtime.Dict{Entries: entries}, nil
	case map[string]map[string]any:
		entries := map[string]runtime.DictEntry{}
		for key, item := range value {
			converted, err := nativeToValue(item)
			if err != nil {
				return nil, err
			}
			keyValue := runtime.String{Value: key}
			entries[dictKey(keyValue)] = runtime.DictEntry{Key: keyValue, Value: converted}
		}
		return runtime.Dict{Entries: entries}, nil
	default:
		return nil, fmt.Errorf("unsupported native value %T", value)
	}
}

func valueToTOML(value runtime.Value) (any, error) {
	switch value := value.(type) {
	case runtime.Null:
		return nil, fmt.Errorf("toml.stringify does not support null")
	case runtime.Bool:
		return value.Value, nil
	case runtime.SmallInt:
		return value.Value, nil
	case runtime.Int:
		if value.Value.IsInt64() {
			return value.Value.Int64(), nil
		}
		return value.Value.String(), nil
	case runtime.Decimal:
		return value.Value.FloatString(10), nil
	case runtime.Float:
		return value.Value, nil
	case runtime.String:
		return value.Value, nil
	case runtime.Bytes:
		return base64.StdEncoding.EncodeToString(value.Value), nil
	case *runtime.List:
		items := make([]any, 0, len(value.Elements))
		for _, item := range value.Elements {
			converted, err := valueToTOML(item)
			if err != nil {
				return nil, err
			}
			items = append(items, converted)
		}
		return items, nil
	case runtime.Dict:
		items := map[string]any{}
		for _, __dk := range value.EntryKeys() {
			entry, _ := value.GetEntry(__dk)
			key, ok := entry.Key.(runtime.String)
			if !ok {
				return nil, fmt.Errorf("toml.stringify only supports dicts with string keys")
			}
			converted, err := valueToTOML(entry.Value)
			if err != nil {
				return nil, err
			}
			items[key.Value] = converted
		}
		return items, nil
	default:
		return nil, fmt.Errorf("toml.stringify does not support %s", value.TypeName())
	}
}

func isBuiltinErrorClass(name string) bool {
	switch name {
	case "Error", "RuntimeError", "TypeError", "ValueError", "IOError", "ParseError", "MatchError", "ImmutableError", "PermissionError", "AssertionError", "FatalError", "TimeoutError", "TlsError":
		return true
	default:
		return false
	}
}

func newErrorValue(class string, args []runtime.Value) (runtime.Value, error) {
	if len(args) > 1 {
		return nil, fmt.Errorf("%s expects zero or one argument", class)
	}
	message := ""
	if len(args) == 1 {
		msg, ok := args[0].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("%s message must be string", class)
		}
		message = msg.Value
	}
	return runtime.Error{Class: class, Message: message}, nil
}

func formatArgs(args []runtime.Value) []any {
	out := make([]any, len(args))
	for i, arg := range args {
		out[i] = formatArg(arg)
	}
	return out
}

func formatString(format string, args []runtime.Value) (string, error) {
	formatted := fmt.Sprintf(format, formatArgs(args)...)
	if strings.Contains(formatted, "%!") {
		return "", fmt.Errorf("invalid string.format arguments for %q", format)
	}
	return formatted, nil
}

func decimalFormatScale(value runtime.Value) (int, error) {
	scale, err := indexInt(value)
	if err != nil {
		return 0, fmt.Errorf("decimal scale must be int")
	}
	if scale < 0 || scale > 10000 {
		return 0, fmt.Errorf("decimal scale must be between 0 and 10000")
	}
	return scale, nil
}

func formatArg(value runtime.Value) any {
	switch value := value.(type) {
	case runtime.Null:
		return nil
	case runtime.Bool:
		return value.Value
	case runtime.Int:
		if value.Value.IsInt64() {
			return value.Value.Int64()
		}
		return value.Value.String()
	case runtime.Decimal:
		f, _ := value.Value.Float64()
		return f
	case runtime.Float:
		return value.Value
	case runtime.String:
		return value.Value
	case runtime.Bytes:
		return value.Value
	default:
		return value.Inspect()
	}
}

func (e *Evaluator) evalMethodCallExpression(receiver runtime.Value, name string, call *ast.CallExpression, env *runtime.Environment) (runtime.Value, error) {
	if enumDef, ok := receiver.(*runtime.EnumDef); ok {
		args, err := e.evalCallArguments(call, env)
		if err != nil {
			return nil, err
		}
		for _, v := range enumDef.Variants {
			if strings.EqualFold(v.Name, name) {
				if v.FieldCount != len(args) {
					return nil, fmt.Errorf("enum variant %s.%s expects %d argument(s), got %d", enumDef.Name, v.Name, v.FieldCount, len(args))
				}
				return runtime.EnumVariant{Enum: enumDef, Variant: v.Name, Fields: args}, nil
			}
		}
		if value, handled, err := runtime.EnumStaticMethod(enumDef, name, args); handled {
			return value, err
		}
		return nil, fmt.Errorf("enum %s has no variant %s", enumDef.Name, name)
	}
	if class, ok := receiver.(*runtime.Class); ok {
		methods := lookupStaticMethodOverloads(class, name)
		if len(methods) > 0 {
			return e.applyOverloadedFunction(class.Name+"."+name, methods, call, env, nil, nil)
		}
		args, err := e.evalCallArguments(call, env)
		if err != nil {
			return nil, err
		}
		if method, ok := lookupStaticMethod(class, "__callStatic"); ok {
			if call.Token.Line > 0 {
				e.currentLine = call.Token.Line
			}
			return e.applyFunction(method, []runtime.Value{runtime.String{Value: name}, &runtime.List{Elements: args}})
		}
		return nil, fmt.Errorf("unknown static method %s.%s", class.Name, name)
	}
	if instance, ok := receiver.(*runtime.Instance); ok {
		methods := lookupMethodOverloads(instance.Class, name)
		if len(methods) > 0 {
			return e.applyOverloadedFunction(instance.Class.Name+"."+name, methods, call, env, instance, nil)
		}
		args, err := e.evalCallArguments(call, env)
		if err != nil {
			return nil, err
		}
		if method, ok := lookupMethod(instance.Class, "__call"); ok {
			if call.Token.Line > 0 {
				e.currentLine = call.Token.Line
			}
			return e.applyFunctionWithThis(method, []runtime.Value{runtime.String{Value: name}, &runtime.List{Elements: args}}, instance)
		}
		return nil, native.UnknownMethodError(instance.Class.Name, name)
	}
	args, err := e.evalCallArguments(call, env)
	if err != nil {
		return nil, err
	}
	if nativeObject, ok := receiver.(runtime.NativeObject); ok {
		return e.NativeObjectMethod(nativeObject, name, args)
	}
	if gen, ok := receiver.(*runtime.Generator); ok {
		return native.GeneratorMethod(gen, name, args)
	}
	if arr, ok := receiver.(*runtime.NDArray); ok {
		return native.NDArrayMethod(arr, name, args)
	}
	if node, ok := receiver.(*runtime.HtmlNode); ok {
		return native.HtmlNodeMethod(node, name, args)
	}
	if dist, ok := receiver.(*runtime.Distribution); ok {
		return native.DistributionMethod(dist, name, args)
	}
	if z, ok := receiver.(*runtime.Complex); ok {
		return native.ComplexMethod(z, name, args)
	}
	if frame, ok := receiver.(*runtime.DataFrame); ok {
		return native.DataFrameMethod(frame, name, args, e.callValue)
	}
	if series, ok := receiver.(*runtime.DFSeries); ok {
		return native.DFSeriesMethod(series, name, args)
	}
	if expr, ok := receiver.(*runtime.DFExpr); ok {
		return native.DFExprMethod(expr, name, args)
	}
	if group, ok := receiver.(*runtime.DFGroupBy); ok {
		return native.DFGroupByMethod(group, name, args)
	}
	if instant, ok := receiver.(runtime.DateTimeInstant); ok {
		return native.DateTimeInstantMethod(instant, name, args)
	}
	if duration, ok := receiver.(runtime.DateTimeDuration); ok {
		return native.DateTimeDurationMethod(duration, name, args)
	}
	if zone, ok := receiver.(runtime.DateTimeZone); ok {
		return native.DateTimeZoneMethod(zone, name, args)
	}
	if urlValue, ok := receiver.(runtime.URLValue); ok {
		return native.URLMethod(urlValue, name, args)
	}
	if headers, ok := receiver.(runtime.HTTPHeaders); ok {
		return httpHeadersMethod(headers, name, args)
	}
	if cookie, ok := receiver.(runtime.HTTPCookie); ok {
		return native.HTTPCookieMethod(cookie, name, args)
	}
	if tmpl, ok := receiver.(runtime.TemplateValue); ok {
		return native.TemplateMethod(tmpl, name, args)
	}
	if engine, ok := receiver.(runtime.TemplateEngine); ok {
		return native.TemplateEngineMethod(engine, name, args)
	}
	if errValue, ok := receiver.(runtime.Error); ok {
		return native.ErrorMethod(errValue, name, args)
	}
	if trace, ok := receiver.(runtime.ErrorStackTrace); ok {
		return native.ErrorStackTraceMethod(trace, name, args)
	}
	if frame, ok := receiver.(runtime.ErrorStackFrame); ok {
		return native.ErrorStackFrameMethod(frame, name, args)
	}
	if task, ok := receiver.(*runtime.Task); ok {
		switch name {
		case "await":
			if len(args) != 0 {
				return nil, fmt.Errorf("Task.await expects no arguments")
			}
			return awaitValue(task)
		case "done":
			if len(args) != 0 {
				return nil, fmt.Errorf("Task.done expects no arguments")
			}
			return runtime.Bool{Value: task.Done()}, nil
		case "cancel":
			if len(args) != 0 {
				return nil, fmt.Errorf("Task.cancel expects no arguments")
			}
			task.Cancel()
			return runtime.Null{}, nil
		case "cancelled":
			if len(args) != 0 {
				return nil, fmt.Errorf("Task.cancelled expects no arguments")
			}
			return runtime.Bool{Value: task.Cancelled()}, nil
		default:
			return nil, fmt.Errorf("Task has no method %s", name)
		}
	}
	// Static methods on a builtin type value (bytes.fromString, string.fromCodePoint)
	// resolve without an import, matching the bytecode VM.
	if typeVal, ok := receiver.(runtime.Type); ok {
		if functions, ok := e.builtins[typeVal.Name]; ok {
			if fn, ok := functions[name]; ok {
				return fn(call, args)
			}
		}
	}
	// Re-stamp: argument evaluation can move currentLine off this call site.
	if call.Token.Line > 0 {
		e.currentLine = call.Token.Line
	}
	return e.evalMethodCall(receiver, name, args)
}

func (e *Evaluator) evalParentMethodCall(selector *ast.SelectorExpression, call *ast.CallExpression, env *runtime.Environment) (runtime.Value, bool, error) {
	object, ok := selector.Object.(*ast.Identifier)
	if !ok || object.Value != "parent" {
		return nil, false, nil
	}
	this, err := currentInstance(env)
	if err != nil {
		return nil, true, err
	}
	// Resolve parent from the lexically-enclosing class, not this.Class:
	// a multi-level override chain otherwise re-enters the same method.
	lexicalClass := this.Class
	if len(e.classStack) > 0 {
		lexicalClass = e.classStack[len(e.classStack)-1]
	}
	if lexicalClass.Parent == nil {
		return nil, true, fmt.Errorf("%s has no parent class", lexicalClass.Name)
	}
	methods := lookupMethodOverloads(lexicalClass.Parent, selector.Name.Value)
	if len(methods) == 0 {
		return nil, true, fmt.Errorf("unknown parent method %s.%s", lexicalClass.Parent.Name, selector.Name.Value)
	}
	value, err := e.applyOverloadedFunction(lexicalClass.Parent.Name+"."+selector.Name.Value, methods, call, env, this, nil)
	return value, true, err
}

func (e *Evaluator) evalIndexExpression(expr *ast.IndexExpression, env *runtime.Environment) (runtime.Value, error) {
	left, err := e.evalExpression(expr.Left, env)
	if err != nil {
		return nil, err
	}
	if rng, ok := expr.Index.(*ast.RangeExpression); ok {
		return e.evalSliceExpression(left, rng, env)
	}
	index, err := e.evalExpression(expr.Index, env)
	if err != nil {
		return nil, err
	}
	switch value := left.(type) {
	case *runtime.List:
		i, err := indexInt(index)
		if err != nil {
			return nil, err
		}
		return listElement(value, i)
	case runtime.Dict:
		entry, ok := value.GetEntry(dictKey(index))
		if !ok {
			return runtime.Null{}, nil
		}
		return entry.Value, nil
	case runtime.String:
		i, err := indexInt(index)
		if err != nil {
			return nil, err
		}
		return stringElement(value, i)
	case runtime.Bytes:
		i, err := indexInt(index)
		if err != nil {
			return nil, err
		}
		return bytesElement(value, i)
	case *runtime.Instance:
		if method, ok := lookupMethod(value.Class, "__index"); ok {
			bound := method
			bound.Env = bindThis(method.Env, value)
			return e.applyFunctionWithThis(bound, []runtime.Value{index}, value)
		}
		return nil, fmt.Errorf("%s is not indexable", left.TypeName())
	default:
		return nil, fmt.Errorf("%s is not indexable", left.TypeName())
	}
}

func (e *Evaluator) evalSliceExpression(left runtime.Value, rng *ast.RangeExpression, env *runtime.Environment) (runtime.Value, error) {
	switch value := left.(type) {
	case *runtime.List:
		indices, err := e.sliceIndices(rng, len(value.Elements), env)
		if err != nil {
			return nil, err
		}
		elements := make([]runtime.Value, len(indices))
		for i, idx := range indices {
			elements[i] = value.Elements[idx]
		}
		return &runtime.List{Elements: elements}, nil
	case runtime.String:
		runes := []rune(value.Value)
		indices, err := e.sliceIndices(rng, len(runes), env)
		if err != nil {
			return nil, err
		}
		out := make([]rune, len(indices))
		for i, idx := range indices {
			out[i] = runes[idx]
		}
		return runtime.String{Value: string(out)}, nil
	case runtime.Bytes:
		indices, err := e.sliceIndices(rng, len(value.Value), env)
		if err != nil {
			return nil, err
		}
		out := make([]byte, len(indices))
		for i, idx := range indices {
			out[i] = value.Value[idx]
		}
		return runtime.Bytes{Value: out}, nil
	default:
		return nil, fmt.Errorf("%s does not support slicing", left.TypeName())
	}
}

// sliceIndices computes the indices a slice expression produces, honouring
// Python-style step (including negative). When step is absent (Step == nil),
// it falls back to sliceBounds for the contiguous case.
func (e *Evaluator) sliceIndices(rng *ast.RangeExpression, length int, env *runtime.Environment) ([]int, error) {
	step := 1
	if rng.Step != nil {
		v, err := e.evalExpression(rng.Step, env)
		if err != nil {
			return nil, err
		}
		s, err := indexInt(v)
		if err != nil {
			return nil, err
		}
		step = s
	}
	if step == 0 {
		return nil, fmt.Errorf("slice step cannot be zero")
	}
	if step == 1 {
		start, end, err := e.sliceBounds(rng, length, env)
		if err != nil {
			return nil, err
		}
		out := make([]int, 0, end-start)
		for i := start; i < end; i++ {
			out = append(out, i)
		}
		return out, nil
	}
	// Python-style start/stop defaults depend on step sign.
	var start, stop int
	if step > 0 {
		start = 0
		stop = length
	} else {
		start = length - 1
		stop = -1
	}
	if rng.Start != nil {
		v, err := e.evalExpression(rng.Start, env)
		if err != nil {
			return nil, err
		}
		i, err := indexInt(v)
		if err != nil {
			return nil, err
		}
		if i < 0 {
			i += length
		}
		if step > 0 {
			if i < 0 {
				i = 0
			}
			if i > length {
				i = length
			}
		} else {
			if i < -1 {
				i = -1
			}
			if i > length-1 {
				i = length - 1
			}
		}
		start = i
	}
	if rng.End != nil {
		v, err := e.evalExpression(rng.End, env)
		if err != nil {
			return nil, err
		}
		i, err := indexInt(v)
		if err != nil {
			return nil, err
		}
		if !rng.Exclusive {
			if step > 0 {
				i++
			} else {
				i--
			}
		}
		if i < 0 {
			i += length
		}
		if step > 0 {
			if i < 0 {
				i = 0
			}
			if i > length {
				i = length
			}
		} else {
			if i < -1 {
				i = -1
			}
			if i > length-1 {
				i = length - 1
			}
		}
		stop = i
	}
	out := []int{}
	if step > 0 {
		for i := start; i < stop; i += step {
			out = append(out, i)
		}
	} else {
		for i := start; i > stop; i += step {
			out = append(out, i)
		}
	}
	return out, nil
}

func (e *Evaluator) sliceBounds(rng *ast.RangeExpression, length int, env *runtime.Environment) (int, int, error) {
	start := 0
	end := length
	if rng.Start != nil {
		value, err := e.evalExpression(rng.Start, env)
		if err != nil {
			return 0, 0, err
		}
		start, err = indexInt(value)
		if err != nil {
			return 0, 0, err
		}
	}
	if rng.End != nil {
		value, err := e.evalExpression(rng.End, env)
		if err != nil {
			return 0, 0, err
		}
		end, err = indexInt(value)
		if err != nil {
			return 0, 0, err
		}
		if !rng.Exclusive {
			end++
		}
	}
	if start < 0 {
		start = length + start
	}
	if end < 0 {
		end = length + end
	}
	if start < 0 {
		start = 0
	}
	if end < 0 {
		end = 0
	}
	if start > length {
		start = length
	}
	if end > length {
		end = length
	}
	if end < start {
		end = start
	}
	return start, end, nil
}

func (e *Evaluator) assignIndex(expr *ast.IndexExpression, newValue runtime.Value, env *runtime.Environment) error {
	left, err := e.evalExpression(expr.Left, env)
	if err != nil {
		return err
	}
	index, err := e.evalExpression(expr.Index, env)
	if err != nil {
		return err
	}
	switch value := left.(type) {
	case *runtime.List:
		if value.Frozen {
			return thrownError{value: runtime.Error{Class: "ImmutableError", Message: "cannot modify frozen list"}}
		}
		if len(value.ElementTypes) > 0 && !valueSatisfiesElementTag(newValue, value.ElementTypes[0]) {
			return thrownError{value: runtime.Error{Class: "TypeError", Message: fmt.Sprintf("cannot assign %s to list<%s>", newValue.TypeName(), value.ElementTypes[0])}}
		}
		i, err := indexInt(index)
		if err != nil {
			return err
		}
		if i < 0 {
			i = len(value.Elements) + i
		}
		if i < 0 || i >= len(value.Elements) {
			return fmt.Errorf("list index out of range")
		}
		value.Elements[i] = newValue
		return nil
	case runtime.Dict:
		if value.Frozen {
			return thrownError{value: runtime.Error{Class: "ImmutableError", Message: "cannot modify frozen dict"}}
		}
		if err := checkDictWriteTags(value, index, newValue); err != nil {
			return err
		}
		value.PutEntry(dictKey(index), runtime.DictEntry{Key: index, Value: newValue})
		return nil
	case *runtime.Instance:
		if method, ok := lookupMethod(value.Class, "__setIndex"); ok {
			bound := method
			bound.Env = bindThis(method.Env, value)
			_, err := e.applyFunctionWithThis(bound, []runtime.Value{index, newValue}, value)
			return err
		}
		return fmt.Errorf("%s does not support index assignment", left.TypeName())
	default:
		return fmt.Errorf("%s does not support index assignment", left.TypeName())
	}
}

func (e *Evaluator) assignSelector(expr *ast.SelectorExpression, newValue runtime.Value, env *runtime.Environment) error {
	object, err := e.evalExpression(expr.Object, env)
	if err != nil {
		return err
	}
	instance, ok := object.(*runtime.Instance)
	if ok {
		if instance.Frozen {
			return thrownError{value: runtime.Error{Class: "ImmutableError", Message: "cannot modify frozen instance of " + instance.Class.Name}}
		}
		if instance.LockedFields[expr.Name.Value] {
			return thrownError{value: runtime.Error{Class: "ImmutableError", Message: "cannot modify immutable field " + expr.Name.Value + " of " + instance.Class.Name}}
		}
		transformed, err := e.applyFieldDecorators(instance.Class, expr.Name.Value, newValue, env)
		if err != nil {
			return err
		}
		newValue = transformed
		if instance.HasField(expr.Name.Value) {
			instance.SetField(expr.Name.Value, newValue)
			return nil
		}
		if method, ok := lookupMethod(instance.Class, "__set"); ok {
			_, err := e.applyFunctionWithThis(method, []runtime.Value{runtime.String{Value: expr.Name.Value}, newValue}, instance)
			return err
		}
		return fmt.Errorf("unknown field %s.%s", instance.Class.Name, expr.Name.Value)
	}
	class, ok := object.(*runtime.Class)
	if ok {
		// Direct assignment to a static let / const member (immutable
		// const is enforced at the declaration site by the analyzer;
		// the runtime treats all StaticValues as writable so user
		// code that genuinely intends mutation through `static let`
		// works).
		for cur := class; cur != nil; cur = cur.Parent {
			if _, present := cur.StaticValues[expr.Name.Value]; present {
				cur.StaticValues[expr.Name.Value] = newValue
				return nil
			}
		}
		if method, ok := lookupStaticMethod(class, "__setStatic"); ok {
			_, err := e.applyFunction(method, []runtime.Value{runtime.String{Value: expr.Name.Value}, newValue})
			return err
		}
		return fmt.Errorf("unknown static member %s.%s", class.Name, expr.Name.Value)
	}
	return fmt.Errorf("%s does not support field assignment", object.TypeName())
}

// evalRangeBuiltin implements the top-level `range(start, end[, step])`
// shorthand, producing a list<int> inclusive of both endpoints. Negative
// steps are allowed when start > end. Step zero is rejected.
func (e *Evaluator) evalAssertCall(call *ast.CallExpression, env *runtime.Environment) (runtime.Value, error) {
	if e.AssertionsDisabled {
		return runtime.Null{}, nil
	}
	if len(call.Arguments) < 1 || len(call.Arguments) > 2 {
		return nil, fmt.Errorf("assert expects (condition) or (condition, message)")
	}
	for _, arg := range call.Arguments {
		if arg.Name != nil {
			return nil, fmt.Errorf("assert does not accept named arguments")
		}
	}
	condValue, err := e.evalExpression(call.Arguments[0].Value, env)
	if err != nil {
		return nil, err
	}
	if isTruthy(condValue) {
		return runtime.Null{}, nil
	}
	message := "assertion failed: " + call.Arguments[0].Value.String()
	if len(call.Arguments) == 2 {
		msgValue, err := e.evalExpression(call.Arguments[1].Value, env)
		if err != nil {
			return nil, err
		}
		msgStr, ok := msgValue.(runtime.String)
		if !ok {
			return nil, fmt.Errorf("assert message must be string")
		}
		message = msgStr.Value
	}
	return nil, thrownError{value: e.withTrace(runtime.Error{Class: "AssertionError", Message: message, Parents: e.errorParentChain("AssertionError")})}
}

func (e *Evaluator) evalRangeBuiltin(call *ast.CallExpression, env *runtime.Environment) (runtime.Value, error) {
	return e.evalRangeLike(call, env, false)
}

// evalZRangeBuiltin is range with an exclusive end and a 1-arg form (zrange(n)).
func (e *Evaluator) evalZRangeBuiltin(call *ast.CallExpression, env *runtime.Environment) (runtime.Value, error) {
	return e.evalRangeLike(call, env, true)
}

func (e *Evaluator) evalRangeLike(call *ast.CallExpression, env *runtime.Environment, exclusive bool) (runtime.Value, error) {
	args, err := e.evalCallArguments(call, env)
	if err != nil {
		return nil, err
	}
	name := "range"
	minArgs := 2
	if exclusive {
		name = "zrange"
		minArgs = 1
	}
	if len(args) < minArgs || len(args) > 3 {
		if exclusive {
			return nil, fmt.Errorf("zrange expects (n), (start, end), or (start, end, step)")
		}
		return nil, fmt.Errorf("range expects (start, end) or (start, end, step)")
	}
	startBig := big.NewInt(0)
	endIdx := 0
	if len(args) >= 2 {
		s, ok := native.IntValueToBigInt(args[0])
		if !ok {
			return nil, fmt.Errorf("%s start must be int", name)
		}
		startBig = s
		endIdx = 1
	}
	endBig, ok := native.IntValueToBigInt(args[endIdx])
	if !ok {
		return nil, fmt.Errorf("%s end must be int", name)
	}
	step := big.NewInt(1)
	if len(args) == 3 {
		stepBig, ok := native.IntValueToBigInt(args[2])
		if !ok {
			return nil, fmt.Errorf("%s step must be int", name)
		}
		step = stepBig
	} else if startBig.Cmp(endBig) > 0 {
		step = big.NewInt(-1)
	}
	if step.Sign() == 0 {
		return nil, fmt.Errorf("%s step cannot be zero", name)
	}
	rng := runtime.Range{Start: new(big.Int).Set(startBig), End: new(big.Int).Set(endBig), Exclusive: exclusive, Step: new(big.Int).Set(step)}
	return rangeToList(rng), nil
}

// tagCollectionWithTypeRef returns a copy of the given collection
// value with its ElementTypes field populated from the declared
// TypeRef. Non-collection values are returned unchanged. The tag
// covers list<T>, T[], set<T>, dict<K,V>. Untagged collections that
// flow through this path gain a fresh tag; already-tagged ones are
// rebound to the new annotation.
func tagCollectionWithTypeRef(value runtime.Value, typ *ast.TypeRef) runtime.Value {
	if typ == nil || typ.Operator != "" {
		return value
	}
	switch v := value.(type) {
	case *runtime.List:
		var tag []string
		if typ.ListAlias && len(typ.Arguments) == 0 && typ.Name != "" && !strings.EqualFold(typ.Name, "list") {
			tag = []string{typ.Name}
		} else if len(typ.Arguments) > 0 {
			tag = make([]string, 0, len(typ.Arguments))
			for _, arg := range typ.Arguments {
				tag = append(tag, elementTagName(arg))
			}
		}
		if tag != nil {
			v.ElementTypes = tag
		}
		return v
	case runtime.Set:
		if len(typ.Arguments) > 0 && typ.Arguments[0] != nil {
			v.ElementTypes = []string{elementTagName(typ.Arguments[0])}
		}
		return v
	case runtime.Dict:
		if len(typ.Arguments) >= 2 && typ.Arguments[0] != nil && typ.Arguments[1] != nil {
			v.ElementTypes = []string{elementTagName(typ.Arguments[0]), elementTagName(typ.Arguments[1])}
		}
		return v
	}
	return value
}

// rangeToList materialises a range value as a list<int>. Shared by
// `range()` and `Range.toList()` so both produce identical output.
func rangeToList(rng runtime.Range) *runtime.List {
	var elements []runtime.Value
	current := new(big.Int).Set(rng.Start)
	step := rng.Step
	for {
		cmp := current.Cmp(rng.End)
		if step.Sign() > 0 {
			if rng.Exclusive && cmp >= 0 {
				break
			}
			if !rng.Exclusive && cmp > 0 {
				break
			}
		} else {
			if rng.Exclusive && cmp <= 0 {
				break
			}
			if !rng.Exclusive && cmp < 0 {
				break
			}
		}
		elements = append(elements, runtime.Int{Value: new(big.Int).Set(current)})
		current.Add(current, step)
	}
	return &runtime.List{Elements: elements}
}

func (e *Evaluator) evalRangeExpression(expr *ast.RangeExpression, env *runtime.Environment) (runtime.Value, error) {
	start := big.NewInt(0)
	var startStr *runtime.String
	if expr.Start != nil {
		value, err := e.evalExpression(expr.Start, env)
		if err != nil {
			return nil, err
		}
		if s, ok := value.(runtime.String); ok && len([]rune(s.Value)) == 1 {
			startStr = &s
		} else {
			startBig, ok := native.IntValueToBigInt(value)
			if !ok {
				return nil, fmt.Errorf("range start must be int")
			}
			start = startBig
		}
	}
	if expr.End == nil {
		return nil, fmt.Errorf("open-ended ranges are not evaluated outside slices yet")
	}
	endValue, err := e.evalExpression(expr.End, env)
	if err != nil {
		return nil, err
	}
	if startStr != nil {
		// Char range: 'a'..'z' produces an eager list<string> of single
		// character entries covering the inclusive codepoint span. The
		// `..<` exclusive form omits the last element. Step is fixed at +1.
		endStr, ok := endValue.(runtime.String)
		if !ok || len([]rune(endStr.Value)) != 1 {
			return nil, fmt.Errorf("char range end must be a single-character string")
		}
		startRune := []rune(startStr.Value)[0]
		endRune := []rune(endStr.Value)[0]
		var elements []runtime.Value
		if startRune <= endRune {
			for r := startRune; r <= endRune; r++ {
				if expr.Exclusive && r == endRune {
					break
				}
				elements = append(elements, runtime.String{Value: string(r)})
			}
		} else {
			for r := startRune; r >= endRune; r-- {
				if expr.Exclusive && r == endRune {
					break
				}
				elements = append(elements, runtime.String{Value: string(r)})
			}
		}
		return &runtime.List{Elements: elements}, nil
	}
	endBig, ok := native.IntValueToBigInt(endValue)
	if !ok {
		return nil, fmt.Errorf("range end must be int")
	}
	step := big.NewInt(1)
	if expr.Step != nil {
		stepValue, err := e.evalExpression(expr.Step, env)
		if err != nil {
			return nil, err
		}
		stepBig, ok := native.IntValueToBigInt(stepValue)
		if !ok {
			return nil, fmt.Errorf("range step must be int")
		}
		step = stepBig
	}
	if step.Sign() == 0 {
		return nil, fmt.Errorf("range step cannot be zero")
	}
	return runtime.Range{Start: new(big.Int).Set(start), End: new(big.Int).Set(endBig), Exclusive: expr.Exclusive, Step: new(big.Int).Set(step)}, nil
}

func (e *Evaluator) evalCallArguments(call *ast.CallExpression, env *runtime.Environment) ([]runtime.Value, error) {
	args := make([]runtime.Value, 0, len(call.Arguments))
	for _, arg := range call.Arguments {
		if arg.Name != nil {
			return nil, fmt.Errorf("named arguments are only supported for Geblang functions and methods")
		}
		value, err := e.evalExpression(arg.Value, env)
		if err != nil {
			return nil, err
		}
		if arg.Spread {
			list, ok := value.(*runtime.List)
			if !ok {
				return nil, fmt.Errorf("spread argument must be a list")
			}
			args = append(args, list.Elements...)
			continue
		}
		args = append(args, value)
	}
	return args, nil
}

type evaluatedCallArg struct {
	name  string
	value runtime.Value
	// fromSpread: binder silently drops unknown names; explicit named args don't.
	fromSpread bool
}

func (e *Evaluator) evalDetailedCallArguments(call *ast.CallExpression, env *runtime.Environment) ([]evaluatedCallArg, error) {
	args := make([]evaluatedCallArg, 0, len(call.Arguments))
	for _, arg := range call.Arguments {
		value, err := e.evalExpression(arg.Value, env)
		if err != nil {
			return nil, err
		}
		if arg.Spread {
			switch value := value.(type) {
			case *runtime.List:
				for _, elem := range value.Elements {
					args = append(args, evaluatedCallArg{name: "", value: elem})
				}
			case runtime.Dict:
				expanded, err := dictSpreadCallArguments(value)
				if err != nil {
					return nil, err
				}
				args = append(args, expanded...)
			default:
				return nil, fmt.Errorf("spread argument must be a list or dict")
			}
			continue
		}
		name := ""
		if arg.Name != nil {
			name = arg.Name.Value
		}
		args = append(args, evaluatedCallArg{name: name, value: value})
	}
	return args, nil
}

func dictSpreadCallArguments(dict runtime.Dict) ([]evaluatedCallArg, error) {
	type namedArg struct {
		name  string
		value runtime.Value
	}
	named := make([]namedArg, 0, dict.Len())
	for _, __dk := range dict.EntryKeys() {
		entry, _ := dict.GetEntry(__dk)
		key, ok := entry.Key.(runtime.String)
		if !ok {
			return nil, fmt.Errorf("spread dict argument keys must be strings")
		}
		named = append(named, namedArg{name: key.Value, value: entry.Value})
	}
	sort.Slice(named, func(i, j int) bool { return named[i].name < named[j].name })
	args := make([]evaluatedCallArg, 0, len(named))
	for _, arg := range named {
		args = append(args, evaluatedCallArg{name: arg.name, value: arg.value, fromSpread: true})
	}
	return args, nil
}

func (e *Evaluator) applyOverloadedFunction(label string, overloads []runtime.Function, call *ast.CallExpression, env *runtime.Environment, this *runtime.Instance, expected *ast.TypeRef) (runtime.Value, error) {
	// Build per-argument expected-type hints from overloads that match the outer
	// expected return type. If all candidate overloads agree on a parameter type
	// for a given position, propagate it when evaluating that argument so that
	// nested overloaded calls (e.g. use(make())) can resolve unambiguously.
	hints := overloadParamTypeHints(overloads, len(call.Arguments), expected)
	provided, err := e.evalDetailedCallArgumentsWithHints(call, env, hints)
	if err != nil {
		return nil, err
	}
	var matches []runtime.Function
	var matchedArgs [][]runtime.Value
	var matchedDropped []int
	for _, fn := range overloads {
		args, dropped, ok := bindEvaluatedFunctionCallArgumentsDetail(fn, provided)
		if !ok || !functionArgumentsMatch(fn, args) || !functionReturnMatchesExpected(fn, expected) {
			continue
		}
		matches = append(matches, fn)
		matchedArgs = append(matchedArgs, args)
		matchedDropped = append(matchedDropped, dropped)
	}
	// Explicit type args break ties only; single-candidate mismatches stay
	// with construct-site validation for the precise per-parameter error.
	if len(matches) > 1 && len(call.TypeArguments) > 0 {
		kept := matches[:0]
		keptArgs := matchedArgs[:0]
		keptDropped := matchedDropped[:0]
		for i, fn := range matches {
			if functionArgumentsMatchWithCallTypeArgs(fn, matchedArgs[i], call.TypeArguments) {
				kept = append(kept, fn)
				keptArgs = append(keptArgs, matchedArgs[i])
				keptDropped = append(keptDropped, matchedDropped[i])
			}
		}
		if len(kept) > 0 {
			matches = kept
			matchedArgs = keptArgs
			matchedDropped = keptDropped
		}
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("no matching overload for %s", label)
	}
	if len(matches) > 1 {
		minDropped := matchedDropped[0]
		for _, d := range matchedDropped[1:] {
			if d < minDropped {
				minDropped = d
			}
		}
		kept := matches[:0]
		keptArgs := matchedArgs[:0]
		for i, fn := range matches {
			if matchedDropped[i] == minDropped {
				kept = append(kept, fn)
				keptArgs = append(keptArgs, matchedArgs[i])
			}
		}
		matches = kept
		matchedArgs = keptArgs
	}
	if len(matches) > 1 {
		return nil, fmt.Errorf("ambiguous overload for %s", label)
	}
	if this != nil && this.Class != nil && strings.EqualFold(label, this.Class.Name) {
		// Skip when dispatching a parent's same-named constructor via parent().
		if matches[0].OwnerClass == nil || matches[0].OwnerClass == this.Class {
			if err := e.applyAutoParentConstructor(this, matches[0]); err != nil {
				return nil, err
			}
		}
	}
	chosen := matches[0]
	if len(call.TypeArguments) > 0 && len(chosen.TypeParameters) > 0 {
		merged := map[string]string{}
		for k, v := range chosen.TypeBindings {
			merged[k] = v
		}
		for i, t := range call.TypeArguments {
			if i >= len(chosen.TypeParameters) {
				break
			}
			if t == nil || t.Operator != "" || t.Name == "" {
				continue
			}
			merged[chosen.TypeParameters[i]] = t.Name
		}
		chosen.TypeBindings = merged
	}
	// Re-stamp: argument evaluation above can move currentLine off this call site.
	if call.Token.Line > 0 {
		e.currentLine = call.Token.Line
	}
	return e.applyFunctionWithThis(chosen, matchedArgs[0], this)
}

// overloadParamTypeHints returns a per-position expected TypeRef when every
// overload that could match the outer expected return type agrees on the
// parameter type at that position.  A nil entry means no consensus.
func overloadParamTypeHints(overloads []runtime.Function, argc int, expected *ast.TypeRef) []*ast.TypeRef {
	if argc == 0 {
		return nil
	}
	hints := make([]*ast.TypeRef, argc)
	for i := range hints {
		var agreed *ast.TypeRef
		first := true
		for _, fn := range overloads {
			if !functionReturnMatchesExpected(fn, expected) {
				continue
			}
			if i >= len(fn.Parameters) {
				agreed = nil
				first = false
				continue
			}
			param := fn.Parameters[i]
			if param.Type == nil || param.Type.Name == "any" || param.Type.Operator != "" {
				agreed = nil
				first = false
				continue
			}
			if first {
				agreed = param.Type
				first = false
			} else if agreed == nil || param.Type.Name != agreed.Name {
				agreed = nil
				break
			}
		}
		hints[i] = agreed
	}
	return hints
}

func (e *Evaluator) evalDetailedCallArgumentsWithHints(call *ast.CallExpression, env *runtime.Environment, hints []*ast.TypeRef) ([]evaluatedCallArg, error) {
	args := make([]evaluatedCallArg, 0, len(call.Arguments))
	for i, arg := range call.Arguments {
		var hint *ast.TypeRef
		if hints != nil && i < len(hints) {
			hint = hints[i]
		}
		var value runtime.Value
		var err error
		if hint != nil {
			value, err = e.evalExpressionWithExpectedType(arg.Value, env, hint)
		} else {
			value, err = e.evalExpression(arg.Value, env)
		}
		if err != nil {
			return nil, err
		}
		if arg.Spread {
			switch value := value.(type) {
			case *runtime.List:
				for _, elem := range value.Elements {
					args = append(args, evaluatedCallArg{name: "", value: elem})
				}
			case runtime.Dict:
				expanded, err := dictSpreadCallArguments(value)
				if err != nil {
					return nil, err
				}
				args = append(args, expanded...)
			default:
				return nil, fmt.Errorf("spread argument must be a list or dict")
			}
			continue
		}
		name := ""
		if arg.Name != nil {
			name = arg.Name.Value
		}
		args = append(args, evaluatedCallArg{name: name, value: value})
	}
	return args, nil
}

func selectOverload(label string, overloads []runtime.Function, args []runtime.Value) (runtime.Function, error) {
	var matches []runtime.Function
	for _, fn := range overloads {
		if !functionAcceptsPositionalArgs(fn, len(args)) {
			continue
		}
		bound := make([]runtime.Value, len(fn.Parameters))
		copy(bound, args)
		if !functionArgumentsMatch(fn, bound) {
			continue
		}
		matches = append(matches, fn)
	}
	if len(matches) == 0 {
		return runtime.Function{}, fmt.Errorf("no matching overload for %s", label)
	}
	if len(matches) > 1 {
		return runtime.Function{}, fmt.Errorf("ambiguous overload for %s", label)
	}
	return matches[0], nil
}

func functionAcceptsPositionalArgs(fn runtime.Function, count int) bool {
	if fn.Native != nil && len(fn.Parameters) == 0 {
		return true
	}
	if count > len(fn.Parameters) {
		return false
	}
	for i := count; i < len(fn.Parameters); i++ {
		if fn.Parameters[i].Default == nil {
			return false
		}
	}
	return true
}

func bindEvaluatedFunctionCallArguments(fn runtime.Function, provided []evaluatedCallArg) ([]runtime.Value, bool) {
	args, _, err := bindEvaluatedFunctionCallArgumentsErr(fn, provided)
	return args, err == nil
}

// bindEvaluatedFunctionCallArgumentsDetail also reports how many fromSpread
// args were silently dropped. Overload resolution prefers the overload
// that drops fewest so spread + overload disambiguates predictably.
func bindEvaluatedFunctionCallArgumentsDetail(fn runtime.Function, provided []evaluatedCallArg) ([]runtime.Value, int, bool) {
	args, dropped, err := bindEvaluatedFunctionCallArgumentsErr(fn, provided)
	return args, dropped, err == nil
}

// bindEvaluatedFunctionCallArgumentsErr orders a call's evaluated
// arguments through the shared binder, so the evaluator binds (and
// reports errors) identically to the compiler and the VM: named
// matching is case-insensitive and positional arguments may follow
// named ones, filling the next unassigned slot.
func bindEvaluatedFunctionCallArgumentsErr(fn runtime.Function, provided []evaluatedCallArg) ([]runtime.Value, int, error) {
	if fn.Native != nil && len(fn.Parameters) == 0 {
		args := make([]runtime.Value, 0, len(provided))
		for _, arg := range provided {
			if arg.name != "" {
				return nil, 0, fmt.Errorf("%s has no parameter %s", binding.DisplayName(fn.Name), arg.name)
			}
			args = append(args, arg.value)
		}
		return args, 0, nil
	}
	// Decorators are transparent to callers: named args bind to the original signature, invoked positionally on the wrapper.
	params := fn.Parameters
	if len(fn.OriginalParameters) > 0 {
		params = fn.OriginalParameters
	}
	paramNames := make([]string, len(params))
	hasDefault := make([]bool, len(params))
	known := map[string]bool{}
	for i, p := range params {
		if p.Name != nil {
			paramNames[i] = p.Name.Value
			known[strings.ToLower(p.Name.Value)] = true
		}
		hasDefault[i] = p.Default != nil
	}
	variadic := len(params) > 0 && params[len(params)-1].Variadic
	if variadic {
		// The variadic slot needs no caller-supplied value.
		hasDefault[len(hasDefault)-1] = true
	}
	dropped := 0
	filtered := make([]evaluatedCallArg, 0, len(provided))
	for _, arg := range provided {
		if arg.fromSpread && arg.name != "" && !known[strings.ToLower(arg.name)] {
			dropped++
			continue
		}
		filtered = append(filtered, arg)
	}
	provided = filtered
	sig := binding.Signature{
		FuncName:   fn.Name,
		ParamNames: paramNames,
		HasDefault: hasDefault,
		Variadic:   variadic,
	}
	bargs := make([]binding.Arg, len(provided))
	for i, arg := range provided {
		bargs[i].Name = arg.name
	}
	result, err := binding.Order(sig, bargs)
	if err != nil {
		return nil, 0, err
	}
	argsLen := len(params) + len(result.TailArgs)
	args := make([]runtime.Value, argsLen)
	for i, slot := range result.Slots {
		if slot != binding.DefaultSlot {
			args[i] = provided[slot].value
		}
	}
	for i, argIndex := range result.TailArgs {
		args[len(params)+i] = provided[argIndex].value
	}
	return args, dropped, nil
}

func functionArgumentsMatch(fn runtime.Function, args []runtime.Value) bool {
	if fn.Native != nil && len(fn.Parameters) == 0 {
		return true
	}
	typeParams := functionTypeParameterSetOrNil(fn)
	isVariadic := len(fn.Parameters) > 0 && fn.Parameters[len(fn.Parameters)-1].Variadic
	for i, arg := range args {
		if arg == nil {
			continue
		}
		paramIdx := i
		if isVariadic && i >= len(fn.Parameters) {
			paramIdx = len(fn.Parameters) - 1
		} else if i >= len(fn.Parameters) {
			return false
		}
		if !matchValueToTypeRef(typeParams, arg, fn.Parameters[paramIdx].Type) {
			return false
		}
	}
	return true
}

// functionArgumentsMatchWithCallTypeArgs is functionArgumentsMatch with the
// call site's explicit type args substituted into bound params.
func functionArgumentsMatchWithCallTypeArgs(fn runtime.Function, args []runtime.Value, typeArgs []*ast.TypeRef) bool {
	if len(typeArgs) == 0 || len(fn.TypeParameters) == 0 {
		return functionArgumentsMatch(fn, args)
	}
	inherited := map[string]string{}
	for k, v := range fn.TypeBindings {
		inherited[k] = v
	}
	for i, t := range typeArgs {
		if i >= len(fn.TypeParameters) {
			break
		}
		if t == nil || t.Operator != "" || t.Name == "" {
			continue
		}
		inherited[fn.TypeParameters[i]] = t.Name
	}
	typeParams := functionTypeParameterSetOrNil(fn)
	isVariadic := len(fn.Parameters) > 0 && fn.Parameters[len(fn.Parameters)-1].Variadic
	for i, arg := range args {
		if arg == nil {
			continue
		}
		paramIdx := i
		if isVariadic && i >= len(fn.Parameters) {
			paramIdx = len(fn.Parameters) - 1
		} else if i >= len(fn.Parameters) {
			return false
		}
		if !matchValueToTypeRefWith(typeParams, inherited, arg, fn.Parameters[paramIdx].Type) {
			return false
		}
	}
	return true
}

// functionArgumentsMatchError returns nil if all args match, or a descriptive error for the
// first mismatched argument. The receiver's reified bindings constrain
// class-level T params; explicit fn bindings win on collision.
func functionArgumentsMatchError(fn runtime.Function, args []runtime.Value, receiver *runtime.Instance) error {
	if fn.Native != nil && len(fn.Parameters) == 0 {
		return nil
	}
	typeParams := functionTypeParameterSetOrNil(fn)
	inherited := fn.TypeBindings
	// Constructors validate at the construct site, not against the receiver.
	isConstructor := fn.OwnerClass != nil && fn.OwnerClass.Name == fn.Name
	if receiver != nil && !isConstructor && len(receiver.TypeBindings) > 0 {
		if len(inherited) == 0 {
			inherited = receiver.TypeBindings
		} else {
			merged := make(map[string]string, len(inherited)+len(receiver.TypeBindings))
			for k, v := range receiver.TypeBindings {
				merged[k] = v
			}
			for k, v := range inherited {
				merged[k] = v
			}
			inherited = merged
		}
	}
	isVariadic := len(fn.Parameters) > 0 && fn.Parameters[len(fn.Parameters)-1].Variadic
	for i, arg := range args {
		if arg == nil {
			continue
		}
		paramIdx := i
		if isVariadic && i >= len(fn.Parameters) {
			paramIdx = len(fn.Parameters) - 1
		} else if i >= len(fn.Parameters) {
			return fmt.Errorf("too many arguments for %s", fn.Name)
		}
		param := fn.Parameters[paramIdx]
		if !matchValueToTypeRefWith(typeParams, inherited, arg, param.Type) {
			paramTypeName := ""
			if param.Type != nil {
				paramTypeName = param.Type.String()
			}
			name := fn.Name
			if name == "" {
				name = "anonymous"
			}
			// Methods report as Class.method, matching VM compiled names.
			if receiver != nil && fn.OwnerClass != nil && fn.OwnerClass.Name != fn.Name {
				name = fn.OwnerClass.Name + "." + fn.Name
			}
			suffix := collectionMismatchSuffix(arg, param.Type)
			gotName := descriptiveTypeName(arg)
			if suffix != "" {
				gotName = arg.TypeName()
			}
			return fmt.Errorf("%s expects %s for parameter '%s', got %s%s", name, paramTypeName, param.Name.Value, gotName, suffix)
		}
	}
	return nil
}

func (e *Evaluator) evalFunctionCallArguments(fn runtime.Function, call *ast.CallExpression, env *runtime.Environment) ([]runtime.Value, error) {
	hasNamed := false
	for _, arg := range call.Arguments {
		if arg.Name != nil || arg.Spread {
			hasNamed = true
			break
		}
	}
	if !hasNamed {
		return e.evalCallArgumentsWithHints(call, env, fn.Parameters)
	}
	provided, err := e.evalDetailedCallArguments(call, env)
	if err != nil {
		return nil, err
	}
	args, _, err := bindEvaluatedFunctionCallArgumentsErr(fn, provided)
	if err != nil {
		return nil, err
	}
	return args, nil
}

func (e *Evaluator) evalCallArgumentsWithHints(call *ast.CallExpression, env *runtime.Environment, params []ast.Parameter) ([]runtime.Value, error) {
	args := make([]runtime.Value, 0, len(call.Arguments))
	paramIdx := 0
	for _, arg := range call.Arguments {
		var expected *ast.TypeRef
		if paramIdx < len(params) {
			p := params[paramIdx]
			if p.Type != nil && p.Type.Operator == "" && p.Type.Name != "any" {
				expected = p.Type
			}
			if !arg.Spread {
				paramIdx++
			}
		}
		value, err := e.evalExpressionWithExpectedType(arg.Value, env, expected)
		if err != nil {
			return nil, err
		}
		if arg.Spread {
			list, ok := value.(*runtime.List)
			if !ok {
				return nil, fmt.Errorf("spread argument must be a list")
			}
			args = append(args, list.Elements...)
			continue
		}
		args = append(args, value)
	}
	return args, nil
}

func (e *Evaluator) applyFunction(fn runtime.Function, args []runtime.Value) (runtime.Value, error) {
	return e.applyFunctionWithThis(fn, args, nil)
}

// applyFunctionWithTypeArgs runs a generic function with type-parameter
// bindings pre-seeded from the call site's explicit `<TypeArgs>` clause.
// These bindings take strict priority over the per-call inference loop in
// applyFunctionWithThisSync: an `assertIs<int>(...)` call binds T to int
// regardless of what the argument's runtime type is, and the parameter
// type check then resolves T to int through the planted callEnv binding.
// When the call site has no explicit type arguments, falls through to the
// normal inference path.
// inferGenericBindingsFromTypeRef walks a parameter's TypeRef tree
// against the concrete arg value to discover bindings for any leaf
// type-parameter references. Mirrors VM.inferGenericBindingsFromSpec
// so the same nested-generic cases bind identically in both backends.
// Direct type-param leaves like `T` bind from arg.TypeName(); nested
// container shapes recurse into the inner spec against the container's
// first element/entry.
func (e *Evaluator) inferGenericBindingsFromTypeRef(spec *ast.TypeRef, value runtime.Value, typeParamSet map[string]bool, callEnv *runtime.Environment, this *runtime.Instance) {
	if spec == nil || spec.Operator != "" || value == nil {
		return
	}
	if len(spec.Arguments) == 0 {
		if !typeParamSet[strings.ToLower(spec.Name)] {
			return
		}
		e.recordInferredBinding(spec.Name, value.TypeName(), callEnv, this)
		return
	}
	switch strings.ToLower(spec.Name) {
	case "list":
		list, ok := value.(*runtime.List)
		if !ok || len(list.Elements) == 0 || len(spec.Arguments) == 0 {
			return
		}
		e.inferGenericBindingsFromTypeRef(spec.Arguments[0], list.Elements[0], typeParamSet, callEnv, this)
	case "set":
		set, ok := value.(runtime.Set)
		if !ok || len(set.Elements) == 0 || len(spec.Arguments) == 0 {
			return
		}
		for _, entry := range set.Elements {
			e.inferGenericBindingsFromTypeRef(spec.Arguments[0], entry.Value, typeParamSet, callEnv, this)
			break
		}
	case "dict":
		if len(spec.Arguments) != 2 {
			return
		}
		d, ok := value.(runtime.Dict)
		if !ok || d.Len() == 0 {
			return
		}
		d.ForEachEntry(func(_ string, entry runtime.DictEntry) bool {
			e.inferGenericBindingsFromTypeRef(spec.Arguments[0], entry.Key, typeParamSet, callEnv, this)
			e.inferGenericBindingsFromTypeRef(spec.Arguments[1], entry.Value, typeParamSet, callEnv, this)
			return false
		})
	}
}

// recordInferredBinding stores a discovered (type-param-name, type-name)
// binding into the call environment and, for constructor calls, into
// the instance's TypeBindings so reflect.typeBindings can surface it.
// "skip if already bound" semantics match the legacy non-recursive
// code so explicit `<T=...>` args planted by the caller win.
func (e *Evaluator) recordInferredBinding(name, typeName string, callEnv *runtime.Environment, this *runtime.Instance) {
	if name == "" || typeName == "" {
		return
	}
	if _, already := callEnv.GetTypeBinding(name); !already {
		callEnv.DefineTypeBinding(name, typeName)
	}
	if this == nil || this.Class == nil {
		return
	}
	if _, alreadyBound := this.TypeBindings[name]; alreadyBound {
		return
	}
	for _, cp := range this.Class.TypeParameters {
		if strings.EqualFold(cp, name) {
			if this.TypeBindings == nil {
				this.TypeBindings = map[string]string{}
			}
			this.TypeBindings[name] = typeName
			return
		}
	}
}

func (e *Evaluator) applyFunctionWithTypeArgs(fn runtime.Function, args []runtime.Value, typeArgs []*ast.TypeRef) (runtime.Value, error) {
	if len(typeArgs) == 0 || len(fn.TypeParameters) == 0 {
		return e.applyFunction(fn, args)
	}
	merged := map[string]string{}
	for k, v := range fn.TypeBindings {
		merged[k] = v
	}
	for i, t := range typeArgs {
		if i >= len(fn.TypeParameters) {
			break
		}
		if t == nil || t.Operator != "" || t.Name == "" {
			continue
		}
		merged[fn.TypeParameters[i]] = t.Name
	}
	clone := fn
	clone.TypeBindings = merged
	return e.applyFunction(clone, args)
}

func (e *Evaluator) applyFunctionWithThis(fn runtime.Function, args []runtime.Value, this *runtime.Instance) (runtime.Value, error) {
	if fn.IsGenerator {
		return e.lazyGenerator(fn, args, this), nil
	}
	if fn.Async {
		return e.startAsyncFunction(fn, args, this), nil
	}
	if fn.Native != nil {
		return fn.Native(this, args)
	}
	return e.applyFunctionWithThisSync(fn, args, this)
}

// applyEnumMethod invokes an enum instance method with `this` bound to the
// receiving variant. The variant flows through pendingEnumThis to the call-env
// setup (and into generator/async children) since `this` is not a *Instance.
func (e *Evaluator) applyEnumMethod(fn runtime.Function, args []runtime.Value, receiver runtime.EnumVariant) (runtime.Value, error) {
	e.pendingEnumThis = receiver
	if fn.IsGenerator || fn.Async {
		// The generator/async child consumes pendingEnumThis at body start.
		e.enumThisForChild = receiver
		defer func() { e.enumThisForChild = nil }()
	}
	return e.applyFunctionWithThis(fn, args, nil)
}

func (e *Evaluator) applyFunctionWithThisSync(fn runtime.Function, args []runtime.Value, this *runtime.Instance) (runtime.Value, error) {
	if fn.IsGenerator {
		return e.lazyGenerator(fn, args, this), nil
	}
	if fn.Native != nil {
		return fn.Native(this, args)
	}
	maxDepth := e.maxCallDepth
	if maxDepth <= 0 {
		maxDepth = DefaultMaxCallDepth
	}
	if e.callDepth >= maxDepth {
		return nil, thrownError{value: e.withTrace(runtime.Error{Class: "FatalError", Message: fmt.Sprintf("maximum call depth exceeded (%d)", maxDepth), Fatal: true})}
	}
	e.callDepth++
	defer func() {
		e.callDepth--
	}()
	if fn.OwnerClass != nil {
		e.classStack = append(e.classStack, fn.OwnerClass)
		defer func() {
			e.classStack = e.classStack[:len(e.classStack)-1]
		}()
	}
	isVariadic := len(fn.Parameters) > 0 && fn.Parameters[len(fn.Parameters)-1].Variadic
	if !isVariadic && len(args) > len(fn.Parameters) {
		return nil, fmt.Errorf("%s expects at most %d arguments, got %d", binding.DisplayName(fn.Name), len(fn.Parameters), len(args))
	}
	if err := functionArgumentsMatchError(fn, args, this); err != nil {
		return nil, err
	}
	callEnv := runtime.NewEnclosedEnvironment(fn.Env)
	enumThis := e.pendingEnumThis
	e.pendingEnumThis = nil
	if this != nil {
		if err := callEnv.Define("this", this, false); err != nil {
			return nil, err
		}
	} else if enumThis != nil {
		if err := callEnv.Define("this", enumThis, false); err != nil {
			return nil, err
		}
	}
	// Plant the captured type bindings from the function's creation
	// site (the outer generic frame for a lambda or a generic-function
	// reference). Functions without TypeBindings (defined outside any
	// generic scope) skip this step.
	for name, typeName := range fn.TypeBindings {
		callEnv.DefineTypeBinding(name, typeName)
	}
	// Inherit class type bindings from receiver instance (lower priority - set first so
	// that inferred own-param bindings below can override if needed, but class params win
	// for methods since the instance binding was set at construction time).
	if this != nil {
		for name, typeName := range this.TypeBindings {
			callEnv.DefineTypeBinding(name, typeName)
		}
	}
	// Infer function type parameter bindings from params+args.
	if len(fn.TypeParameters) > 0 {
		typeParamSet := map[string]bool{}
		for _, p := range fn.TypeParameters {
			typeParamSet[strings.ToLower(p)] = true
		}
		for i, param := range fn.Parameters {
			if i >= len(args) || param.Type == nil {
				break
			}
			e.inferGenericBindingsFromTypeRef(param.Type, args[i], typeParamSet, callEnv, this)
		}
	}
	if err := e.checkTypeParamConstraints(fn, callEnv); err != nil {
		return nil, err
	}
	for i, param := range fn.Parameters {
		var value runtime.Value
		if param.Variadic {
			variadicElements := make([]runtime.Value, 0)
			if i < len(args) {
				// args may carry nil holes for unfilled default slots.
				for _, v := range args[i:] {
					if v != nil {
						variadicElements = append(variadicElements, v)
					}
				}
			}
			value = &runtime.List{Elements: variadicElements}
		} else if i < len(args) && args[i] != nil {
			value = args[i]
		} else if param.Default != nil {
			evaluated, err := e.evalExpression(param.Default, callEnv)
			if err != nil {
				return nil, err
			}
			value = evaluated
		} else {
			return nil, fmt.Errorf("%s missing argument %s", binding.DisplayName(fn.Name), param.Name.Value)
		}
		if param.Const {
			value = runtime.FreezeShallowCopy(value)
		}
		if err := callEnv.Define(param.Name.Value, value, false); err != nil {
			return nil, err
		}
	}
	e.pushDeferFrame()
	e.pushFunction(&fn, callEnv)
	defer e.popFunction()
	sig, err := e.evalBlock(fn.Body, callEnv)
	if err != nil {
		return nil, err
	}
	sig, err = e.runAndPopDefers(sig)
	if err != nil {
		return nil, err
	}
	if sig.exited {
		return exitValue{code: sig.exitCode}, nil
	}
	if sig.kind == "return" {
		return sig.value, nil
	}
	if sig.kind == "break" || sig.kind == "continue" {
		return nil, fmt.Errorf("%s cannot leave a function body", sig.kind)
	}
	if sig.kind == "generatorClosed" {
		return runtime.Null{}, nil
	}
	if sig.kind == "debugCancelled" {
		return nil, ErrDebugEvaluationCancelled
	}
	if sig.kind == "throw" && sig.thrown != nil {
		return nil, thrownError{value: *sig.thrown}
	}
	return runtime.Null{}, nil
}

func (e *Evaluator) lazyGenerator(fn runtime.Function, args []runtime.Value, this *runtime.Instance) *runtime.Generator {
	enumThis := e.enumThisForChild
	items := make(chan generatorItem)
	doneCh := make(chan struct{})
	stop := sync.Once{}
	start := sync.Once{}
	var pendingErr error
	closed := false
	return runtime.NewClosableGenerator(func() (runtime.Value, bool, error) {
		start.Do(func() {
			// Construct the child evaluator on the parent goroutine to keep
			// concurrent generator starts race-clean on package-level
			// native callbacks.
			child := e.childForCallback()
			child.pendingEnumThis = enumThis
			runner := fn
			runner.IsGenerator = false
			child.pushYieldChannel(items, doneCh)
			endThread := child.startDebugThread("generator")
			go func() {
				defer endThread()
				defer close(items)
				defer child.popYieldFrame()
				value, err := child.applyFunctionWithThisSync(runner, args, this)
				if err != nil {
					select {
					case items <- generatorItem{err: err}:
					case <-doneCh:
					}
					return
				}
				if exit, ok := value.(exitValue); ok {
					select {
					case items <- generatorItem{err: fmt.Errorf("generator attempted to exit with code %d", exit.code)}:
					case <-doneCh:
					}
				}
			}()
		})
		if closed {
			return nil, false, pendingErr
		}
		item, ok := <-items
		if !ok {
			closed = true
			return nil, false, nil
		}
		if item.err != nil {
			closed = true
			pendingErr = item.err
			return nil, false, item.err
		}
		return item.value, true, nil
	}, func() {
		stop.Do(func() {
			close(doneCh)
		})
	})
}

func (e *Evaluator) startAsyncFunction(fn runtime.Function, args []runtime.Value, this *runtime.Instance) *runtime.Task {
	task := runtime.NewTask()
	// Build the child evaluator on the parent's goroutine; spawning it
	// inside the goroutine races with parallel async calls on writes to
	// package-level native callbacks (InstanceInvoker, ClassDeserializer).
	child := e.childForCallback()
	child.pendingEnumThis = e.enumThisForChild
	endThread := child.startDebugThread("async worker")
	runtime.AsyncEnter()
	go func() {
		defer endThread()
		defer runtime.AsyncLeave()
		value, err := child.applyFunctionWithThisSync(fn, args, this)
		if exit, ok := value.(exitValue); ok && err == nil {
			err = fmt.Errorf("async function attempted to exit with code %d", exit.code)
			value = runtime.Null{}
		}
		task.Complete(value, err)
	}()
	return task
}

func awaitValue(value runtime.Value) (runtime.Value, error) {
	task, ok := value.(*runtime.Task)
	if !ok {
		return value, nil
	}
	result := task.Await()
	if result.Err != nil {
		return nil, result.Err
	}
	if result.Value == nil {
		return runtime.Null{}, nil
	}
	return result.Value, nil
}

func bindThis(env *runtime.Environment, this *runtime.Instance) *runtime.Environment {
	bound := runtime.NewEnclosedEnvironment(env)
	_ = bound.Define("this", this, false)
	return bound
}

func (e *Evaluator) pushFunction(fn *runtime.Function, env *runtime.Environment) {
	line := 0
	if fn != nil && fn.Body != nil {
		line = fn.Body.Token.Line
	}
	name := "<closure>"
	if fn != nil && fn.Name != "" {
		name = fn.Name
		// Qualify methods by declaring class to match the VM registry.
		if fn.OwnerClass != nil {
			name = fn.OwnerClass.Name + "." + name
		}
	}
	if len(e.callStack) == 0 {
		e.topLevelLine = e.currentLine
	}
	e.callStack = append(e.callStack, evalFrame{name: name, line: line, callLine: e.currentLine, fn: fn, env: env})
}

func (e *Evaluator) popFunction() {
	if len(e.callStack) > 0 {
		e.callStack = e.callStack[:len(e.callStack)-1]
	}
}

func (e *Evaluator) currentFunction() *runtime.Function {
	if len(e.callStack) == 0 {
		return nil
	}
	return e.callStack[len(e.callStack)-1].fn
}

// captureFrames maps the eval call stack to contract frames: frame i displays the line where it called inward (callStack[i+1].callLine), innermost displays errorLine via CallLine 0.
func (e *Evaluator) captureFrames() ([]runtime.StackFrame, int, int) {
	n := len(e.callStack)
	frames := make([]runtime.StackFrame, 0, n)
	for i := n - 1; i >= 0; i-- {
		line := 0
		if i+1 < n {
			line = e.callStack[i+1].callLine
		}
		frames = append(frames, runtime.StackFrame{Name: e.callStack[i].name, CallLine: line})
	}
	topLevelLine := e.topLevelLine
	if n > 0 {
		topLevelLine = e.callStack[0].callLine
	} else {
		// No frames: the throw/fault happened at module top level; show its line.
		topLevelLine = e.currentLine
	}
	return runtime.CollapseFrames(frames), e.currentLine, topLevelLine
}

func (e *Evaluator) withTrace(err runtime.Error) runtime.Error {
	if !err.HasStackTrace() {
		err.TraceFrames, err.ErrorLine, err.TopLevelLine = e.captureFrames()
	}
	return err
}

func (e *Evaluator) pushYieldFrame() {
	e.yieldFrames = append(e.yieldFrames, &yieldFrame{})
}

func (e *Evaluator) pushYieldChannel(ch chan generatorItem, done <-chan struct{}) {
	e.yieldFrames = append(e.yieldFrames, &yieldFrame{ch: ch, done: done})
}

func (e *Evaluator) appendYield(value runtime.Value) (bool, error) {
	if len(e.yieldFrames) == 0 {
		return false, fmt.Errorf("yield can only be used inside a generator function")
	}
	index := len(e.yieldFrames) - 1
	frame := e.yieldFrames[index]
	if frame.ch != nil {
		select {
		case frame.ch <- generatorItem{value: value}:
			return false, nil
		case <-frame.done:
			return true, nil
		}
	}
	frame.values = append(frame.values, value)
	return false, nil
}

func (e *Evaluator) popYieldFrame() []runtime.Value {
	if len(e.yieldFrames) == 0 {
		return nil
	}
	index := len(e.yieldFrames) - 1
	values := e.yieldFrames[index].values
	e.yieldFrames = e.yieldFrames[:index]
	return values
}

func (e *Evaluator) pushDeferFrame() {
	e.deferFrames = append(e.deferFrames, &deferFrame{})
}

func (e *Evaluator) registerDefer(expr ast.Expression, env *runtime.Environment) {
	if len(e.deferFrames) == 0 {
		e.pushDeferFrame()
	}
	frame := e.deferFrames[len(e.deferFrames)-1]
	frame.calls = append(frame.calls, deferredCall{expr: expr, env: env})
}

// freezeDeferredCall freezes a deferred call's arguments and callee to their registration-time values via PrecomputedValue nodes on a shallow copy (the original AST is never mutated so loop bodies re-freeze per iteration); non-call defer expressions pass through.
func (e *Evaluator) freezeDeferredCall(expr ast.Expression, env *runtime.Environment) (ast.Expression, error) {
	call, ok := expr.(*ast.CallExpression)
	if !ok {
		return expr, nil
	}
	frozen := *call
	calleeValue, frozenCallee, err := e.freezeDeferCallee(call.Callee, env)
	if err != nil {
		return nil, err
	}
	frozen.Callee = frozenCallee
	hints := overloadParamTypeHints(overloadsFromValue(calleeValue), len(call.Arguments), nil)
	frozen.Arguments = make([]ast.CallArgument, len(call.Arguments))
	for i, arg := range call.Arguments {
		var hint *ast.TypeRef
		if hints != nil && i < len(hints) {
			hint = hints[i]
		}
		var value runtime.Value
		if hint != nil {
			value, err = e.evalExpressionWithExpectedType(arg.Value, env, hint)
		} else {
			value, err = e.evalExpression(arg.Value, env)
		}
		if err != nil {
			return nil, err
		}
		newArg := arg
		newArg.Value = &ast.PrecomputedValue{Token: call.Token, Value: value}
		frozen.Arguments[i] = newArg
	}
	return &frozen, nil
}

// freezeDeferCallee freezes a deferred method call's receiver, or the whole callable when it resolves to a value; the returned value (non-nil only for whole-callee freezes) drives argument hint fidelity.
func (e *Evaluator) freezeDeferCallee(callee ast.Expression, env *runtime.Environment) (runtime.Value, ast.Expression, error) {
	switch c := callee.(type) {
	case *ast.Identifier:
		// Freeze a variable-held callable or declared function to the value bound now (a later local can shadow the name).
		value, ok := env.Get(c.Value)
		if !ok {
			return nil, callee, nil
		}
		return value, &ast.PrecomputedValue{Token: c.Token, Value: value}, nil
	case *ast.SelectorExpression:
		if objIdent, ok := c.Object.(*ast.Identifier); ok {
			if objVal, found := env.Get(objIdent.Value); found {
				if _, isInstance := objVal.(*runtime.Instance); isInstance {
					// Instance receiver: freeze it and keep method dispatch (overload selection) at pop, matching the VM's OpDeferMethodCall.
					newSel := *c
					newSel.Object = &ast.PrecomputedValue{Token: objIdent.Token, Value: objVal}
					return nil, &newSel, nil
				}
			}
		}
		// Module-qualified, static (Class.method), or nested selector: freeze the whole callable when it resolves to a value; otherwise leave it (a native like io.println is not a first-class value) and rely on frozen arguments.
		value, err := e.evalExpression(callee, env)
		if err != nil {
			return nil, callee, nil
		}
		return value, &ast.PrecomputedValue{Token: c.Token, Value: value}, nil
	default:
		value, err := e.evalExpression(callee, env)
		if err != nil {
			return nil, callee, nil
		}
		return value, &ast.PrecomputedValue{Value: value}, nil
	}
}

func overloadsFromValue(v runtime.Value) []runtime.Function {
	switch f := v.(type) {
	case runtime.Function:
		return []runtime.Function{f}
	case runtime.OverloadedFunction:
		return f.Overloads
	}
	return nil
}

func (e *Evaluator) runAndPopDefers(sig signal) (signal, error) {
	if len(e.deferFrames) == 0 {
		return sig, nil
	}
	frame := e.deferFrames[len(e.deferFrames)-1]
	e.deferFrames = e.deferFrames[:len(e.deferFrames)-1]
	for i := len(frame.calls) - 1; i >= 0; i-- {
		value, err := e.evalExpression(frame.calls[i].expr, frame.calls[i].env)
		if err != nil {
			return signal{}, err
		}
		if exit, ok := value.(exitValue); ok {
			return signal{exited: true, exitCode: exit.code}, nil
		}
	}
	return sig, nil
}

func (e *Evaluator) evalIncrement(expr ast.Expression, operator string, env *runtime.Environment) (runtime.Value, error) {
	ident, ok := expr.(*ast.Identifier)
	if !ok {
		return nil, fmt.Errorf("%s currently supports identifiers only", operator)
	}
	current, ok := env.Get(ident.Value)
	if !ok {
		return nil, fmt.Errorf("%q is not declared", ident.Value)
	}
	intValue, ok := current.(runtime.Int)
	if !ok {
		return nil, fmt.Errorf("%s currently supports int values only", operator)
	}
	next := new(big.Int).Set(intValue.Value)
	if operator == "++" {
		next.Add(next, big.NewInt(1))
	} else {
		next.Sub(next, big.NewInt(1))
	}
	value := runtime.Int{Value: next}
	if err := env.Assign(ident.Value, value); err != nil {
		return nil, err
	}
	return value, nil
}

type exitValue struct {
	code int
}

func (v exitValue) TypeName() string { return "exit" }
func (v exitValue) Inspect() string  { return fmt.Sprintf("exit(%d)", v.code) }

func selectorName(expr ast.Expression) (string, string, bool) {
	selector, ok := expr.(*ast.SelectorExpression)
	if !ok {
		return "", "", false
	}
	ident, ok := selector.Object.(*ast.Identifier)
	if !ok {
		return "", "", false
	}
	return ident.Value, selector.Name.Value, true
}

func (e *Evaluator) singlePrintableValue(call *ast.CallExpression, args []runtime.Value) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("%s expects exactly one argument", call.Callee.String())
	}
	return e.displayString(args[0])
}

func singleIntValue(call *ast.CallExpression, args []runtime.Value) (int64, error) {
	if len(args) != 1 {
		return 0, fmt.Errorf("%s expects exactly one argument", call.Callee.String())
	}
	arg, ok := args[0].(runtime.Int)
	if !ok {
		return 0, fmt.Errorf("%s expects an integer argument", call.Callee.String())
	}
	if !arg.Value.IsInt64() {
		return 0, fmt.Errorf("%s integer argument is out of int64 range", call.Callee.String())
	}
	return arg.Value.Int64(), nil
}

func singleStringValue(call *ast.CallExpression, args []runtime.Value) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("%s expects exactly one argument", call.Callee.String())
	}
	arg, ok := args[0].(runtime.String)
	if !ok {
		return "", fmt.Errorf("%s expects a string argument", call.Callee.String())
	}
	return arg.Value, nil
}

func (e *Evaluator) evalPrefix(operator string, right runtime.Value) (runtime.Value, error) {
	if value, ok, err := e.evalPrefixOperatorMethod(operator, right); ok || err != nil {
		return value, err
	}
	switch operator {
	case "!":
		value, ok := right.(runtime.Bool)
		if !ok {
			return nil, fmt.Errorf("! expects bool, got %s", right.TypeName())
		}
		return runtime.Bool{Value: !value.Value}, nil
	case "-":
		switch value := right.(type) {
		case runtime.Int:
			return runtime.Int{Value: new(big.Int).Neg(value.Value)}, nil
		case runtime.Decimal:
			return runtime.Decimal{Value: new(big.Rat).Neg(value.Value)}, nil
		case runtime.Float:
			return runtime.Float{Value: -value.Value}, nil
		default:
			if result, handled, err := native.UnaryMinusValue(right); handled {
				if err != nil {
					return nil, err
				}
				return result, nil
			}
			return nil, fmt.Errorf("- expects numeric value, got %s", right.TypeName())
		}
	case "~":
		value, ok := right.(runtime.Int)
		if !ok {
			return nil, fmt.Errorf("~ expects int, got %s", right.TypeName())
		}
		return runtime.Int{Value: new(big.Int).Not(value.Value)}, nil
	default:
		return nil, fmt.Errorf("unsupported prefix operator %q", operator)
	}
}

func (e *Evaluator) evalPrefixOperatorMethod(operator string, right runtime.Value) (runtime.Value, bool, error) {
	instance, ok := right.(*runtime.Instance)
	if !ok {
		return nil, false, nil
	}
	name, ok := prefixOperatorMethodName(operator)
	if !ok {
		return nil, false, nil
	}
	method, ok := lookupMethod(instance.Class, name)
	if !ok {
		return nil, false, nil
	}
	value, err := e.applyFunctionWithThis(method, nil, instance)
	if err != nil {
		return nil, true, err
	}
	return value, true, nil
}

func prefixOperatorMethodName(operator string) (string, bool) {
	switch operator {
	case "!":
		return "__not", true
	case "-":
		return "__neg", true
	case "~":
		return "__bitnot", true
	default:
		return "", false
	}
}

func (e *Evaluator) evalInfix(operator string, left runtime.Value, right runtime.Value) (runtime.Value, error) {
	if operator == "&&" || operator == "||" || operator == "xor" {
		return evalBoolInfix(operator, left, right)
	}
	if operator == "==" || operator == "!=" {
		equal, err := e.valuesEqual(left, right)
		if err != nil {
			return nil, err
		}
		if operator == "!=" {
			equal = !equal
		}
		return runtime.Bool{Value: equal}, nil
	}
	if operator == "is" || operator == "is not" {
		same := valuesIdentical(left, right)
		if operator == "is not" {
			same = !same
		}
		return runtime.Bool{Value: same}, nil
	}
	if operator == "in" {
		return e.evalContains(left, right)
	}
	if value, ok, err := e.evalOperatorMethod(operator, left, right); ok || err != nil {
		return value, err
	}
	if left.TypeName() == "string" && right.TypeName() == "string" && operator == "+" {
		return runtime.String{Value: left.(runtime.String).Value + right.(runtime.String).Value}, nil
	}
	if isComparison(operator) {
		return evalComparison(operator, left, right)
	}
	return evalNumericInfix(operator, left, right)
}

// evalContains implements `needle in container` membership: element for
// lists, key for dicts, member for sets, substring for strings, range
// membership, and `__contains` dispatch for user objects.
func (e *Evaluator) evalContains(needle, container runtime.Value) (runtime.Value, error) {
	switch c := container.(type) {
	case *runtime.List:
		for _, el := range c.Elements {
			eq, err := e.valuesEqual(needle, el)
			if err != nil {
				return nil, err
			}
			if eq {
				return runtime.Bool{Value: true}, nil
			}
		}
		return runtime.Bool{Value: false}, nil
	case runtime.Dict:
		_, ok := c.GetEntry(dictKey(needle))
		return runtime.Bool{Value: ok}, nil
	case runtime.Set:
		_, ok := c.Elements[dictKey(needle)]
		return runtime.Bool{Value: ok}, nil
	case runtime.String:
		s, ok := needle.(runtime.String)
		if !ok {
			return nil, fmt.Errorf("in: left operand must be a string when the right operand is a string")
		}
		return runtime.Bool{Value: strings.Contains(c.Value, s.Value)}, nil
	case runtime.Range:
		n, ok := native.IntValueToBigInt(needle)
		if !ok {
			return runtime.Bool{Value: false}, nil
		}
		return runtime.Bool{Value: c.ContainsInt(n)}, nil
	case *runtime.Instance:
		if method, ok := lookupMethod(c.Class, "__contains"); ok {
			bound := method
			bound.Env = bindThis(method.Env, c)
			return e.applyFunctionWithThis(bound, []runtime.Value{needle}, c)
		}
		return nil, fmt.Errorf("%s does not support 'in' (define __contains)", c.TypeName())
	default:
		return nil, fmt.Errorf("'in' requires a list, dict, set, string, range, or an object with __contains, got %s", container.TypeName())
	}
}

func (e *Evaluator) evalOperatorMethod(operator string, left runtime.Value, right runtime.Value) (runtime.Value, bool, error) {
	if isComparison(operator) {
		return e.evalComparisonMethod(operator, left, right)
	}
	instance, ok := left.(*runtime.Instance)
	if !ok {
		return nil, false, nil
	}
	name, ok := operatorMethodName(operator)
	if !ok {
		return nil, false, nil
	}
	method, ok := lookupMethod(instance.Class, name)
	if !ok {
		return nil, false, nil
	}
	value, err := e.applyFunctionWithThis(method, []runtime.Value{right}, instance)
	if err != nil {
		return nil, true, err
	}
	if isComparison(operator) {
		if _, ok := value.(runtime.Bool); !ok {
			return nil, true, fmt.Errorf("%s.%s must return bool", instance.Class.Name, name)
		}
	}
	return value, true, nil
}

// comparisonAttempt is one step in the ordering-dunder resolution
// chain shared (by construction) with the VM's compare: direct dunder,
// then negated inverse on the left, then swapped operands on the right.
type comparisonAttempt struct {
	name   string
	recv   runtime.Value
	arg    runtime.Value
	negate bool
}

func comparisonAttempts(operator string, left, right runtime.Value) []comparisonAttempt {
	switch operator {
	case "<":
		return []comparisonAttempt{{"__lt", left, right, false}, {"__gt", right, left, false}}
	case ">":
		return []comparisonAttempt{{"__gt", left, right, false}, {"__lt", right, left, false}}
	case "<=":
		return []comparisonAttempt{{"__lte", left, right, false}, {"__gt", left, right, true}, {"__lt", right, left, true}}
	case ">=":
		return []comparisonAttempt{{"__gte", left, right, false}, {"__lt", left, right, true}, {"__gt", right, left, true}}
	default:
		return nil
	}
}

func (e *Evaluator) evalComparisonMethod(operator string, left, right runtime.Value) (runtime.Value, bool, error) {
	// Dunder comparison needs an instance receiver on one side; skip
	// the attempt-list allocation for primitive comparisons.
	if _, ok := left.(*runtime.Instance); !ok {
		if _, ok := right.(*runtime.Instance); !ok {
			return nil, false, nil
		}
	}
	for _, at := range comparisonAttempts(operator, left, right) {
		instance, ok := at.recv.(*runtime.Instance)
		if !ok {
			continue
		}
		method, ok := lookupMethod(instance.Class, at.name)
		if !ok {
			continue
		}
		value, err := e.applyFunctionWithThis(method, []runtime.Value{at.arg}, instance)
		if err != nil {
			return nil, true, err
		}
		result, ok := value.(runtime.Bool)
		if !ok {
			return nil, true, fmt.Errorf("%s.%s must return bool", instance.Class.Name, at.name)
		}
		if at.negate {
			result.Value = !result.Value
		}
		return result, true, nil
	}
	return nil, false, nil
}

func operatorMethodName(operator string) (string, bool) {
	switch operator {
	case "+":
		return "__add", true
	case "-":
		return "__sub", true
	case "*":
		return "__mul", true
	case "/":
		return "__div", true
	case "//":
		return "__intdiv", true
	case "%":
		return "__mod", true
	case "**":
		return "__pow", true
	case "<":
		return "__lt", true
	case "<=":
		return "__lte", true
	case ">":
		return "__gt", true
	case ">=":
		return "__gte", true
	case "&":
		return "__bitand", true
	case "|":
		return "__bitor", true
	case "^":
		return "__bitxor", true
	case "<<":
		return "__lshift", true
	case ">>":
		return "__rshift", true
	default:
		return "", false
	}
}

func (e *Evaluator) valuesEqual(left runtime.Value, right runtime.Value) (bool, error) {
	if ev, ok := left.(runtime.EnumVariant); ok {
		rv, ok := right.(runtime.EnumVariant)
		if !ok || ev.Enum != rv.Enum || ev.Variant != rv.Variant || len(ev.Fields) != len(rv.Fields) {
			return false, nil
		}
		for i, f := range ev.Fields {
			eq, err := e.valuesEqual(f, rv.Fields[i])
			if !eq || err != nil {
				return eq, err
			}
		}
		return true, nil
	}
	if instance, ok := left.(*runtime.Instance); ok {
		if method, ok := lookupMethod(instance.Class, "__eq"); ok {
			value, err := e.applyFunctionWithThis(method, []runtime.Value{right}, instance)
			if err != nil {
				return false, err
			}
			result, ok := value.(runtime.Bool)
			if !ok {
				return false, fmt.Errorf("%s.__eq must return bool", instance.Class.Name)
			}
			return result.Value, nil
		}
		other, ok := right.(*runtime.Instance)
		if !ok || other.Class != instance.Class {
			return false, nil
		}
		if len(instance.Fields) != len(other.Fields) {
			return false, nil
		}
		for name, leftField := range instance.Fields {
			rightField, ok := other.Fields[name]
			if !ok {
				return false, nil
			}
			equal, err := e.valuesEqual(leftField, rightField)
			if err != nil {
				return false, err
			}
			if !equal {
				return false, nil
			}
		}
		return true, nil
	}
	// Container recursion routes back through valuesEqual so a nested instance compares exactly as it does at top level (structural, honoring __eq), not by identity.
	switch leftValue := left.(type) {
	case *runtime.List:
		rightValue, ok := right.(*runtime.List)
		if !ok || len(leftValue.Elements) != len(rightValue.Elements) {
			return false, nil
		}
		for i, element := range leftValue.Elements {
			equal, err := e.valuesEqual(element, rightValue.Elements[i])
			if err != nil || !equal {
				return equal, err
			}
		}
		return true, nil
	case runtime.Dict:
		rightValue, ok := right.(runtime.Dict)
		if !ok || leftValue.Len() != rightValue.Len() {
			return false, nil
		}
		equal := true
		var iterErr error
		leftValue.ForEachEntry(func(key string, entry runtime.DictEntry) bool {
			other, ok := rightValue.GetEntry(key)
			if !ok {
				equal = false
				return false
			}
			keyEq, err := e.valuesEqual(entry.Key, other.Key)
			if err != nil {
				iterErr, equal = err, false
				return false
			}
			valEq, err := e.valuesEqual(entry.Value, other.Value)
			if err != nil {
				iterErr, equal = err, false
				return false
			}
			if !keyEq || !valEq {
				equal = false
				return false
			}
			return true
		})
		return equal, iterErr
	case runtime.Set:
		rightValue, ok := right.(runtime.Set)
		if !ok || len(leftValue.Elements) != len(rightValue.Elements) {
			return false, nil
		}
		for key, entry := range leftValue.Elements {
			other, ok := rightValue.Elements[key]
			if !ok {
				return false, nil
			}
			equal, err := e.valuesEqual(entry.Value, other.Value)
			if err != nil || !equal {
				return equal, err
			}
		}
		return true, nil
	}
	return primitiveEqual(left, right), nil
}

func valuesIdentical(left runtime.Value, right runtime.Value) bool {
	switch l := left.(type) {
	case *runtime.Instance:
		r, ok := right.(*runtime.Instance)
		return ok && l == r
	case *runtime.Class:
		r, ok := right.(*runtime.Class)
		return ok && l == r
	case runtime.Null:
		_, ok := right.(runtime.Null)
		return ok
	default:
		return primitiveEqual(left, right)
	}
}

func primitiveEqual(left runtime.Value, right runtime.Value) bool {
	if eq, both := runtime.NumericValuesEqual(left, right); both {
		return eq
	}
	switch leftValue := left.(type) {
	case *runtime.List:
		rightValue, ok := right.(*runtime.List)
		if !ok || len(leftValue.Elements) != len(rightValue.Elements) {
			return false
		}
		for i, element := range leftValue.Elements {
			if !primitiveEqual(element, rightValue.Elements[i]) {
				return false
			}
		}
		return true
	case runtime.Dict:
		rightValue, ok := right.(runtime.Dict)
		if !ok || leftValue.Len() != rightValue.Len() {
			return false
		}
		equal := true
		leftValue.ForEachEntry(func(key string, entry runtime.DictEntry) bool {
			other, ok := rightValue.GetEntry(key)
			if !ok || !primitiveEqual(entry.Key, other.Key) || !primitiveEqual(entry.Value, other.Value) {
				equal = false
				return false
			}
			return true
		})
		return equal
	case runtime.Set:
		rightValue, ok := right.(runtime.Set)
		if !ok || len(leftValue.Elements) != len(rightValue.Elements) {
			return false
		}
		for key, entry := range leftValue.Elements {
			other, ok := rightValue.Elements[key]
			if !ok || !primitiveEqual(entry.Value, other.Value) {
				return false
			}
		}
		return true
	case *runtime.Instance:
		rightValue, ok := right.(*runtime.Instance)
		return ok && leftValue == rightValue
	}
	eq, _ := runtime.LeafValuesEqual(left, right)
	return eq
}

func evalBoolInfix(operator string, left runtime.Value, right runtime.Value) (runtime.Value, error) {
	l, ok := left.(runtime.Bool)
	if !ok {
		return nil, fmt.Errorf("%s expects bool operands", operator)
	}
	r, ok := right.(runtime.Bool)
	if !ok {
		return nil, fmt.Errorf("%s expects bool operands", operator)
	}
	switch operator {
	case "&&":
		return runtime.Bool{Value: l.Value && r.Value}, nil
	case "||":
		return runtime.Bool{Value: l.Value || r.Value}, nil
	case "xor":
		return runtime.Bool{Value: l.Value != r.Value}, nil
	default:
		return nil, fmt.Errorf("unsupported bool operator %q", operator)
	}
}

func evalNumericInfix(operator string, left runtime.Value, right runtime.Value) (runtime.Value, error) {
	// Promote SmallInt to Int so the rest of this dispatcher (and
	// the cascading per-type infix paths) only has to handle the
	// big.Int representation. SmallInt arrives here from native
	// integer sources (json parser, csv, etc.) that produce the
	// runtime's interface-inline int. The conversion allocates
	// O(1) big.Int per arithmetic step; the storage benefit of
	// SmallInt at parse-time pays for it as long as the loop body
	// doesn't run arithmetic on every value.
	if l, ok := left.(runtime.SmallInt); ok {
		if r, ok := right.(runtime.SmallInt); ok {
			if value, handled, err := evalSmallIntInfix(operator, l.Value, r.Value); handled {
				return value, err
			}
		}
	}
	if s, ok := left.(runtime.SmallInt); ok {
		left = runtime.NewInt64(s.Value)
	}
	if s, ok := right.(runtime.SmallInt); ok {
		right = runtime.NewInt64(s.Value)
	}
	switch l := left.(type) {
	case runtime.Int:
		switch r := right.(type) {
		case runtime.Int:
			return evalIntInfix(operator, l, r)
		case runtime.Decimal:
			return evalDecimalInfix(operator, intToDecimal(l), r)
		case runtime.Float:
			return evalFloatInfix(operator, intToFloatValue(l), r)
		}
	case runtime.Decimal:
		switch r := right.(type) {
		case runtime.Int:
			return evalDecimalInfix(operator, l, intToDecimal(r))
		case runtime.Decimal:
			return evalDecimalInfix(operator, l, r)
		case runtime.Float:
			return nil, decimalFloatArithError(operator, left, right)
		}
	case runtime.Float:
		switch r := right.(type) {
		case runtime.Float:
			return evalFloatInfix(operator, l, r)
		case runtime.Int:
			return evalFloatInfix(operator, l, intToFloatValue(r))
		case runtime.Decimal:
			return nil, decimalFloatArithError(operator, left, right)
		}
	}
	if result, handled, err := native.BinaryOperatorValue(operator, left, right); handled {
		if err != nil {
			return nil, err
		}
		return result, nil
	}
	return nil, native.UnsupportedOperandsError(operator, left.TypeName(), right.TypeName())
}

func intToFloatValue(v runtime.Int) runtime.Float {
	f, _ := runtime.NumericToFloat(v)
	return runtime.Float{Value: f}
}

// decimalFloatArithError reports the one remaining precision wall: arithmetic
// mixing decimal and float, which would silently lose decimal exactness.
func decimalFloatArithError(operator string, left, right runtime.Value) error {
	return fmt.Errorf("cannot mix decimal and float in %s (got %s and %s): cast one side - 'as float' drops decimal exactness, 'as decimal' adopts the float's imprecision", operator, left.TypeName(), right.TypeName())
}

// evalSmallIntInfix is the allocation-free integer fast path, mirroring
// the VM's smallIntBinary exactly: floor // and %, overflow promotion
// to big Int. Operators it does not cover report handled=false and
// fall through to the big-Int dispatcher.
func evalSmallIntInfix(operator string, l int64, r int64) (runtime.Value, bool, error) {
	switch operator {
	case "+":
		result := l + r
		if (l^result)&(r^result) < 0 {
			return runtime.Int{Value: new(big.Int).Add(big.NewInt(l), big.NewInt(r))}, true, nil
		}
		return runtime.SmallInt{Value: result}, true, nil
	case "-":
		result := l - r
		if (l^r)&(l^result) < 0 {
			return runtime.Int{Value: new(big.Int).Sub(big.NewInt(l), big.NewInt(r))}, true, nil
		}
		return runtime.SmallInt{Value: result}, true, nil
	case "*":
		result := l * r
		if l != 0 && result/l != r {
			return runtime.Int{Value: new(big.Int).Mul(big.NewInt(l), big.NewInt(r))}, true, nil
		}
		return runtime.SmallInt{Value: result}, true, nil
	case "//":
		if r == 0 {
			return nil, true, fmt.Errorf("integer division by zero")
		}
		if l == math.MinInt64 && r == -1 {
			return runtime.Int{Value: new(big.Int).Neg(big.NewInt(l))}, true, nil
		}
		q := l / r
		rem := l - q*r
		if rem != 0 && ((l < 0) != (r < 0)) {
			q--
		}
		return runtime.SmallInt{Value: q}, true, nil
	case "%":
		if r == 0 {
			return nil, true, fmt.Errorf("modulo by zero")
		}
		m := l % r
		if m != 0 && ((l < 0) != (r < 0)) {
			m += r
		}
		return runtime.SmallInt{Value: m}, true, nil
	case "**":
		if r < 0 {
			return nil, true, fmt.Errorf("int exponent must be a non-negative int64")
		}
		result := new(big.Int).Exp(big.NewInt(l), big.NewInt(r), nil)
		if result.IsInt64() {
			return runtime.SmallInt{Value: result.Int64()}, true, nil
		}
		return runtime.Int{Value: result}, true, nil
	}
	return nil, false, nil
}

func evalIntInfix(operator string, left runtime.Int, right runtime.Int) (runtime.Value, error) {
	switch operator {
	case "+":
		return runtime.Int{Value: new(big.Int).Add(left.Value, right.Value)}, nil
	case "-":
		return runtime.Int{Value: new(big.Int).Sub(left.Value, right.Value)}, nil
	case "*":
		return runtime.Int{Value: new(big.Int).Mul(left.Value, right.Value)}, nil
	case "//":
		if right.Value.Sign() == 0 {
			return nil, fmt.Errorf("integer division by zero")
		}
		quotient, _ := intFloorDivMod(left.Value, right.Value)
		return runtime.Int{Value: quotient}, nil
	case "%":
		if right.Value.Sign() == 0 {
			return nil, fmt.Errorf("modulo by zero")
		}
		_, remainder := intFloorDivMod(left.Value, right.Value)
		return runtime.Int{Value: remainder}, nil
	case "/":
		return evalDecimalInfix("/", intToDecimal(left), intToDecimal(right))
	case "**":
		if !right.Value.IsInt64() || right.Value.Sign() < 0 {
			return nil, fmt.Errorf("int exponent must be a non-negative int64")
		}
		return runtime.Int{Value: new(big.Int).Exp(left.Value, big.NewInt(right.Value.Int64()), nil)}, nil
	case "&":
		return runtime.Int{Value: new(big.Int).And(left.Value, right.Value)}, nil
	case "|":
		return runtime.Int{Value: new(big.Int).Or(left.Value, right.Value)}, nil
	case "^":
		return runtime.Int{Value: new(big.Int).Xor(left.Value, right.Value)}, nil
	case "<<":
		if !right.Value.IsUint64() {
			return nil, fmt.Errorf("shift amount must be a non-negative int, got %s", right.Value.String())
		}
		return runtime.Int{Value: new(big.Int).Lsh(left.Value, uint(right.Value.Uint64()))}, nil
	case ">>":
		if !right.Value.IsUint64() {
			return nil, fmt.Errorf("shift amount must be a non-negative int, got %s", right.Value.String())
		}
		return runtime.Int{Value: new(big.Int).Rsh(left.Value, uint(right.Value.Uint64()))}, nil
	default:
		return nil, fmt.Errorf("unsupported int operator %q", operator)
	}
}

func evalDecimalInfix(operator string, left runtime.Decimal, right runtime.Decimal) (runtime.Value, error) {
	switch operator {
	case "+":
		return runtime.Decimal{Value: new(big.Rat).Add(left.Value, right.Value)}, nil
	case "-":
		return runtime.Decimal{Value: new(big.Rat).Sub(left.Value, right.Value)}, nil
	case "*":
		return runtime.Decimal{Value: new(big.Rat).Mul(left.Value, right.Value)}, nil
	case "/":
		if right.Value.Sign() == 0 {
			return nil, fmt.Errorf("decimal division by zero")
		}
		return runtime.Decimal{Value: new(big.Rat).Quo(left.Value, right.Value)}, nil
	case "//":
		/* Floor division: divide, then floor. Floor toward negative
		 * infinity, not toward zero (`-7 // 2 = -4`, not -3) so the
		 * sign of a non-zero remainder matches the divisor. Mirrors
		 * Python `//` and Geblang's int//int behaviour. The result
		 * is a Decimal with an integer numerator. */
		if right.Value.Sign() == 0 {
			return nil, fmt.Errorf("decimal division by zero")
		}
		q := new(big.Rat).Quo(left.Value, right.Value)
		num := new(big.Int).Set(q.Num())
		den := q.Denom()
		floored, _ := new(big.Int).QuoRem(num, den, new(big.Int))
		if q.Sign() < 0 && new(big.Int).Mul(floored, den).Cmp(num) != 0 {
			floored.Sub(floored, big.NewInt(1))
		}
		return runtime.Decimal{Value: new(big.Rat).SetInt(floored)}, nil
	case "%":
		if right.Value.Sign() == 0 {
			return nil, fmt.Errorf("decimal modulo by zero")
		}
		q := new(big.Rat).Quo(left.Value, right.Value)
		num := new(big.Int).Set(q.Num())
		den := q.Denom()
		floored, _ := new(big.Int).QuoRem(num, den, new(big.Int))
		if q.Sign() < 0 && new(big.Int).Mul(floored, den).Cmp(num) != 0 {
			floored.Sub(floored, big.NewInt(1))
		}
		floorRat := new(big.Rat).SetInt(floored)
		product := new(big.Rat).Mul(floorRat, right.Value)
		return runtime.Decimal{Value: new(big.Rat).Sub(left.Value, product)}, nil
	default:
		return nil, fmt.Errorf("unsupported decimal operator %q", operator)
	}
}

func evalFloatInfix(operator string, left runtime.Float, right runtime.Float) (runtime.Value, error) {
	switch operator {
	case "+":
		return runtime.Float{Value: left.Value + right.Value}, nil
	case "-":
		return runtime.Float{Value: left.Value - right.Value}, nil
	case "*":
		return runtime.Float{Value: left.Value * right.Value}, nil
	case "/":
		return runtime.Float{Value: left.Value / right.Value}, nil
	case "//":
		if right.Value == 0 {
			return nil, fmt.Errorf("float division by zero")
		}
		return runtime.Float{Value: math.Floor(left.Value / right.Value)}, nil
	case "%":
		if right.Value == 0 {
			return nil, fmt.Errorf("float modulo by zero")
		}
		return runtime.Float{Value: left.Value - math.Floor(left.Value/right.Value)*right.Value}, nil
	case "**":
		return runtime.Float{Value: math.Pow(left.Value, right.Value)}, nil
	default:
		return nil, fmt.Errorf("unsupported float operator %q", operator)
	}
}

func evalComparison(operator string, left runtime.Value, right runtime.Value) (runtime.Value, error) {
	if result, handled, err := native.BinaryOperatorValue(operator, left, right); handled {
		if err != nil {
			return nil, err
		}
		return result, nil
	}
	cmp, err := compareValues(left, right)
	if err != nil {
		return nil, err
	}
	switch operator {
	case "<":
		return runtime.Bool{Value: cmp < 0}, nil
	case "<=":
		return runtime.Bool{Value: cmp <= 0}, nil
	case ">":
		return runtime.Bool{Value: cmp > 0}, nil
	case ">=":
		return runtime.Bool{Value: cmp >= 0}, nil
	default:
		return nil, fmt.Errorf("unsupported comparison operator %q", operator)
	}
}

func isTruthy(v runtime.Value) bool {
	b, ok := v.(runtime.Bool)
	return ok && b.Value
}

func (e *Evaluator) callValue(fn runtime.Value, args []runtime.Value) (runtime.Value, error) {
	if e.hofCallLine != 0 {
		// Stamp the callback frame with the HOF call site; reset for the body so an inner callback does not inherit it, restore for the next element.
		e.currentLine = e.hofCallLine
		saved := e.hofCallLine
		e.hofCallLine = 0
		defer func() { e.hofCallLine = saved }()
	}
	switch f := fn.(type) {
	case runtime.Function:
		return e.applyFunction(f, args)
	case runtime.OverloadedFunction:
		chosen, err := overload.Select(f.Name, f.Overloads, args)
		if err != nil {
			return nil, err
		}
		return e.applyFunction(chosen, args)
	case runtime.DecoratorTarget:
		if f.Callable != nil {
			return e.callValue(f.Callable, args)
		}
		return nil, fmt.Errorf("reflect target is not callable")
	case *runtime.Instance:
		method, ok := lookupMethod(f.Class, "__invoke")
		if !ok {
			return nil, fmt.Errorf("value is not callable")
		}
		return e.applyFunctionWithThis(method, args, f)
	default:
		return nil, fmt.Errorf("value is not callable")
	}
}

func (e *Evaluator) compareValues(left runtime.Value, right runtime.Value) (int, error) {
	return compareValues(left, right)
}

func compareValues(left runtime.Value, right runtime.Value) (int, error) {
	// Delegate to the shared kernel so ordering matches the bytecode VM exactly
	// (one source of truth for numeric/string comparison across backends).
	return native.NumericCompare(left, right)
}

func intToDecimal(value runtime.Int) runtime.Decimal {
	return runtime.Decimal{Value: new(big.Rat).SetInt(value.Value)}
}

func intFloorDivMod(left *big.Int, right *big.Int) (*big.Int, *big.Int) {
	quotient := new(big.Int)
	remainder := new(big.Int)
	quotient.QuoRem(left, right, remainder)
	if remainder.Sign() != 0 && ((left.Sign() < 0) != (right.Sign() < 0)) {
		quotient.Sub(quotient, big.NewInt(1))
		remainder.Add(remainder, right)
	}
	return quotient, remainder
}

func isComparison(operator string) bool {
	return operator == "<" || operator == "<=" || operator == ">" || operator == ">="
}

func dictKey(value runtime.Value) string {
	switch value := value.(type) {
	case runtime.Null:
		return "n"
	case runtime.Bool:
		if value.Value {
			return "b1"
		}
		return "b0"
	case runtime.SmallInt:
		return "i" + strconv.FormatInt(value.Value, 10)
	case runtime.Int:
		return "i" + value.Value.String()
	case runtime.Decimal:
		return "d" + value.Value.RatString()
	case runtime.Float:
		floatValue := value.Value
		if floatValue == 0 {
			floatValue = 0
		}
		return "f" + strconv.FormatFloat(floatValue, 'g', -1, 64)
	case runtime.String:
		// Single-byte type prefix; matches native.DictKey so dicts
		// look up identically whether built by VM or evaluator.
		return "s" + value.Value
	case runtime.Bytes:
		return "y" + hex.EncodeToString(value.Value)
	case *runtime.List:
		parts := make([]string, 0, len(value.Elements))
		for _, element := range value.Elements {
			parts = append(parts, dictKey(element))
		}
		return "L[" + strings.Join(parts, ",") + "]"
	case runtime.Set:
		parts := make([]string, 0, len(value.Elements))
		for key := range value.Elements {
			parts = append(parts, key)
		}
		sort.Strings(parts)
		return "S{" + strings.Join(parts, ",") + "}"
	case runtime.Dict:
		type kv struct{ k, v string }
		pairs := make([]kv, 0, value.Len())
		value.ForEachEntry(func(k string, entry runtime.DictEntry) bool {
			pairs = append(pairs, kv{k, dictKey(entry.Value)})
			return true
		})
		sort.Slice(pairs, func(i, j int) bool { return pairs[i].k < pairs[j].k })
		parts := make([]string, len(pairs))
		for i, p := range pairs {
			parts[i] = p.k + "=" + p.v
		}
		return "D{" + strings.Join(parts, ",") + "}"
	case runtime.Range:
		return "r" + value.Start.String() + ":" + value.End.String() + ":" + value.Step.String() + ":" + strconv.FormatBool(value.Exclusive)
	case runtime.BytecodeFunction:
		return fmt.Sprintf("bytecode-func:%s:%s:%d", value.Module, value.Name, value.Index)
	case runtime.BytecodeClass:
		return fmt.Sprintf("bytecode-class:%s:%s:%d", value.Module, value.Name, value.Index)
	case runtime.NativeObject:
		return fmt.Sprintf("native:%s:%d", value.Kind, value.ID)
	case runtime.Error:
		return "error:" + strconv.Quote(value.Class) + ":" + strconv.Quote(value.Message)
	case runtime.Type:
		return "type:" + strconv.Quote(value.Name)
	case *runtime.Module:
		return fmt.Sprintf("module:%p", value)
	case *runtime.Class:
		return fmt.Sprintf("class:%p", value)
	case *runtime.Interface:
		return fmt.Sprintf("interface:%p", value)
	case *runtime.Instance:
		// Delegate so a frozen instance keys by value identically to the VM.
		return native.DictKey(value)
	default:
		return fmt.Sprintf("%T:%p", value, &value)
	}
}

func cloneSetEntries(elements map[string]runtime.SetEntry) map[string]runtime.SetEntry {
	out := make(map[string]runtime.SetEntry, len(elements))
	for key, entry := range elements {
		out[key] = entry
	}
	return out
}

func orderedSetValues(value runtime.Set) []runtime.Value {
	keys := make([]string, 0, len(value.Elements))
	for key := range value.Elements {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	values := make([]runtime.Value, 0, len(keys))
	for _, key := range keys {
		values = append(values, value.Elements[key].Value)
	}
	return values
}

func indexInt(value runtime.Value) (int, error) {
	if small, ok := value.(runtime.SmallInt); ok {
		return int(small.Value), nil
	}
	intValue, ok := value.(runtime.Int)
	if !ok {
		return 0, fmt.Errorf("index must be int, got %s", value.TypeName())
	}
	if !intValue.Value.IsInt64() {
		return 0, fmt.Errorf("index is out of range")
	}
	return int(intValue.Value.Int64()), nil
}

func listElement(value *runtime.List, i int) (runtime.Value, error) {
	if i < 0 {
		i = len(value.Elements) + i
	}
	if i < 0 || i >= len(value.Elements) {
		return nil, fmt.Errorf("list index out of range")
	}
	return value.Elements[i], nil
}

func stringElement(value runtime.String, i int) (runtime.Value, error) {
	ri := runtime.StringRuneInfo(value.Value)
	n := ri.RuneCount(value.Value)
	if i < 0 {
		i = n + i
	}
	if i < 0 || i >= n {
		return nil, fmt.Errorf("string index out of range")
	}
	return runtime.String{Value: ri.RuneAt(value.Value, i)}, nil
}

func bytesElement(value runtime.Bytes, i int) (runtime.Value, error) {
	if i < 0 {
		i = len(value.Value) + i
	}
	if i < 0 || i >= len(value.Value) {
		return nil, fmt.Errorf("bytes index out of range")
	}
	return runtime.NewInt64(int64(value.Value[i])), nil
}
