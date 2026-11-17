package evaluator

import (
	"bufio"
	"bytes"
	"database/sql"
	"geblang/internal/ast"
	"geblang/internal/runtime"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"weak"

	"geblang/internal/cdp"
	"geblang/internal/native"
)

// handleStore holds native handle registries shared by an evaluator and every callback child it spawns.
type handleStore struct {
	nextID               atomic.Int64
	ownersMu             sync.Mutex
	owners               map[int64]*Evaluator
	dbMu                 sync.Mutex
	dbs                  map[int64]*sql.DB
	dbDrivers            map[int64]string
	txs                  map[int64]*dbTxHandle
	stmts                map[int64]*dbStmtHandle
	dbRows               map[int64]weak.Pointer[dbRowsHandle]
	fileMu               sync.Mutex
	files                map[int64]*os.File
	bufReaders           map[int64]*bufio.Reader
	bufferMu             sync.Mutex
	buffers              map[int64]*bytes.Buffer
	streamMu             sync.Mutex
	streams              map[int64]*ioStreamHandle
	processMu            sync.Mutex
	processes            map[int64]*processHandle
	logMu                sync.Mutex
	loggers              map[int64]*loggerHandle
	metricsMu            sync.Mutex
	metrics              map[string]float64
	metricRegistry       map[string]*metricsEntry
	traceMu              sync.Mutex
	traces               map[int64]*traceSpan
	watchMu              sync.Mutex
	watches              map[int64]*watchHandle
	signalMu             sync.Mutex
	signalHandlers       map[string]*signalSubscription
	webMu                sync.Mutex
	webApps              map[int64]*webApp
	wsMu                 sync.Mutex
	websockets           map[int64]*wsHandle
	netMu                sync.Mutex
	netHandles           map[int64]*netHandle
	netServerMu          sync.Mutex
	netServers           map[int64]*netServerHandle
	sshMu                sync.Mutex
	sshClients           map[int64]*sshClientHandle
	sshSessions          map[int64]*sshSessionHandle
	sshTunnels           map[int64]*sshTunnelHandle
	httpServerMu         sync.Mutex
	httpServers          map[int64]*httpServerHandle
	httpStreamMu         sync.Mutex
	httpStreams          map[int64]*httpStreamHandle
	httpClientMu         sync.Mutex
	httpClientHandles    map[int64]*httpClientHandle
	httpCookieJarMu      sync.Mutex
	httpCookieJars       map[int64]http.CookieJar
	httpFetchStreamMu    sync.Mutex
	httpFetchStreams     map[int64]*httpFetchStreamHandle
	httpResponseStreamMu sync.Mutex
	httpResponseStreams  map[int64]*httpResponseStreamHandle
	onnxMu               sync.Mutex
	onnxSessions         map[int64]*native.ONNXSession
	browserMu            sync.Mutex
	browsers             map[int64]*cdp.Browser
	pages                map[int64]*cdp.Page
	browserRoutes        map[string][]browserRoute
	browserSetups        map[*cdp.Browser]pageSetup
	browserContexts      map[int64]*browserContext
	jsonMu               sync.Mutex
	jsonReaders          map[int64]*jsonStreamReader
	xmlMu                sync.Mutex
	xmlReaders           map[int64]*xmlStreamReader
	csvMu                sync.Mutex
	csvReaders           map[int64]*csvStreamReader
	yamlMu               sync.Mutex
	yamlReaders          map[int64]*yamlStreamReader
	extMu                sync.Mutex
	extConns             map[int64]*extHandle
}

func newHandleStore() *handleStore {
	return &handleStore{
		owners:         map[int64]*Evaluator{},
		metrics:        map[string]float64{},
		metricRegistry: map[string]*metricsEntry{},
		webApps:        map[int64]*webApp{},
	}
}

// newDataID allocates an id for state that outlives its creator, such as trace spans.
func (e *Evaluator) newDataID() int64 {
	return e.nextID.Add(1)
}

// newHandleID allocates an id for a resource that the creating evaluator's scope closes in Cleanup.
func (e *Evaluator) newHandleID() int64 {
	return e.newHandleIDFor(e.scope)
}

// newHandleIDFor allocates an id whose handle is closed by owner's Cleanup.
func (e *Evaluator) newHandleIDFor(owner *Evaluator) int64 {
	id := e.nextID.Add(1)
	e.ownersMu.Lock()
	e.owners[id] = owner
	e.ownersMu.Unlock()
	owner.ownedMu.Lock()
	ensureMap(&owner.owned)[id] = struct{}{}
	owner.ownedMu.Unlock()
	return id
}

func (e *Evaluator) releaseHandle(id int64) {
	e.ownersMu.Lock()
	owner := e.owners[id]
	delete(e.owners, id)
	e.ownersMu.Unlock()
	if owner != nil {
		owner.ownedMu.Lock()
		delete(owner.owned, id)
		owner.ownedMu.Unlock()
	}
}

func (e *Evaluator) takeOwnedIDs() []int64 {
	e.ownedMu.Lock()
	ids := make([]int64, 0, len(e.owned))
	for id := range e.owned {
		ids = append(ids, id)
	}
	e.owned = nil
	e.ownedMu.Unlock()
	e.ownersMu.Lock()
	if e.parent == nil {
		e.owners = map[int64]*Evaluator{}
	} else {
		for _, id := range ids {
			delete(e.owners, id)
		}
	}
	e.ownersMu.Unlock()
	return ids
}

// takeHandles removes and returns the entries Cleanup must close; the caller holds the registry's mutex.
func takeHandles[V any](e *Evaluator, m *map[int64]V, ids []int64) []V {
	if e.parent == nil {
		out := make([]V, 0, len(*m))
		for _, v := range *m {
			out = append(out, v)
		}
		*m = nil
		return out
	}
	var out []V
	for _, id := range ids {
		if v, ok := (*m)[id]; ok {
			out = append(out, v)
			delete(*m, id)
		}
	}
	return out
}

// callOwner is the cleanup scope for handles a native opens: the request scope a VM caller attached to the call, else this evaluator's.
func (e *Evaluator) callOwner(call *ast.CallExpression) *Evaluator {
	if call != nil {
		if scope, ok := call.NativeScope.(*Evaluator); ok && scope != nil {
			return scope.scope
		}
	}
	return e.scope
}

// scopedNativeCaller runs natives on the root evaluator's prebuilt table while recording handles against scope.
type scopedNativeCaller struct {
	root  *Evaluator
	scope *Evaluator
}

func (c scopedNativeCaller) CallBuiltin(module, name string, args []runtime.Value, argNames []string) (runtime.Value, error) {
	return c.root.callBuiltinScoped(c.scope, module, name, args, argNames)
}

func (c scopedNativeCaller) NativeObjectMethod(obj runtime.NativeObject, name string, args []runtime.Value) (runtime.Value, error) {
	return c.root.NativeObjectMethod(obj, name, args)
}

func (c scopedNativeCaller) HandleDirectPrint() bool {
	return c.root.HandleDirectPrint()
}

func (e *Evaluator) nativeScopeCaller() scopedNativeCaller {
	root := e
	for root.parent != nil {
		root = root.parent
	}
	return scopedNativeCaller{root: root, scope: e}
}
