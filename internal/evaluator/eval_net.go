package evaluator

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"geblang/internal/ast"
	"geblang/internal/native"
	"geblang/internal/runtime"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

func closeNetHandle(handle *netHandle) error {
	close := func(closer io.Closer) error {
		if err := closer.Close(); err != nil &&
			!errors.Is(err, os.ErrClosed) &&
			!errors.Is(err, net.ErrClosed) &&
			!strings.Contains(err.Error(), "use of closed network connection") {
			return err
		}
		return nil
	}
	if handle.listener != nil {
		return close(handle.listener)
	}
	if handle.conn != nil {
		return close(handle.conn)
	}
	if handle.packet != nil {
		return close(handle.packet)
	}
	return nil
}

func netJoinHostPort(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%s expects exactly two arguments", call.Callee.String())
	}
	host, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s host must be string", call.Callee.String())
	}
	port, ok := args[1].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s port must be string", call.Callee.String())
	}
	return runtime.String{Value: net.JoinHostPort(host.Value, port.Value)}, nil
}

func netSplitHostPort(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	addr, err := singleStringValue(call, args)
	if err != nil {
		return nil, err
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	entries := map[string]runtime.DictEntry{}
	putDict(entries, "host", runtime.String{Value: host})
	putDict(entries, "port", runtime.String{Value: port})
	return runtime.Dict{Entries: entries}, nil
}

func netLookupHost(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	host, err := singleStringValue(call, args)
	if err != nil {
		return nil, err
	}
	addrs, err := net.LookupHost(host)
	if err != nil {
		return nil, err
	}
	values := make([]runtime.Value, 0, len(addrs))
	for _, addr := range addrs {
		values = append(values, runtime.String{Value: addr})
	}
	return &runtime.List{Elements: values}, nil
}

func (e *Evaluator) netListenTCP(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	addr, err := singleStringValue(call, args)
	if err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	return e.registerNetHandle(&netHandle{listener: listener}), nil
}

func (e *Evaluator) netConnectTCP(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	addr, err := singleStringValue(call, args)
	if err != nil {
		return nil, err
	}
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	return e.registerNetHandle(&netHandle{conn: conn}), nil
}

func (e *Evaluator) netAccept(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects listener", call.Callee.String())
	}
	handle, err := e.netHandle(args[0])
	if err != nil {
		return nil, fmt.Errorf("%s %w", call.Callee.String(), err)
	}
	if handle.listener == nil {
		return nil, fmt.Errorf("%s handle is not a listener", call.Callee.String())
	}
	conn, err := handle.listener.Accept()
	if err != nil {
		return nil, err
	}
	return e.registerNetHandle(&netHandle{conn: conn}), nil
}

func (e *Evaluator) netRead(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%s expects connection and byte count", call.Callee.String())
	}
	handle, err := e.netHandle(args[0])
	if err != nil {
		return nil, fmt.Errorf("%s %w", call.Callee.String(), err)
	}
	if handle.conn == nil {
		return nil, fmt.Errorf("%s handle is not a connection", call.Callee.String())
	}
	size, err := int64Argument(call, args[1], "byte count")
	if err != nil {
		return nil, err
	}
	if size < 0 || size > 1<<30 {
		return nil, fmt.Errorf("%s byte count out of range", call.Callee.String())
	}
	buf := make([]byte, size)
	read, err := handle.conn.Read(buf)
	if err != nil && err != io.EOF {
		return nil, err
	}
	return runtime.Bytes{Value: buf[:read]}, nil
}

func (e *Evaluator) netWrite(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%s expects connection and data", call.Callee.String())
	}
	handle, err := e.netHandle(args[0])
	if err != nil {
		return nil, fmt.Errorf("%s %w", call.Callee.String(), err)
	}
	if handle.conn == nil {
		return nil, fmt.Errorf("%s handle is not a connection", call.Callee.String())
	}
	data, err := bytesFromStringOrBytes(call, args[1], "data")
	if err != nil {
		return nil, err
	}
	written, err := handle.conn.Write(data)
	if err != nil {
		return nil, err
	}
	return runtime.NewInt64(int64(written)), nil
}

func (e *Evaluator) netSetDeadline(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%s expects handle and timeout milliseconds", call.Callee.String())
	}
	handle, err := e.netHandle(args[0])
	if err != nil {
		return nil, fmt.Errorf("%s %w", call.Callee.String(), err)
	}
	timeoutMs, err := int64Argument(call, args[1], "timeout milliseconds")
	if err != nil {
		return nil, err
	}
	if timeoutMs < 0 {
		return nil, fmt.Errorf("%s timeout milliseconds must be >= 0", call.Callee.String())
	}
	return runtime.Null{}, setNetHandleDeadline(handle, time.Now().Add(time.Duration(timeoutMs)*time.Millisecond))
}

func (e *Evaluator) netClearDeadline(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects handle", call.Callee.String())
	}
	handle, err := e.netHandle(args[0])
	if err != nil {
		return nil, fmt.Errorf("%s %w", call.Callee.String(), err)
	}
	return runtime.Null{}, setNetHandleDeadline(handle, time.Time{})
}

func (e *Evaluator) netClose(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects handle", call.Callee.String())
	}
	id, err := int64Argument(call, args[0], "handle")
	if err != nil {
		return nil, err
	}
	e.netMu.Lock()
	handle, ok := e.netHandles[id]
	if ok {
		delete(e.netHandles, id)
	}
	e.netMu.Unlock()
	if !ok {
		return nil, fmt.Errorf("unknown net handle %d", id)
	}
	if handle.listener != nil {
		return runtime.Null{}, handle.listener.Close()
	}
	if handle.conn != nil {
		return runtime.Null{}, handle.conn.Close()
	}
	if handle.packet != nil {
		return runtime.Null{}, handle.packet.Close()
	}
	return runtime.Null{}, nil
}

func (e *Evaluator) netLocalAddr(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects handle", call.Callee.String())
	}
	handle, err := e.netHandle(args[0])
	if err != nil {
		return nil, fmt.Errorf("%s %w", call.Callee.String(), err)
	}
	switch {
	case handle.listener != nil:
		return runtime.String{Value: handle.listener.Addr().String()}, nil
	case handle.conn != nil:
		return runtime.String{Value: handle.conn.LocalAddr().String()}, nil
	case handle.packet != nil:
		return runtime.String{Value: handle.packet.LocalAddr().String()}, nil
	default:
		return runtime.String{Value: ""}, nil
	}
}

func (e *Evaluator) netRemoteAddr(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects connection", call.Callee.String())
	}
	handle, err := e.netHandle(args[0])
	if err != nil {
		return nil, fmt.Errorf("%s %w", call.Callee.String(), err)
	}
	if handle.conn == nil {
		return nil, fmt.Errorf("%s handle is not a connection", call.Callee.String())
	}
	return runtime.String{Value: handle.conn.RemoteAddr().String()}, nil
}

func (e *Evaluator) netListenUDP(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	addr, err := singleStringValue(call, args)
	if err != nil {
		return nil, err
	}
	packet, err := net.ListenPacket("udp", addr)
	if err != nil {
		return nil, err
	}
	return e.registerNetHandle(&netHandle{packet: packet}), nil
}

func (e *Evaluator) netDialUDP(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	addr, err := singleStringValue(call, args)
	if err != nil {
		return nil, err
	}
	conn, err := net.Dial("udp", addr)
	if err != nil {
		return nil, err
	}
	return e.registerNetHandle(&netHandle{conn: conn}), nil
}

func (e *Evaluator) netReadFrom(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%s expects packet socket and byte count", call.Callee.String())
	}
	handle, err := e.netHandle(args[0])
	if err != nil {
		return nil, fmt.Errorf("%s %w", call.Callee.String(), err)
	}
	if handle.packet == nil {
		return nil, fmt.Errorf("%s handle is not a packet socket", call.Callee.String())
	}
	size, err := int64Argument(call, args[1], "byte count")
	if err != nil {
		return nil, err
	}
	if size < 0 || size > 1<<30 {
		return nil, fmt.Errorf("%s byte count out of range", call.Callee.String())
	}
	buf := make([]byte, size)
	read, addr, err := handle.packet.ReadFrom(buf)
	if err != nil {
		return nil, err
	}
	entries := map[string]runtime.DictEntry{}
	putDict(entries, "addr", runtime.String{Value: addr.String()})
	putDict(entries, "data", runtime.Bytes{Value: buf[:read]})
	return runtime.Dict{Entries: entries}, nil
}

func (e *Evaluator) netWriteTo(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 3 {
		return nil, fmt.Errorf("%s expects packet socket, address, and data", call.Callee.String())
	}
	handle, err := e.netHandle(args[0])
	if err != nil {
		return nil, fmt.Errorf("%s %w", call.Callee.String(), err)
	}
	if handle.packet == nil {
		return nil, fmt.Errorf("%s handle is not a packet socket", call.Callee.String())
	}
	addr, ok := args[1].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s address must be string", call.Callee.String())
	}
	udpAddr, err := net.ResolveUDPAddr("udp", addr.Value)
	if err != nil {
		return nil, err
	}
	data, err := bytesFromStringOrBytes(call, args[2], "data")
	if err != nil {
		return nil, err
	}
	written, err := handle.packet.WriteTo(data, udpAddr)
	if err != nil {
		return nil, err
	}
	return runtime.NewInt64(int64(written)), nil
}

func (e *Evaluator) registerNetHandle(handle *netHandle) runtime.Value {
	e.netMu.Lock()
	defer e.netMu.Unlock()
	e.nextNetID++
	ensureMap(&e.netHandles)[e.nextNetID] = handle
	return runtime.NewInt64(e.nextNetID)
}

func (e *Evaluator) netHandle(value runtime.Value) (*netHandle, error) {
	id, err := rawInt64(value, "handle")
	if err != nil {
		return nil, err
	}
	e.netMu.Lock()
	handle, ok := e.netHandles[id]
	e.netMu.Unlock()
	if !ok && e.parent != nil {
		return e.parent.netHandle(value)
	}
	if !ok {
		return nil, fmt.Errorf("unknown net handle %d", id)
	}
	return handle, nil
}

// netDial opens a TCP (or TLS) connection and returns a dict with
// the connection handle, an IOStream wrapping the conn, and local /
// remote address strings. Mirrors the F4 proc.spawn shape so the
// stdlib `net.Socket` class can hand back ready-made IOStream values
// for read / write / readLine / lines / close.
//
// Args: (host: string, port: int, opts: dict<string, any> = {})
// Recognised opts: "tls" (bool) for TLS dial, "timeoutMs" (int) for
// connect timeout.
func (e *Evaluator) netDial(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 || len(args) > 3 {
		return nil, fmt.Errorf("%s expects (host, port, opts?)", call.Callee.String())
	}
	host, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s host must be string", call.Callee.String())
	}
	port, ok := native.AsInt64(args[1])
	if !ok {
		return nil, fmt.Errorf("%s port must be int", call.Callee.String())
	}
	useTLS := false
	var timeoutMs int64
	if len(args) == 3 {
		opts, ok := args[2].(runtime.Dict)
		if !ok {
			return nil, fmt.Errorf("%s options must be a dict", call.Callee.String())
		}
		if v, found := dictField(opts, "tls"); found {
			b, ok := v.(runtime.Bool)
			if !ok {
				return nil, fmt.Errorf("%s options.tls must be bool", call.Callee.String())
			}
			useTLS = b.Value
		}
		if v, found := dictField(opts, "timeoutMs"); found {
			n, ok := native.AsInt64(v)
			if !ok {
				return nil, fmt.Errorf("%s options.timeoutMs must be int", call.Callee.String())
			}
			timeoutMs = n
		}
	}
	addr := net.JoinHostPort(host.Value, strconv.FormatInt(port, 10))
	var conn net.Conn
	var err error
	dialer := &net.Dialer{}
	if timeoutMs > 0 {
		dialer.Timeout = time.Duration(timeoutMs) * time.Millisecond
	}
	if useTLS {
		tlsDialer := &tls.Dialer{NetDialer: dialer, Config: &tls.Config{ServerName: host.Value}}
		conn, err = tlsDialer.Dial("tcp", addr)
	} else {
		conn, err = dialer.Dial("tcp", addr)
	}
	if err != nil {
		return nil, err
	}
	netHandleValue := e.registerNetHandle(&netHandle{conn: conn})
	streamHandle := &ioStreamHandle{name: "net socket", reader: conn, writer: conn, closer: conn}
	streamValue := e.registerIOStream(streamHandle)
	entries := map[string]runtime.DictEntry{}
	putDict(entries, "handle", netHandleValue)
	putDict(entries, "stream", streamValue)
	putDict(entries, "localAddr", runtime.String{Value: conn.LocalAddr().String()})
	putDict(entries, "remoteAddr", runtime.String{Value: conn.RemoteAddr().String()})
	return runtime.Dict{Entries: entries}, nil
}

// netServe binds a listener, spawns an accept-loop goroutine, and
// dispatches each accepted connection to the user's handler callback
// as a Socket-shaped dict. The handler runs in a child evaluator so
// captured module-level state is observable; the wrap-bridge route
// is used on the VM side. Returns a server handle for shutdown.
func (e *Evaluator) netServe(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) < 3 || len(args) > 4 {
		return nil, fmt.Errorf("%s expects (host, port, handler, opts?)", call.Callee.String())
	}
	host, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s host must be string", call.Callee.String())
	}
	port, ok := native.AsInt64(args[1])
	if !ok {
		return nil, fmt.Errorf("%s port must be int", call.Callee.String())
	}
	handler, ok := args[2].(runtime.Function)
	if !ok {
		return nil, fmt.Errorf("%s handler must be a function", call.Callee.String())
	}
	pool, err := serverPoolFromArgs(args, 3, call.Callee.String())
	if err != nil {
		return nil, err
	}
	var optsArg runtime.Value = runtime.Null{}
	if len(args) == 4 {
		optsArg = args[3]
	}
	share, err := parseShareHandler(optsArg, call.Callee.String())
	if err != nil {
		return nil, err
	}
	addr := net.JoinHostPort(host.Value, strconv.FormatInt(port, 10))
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	server := &netServerHandle{listener: listener, pool: pool}
	e.netServerMu.Lock()
	e.nextNetServerID++
	id := e.nextNetServerID
	ensureMap(&e.netServers)[id] = server
	e.netServerMu.Unlock()
	server.wg.Add(1)
	go func() {
		defer server.wg.Done()
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			handlerConn := conn
			if pool != nil && !pool.IsUnbounded() {
				if err := pool.Acquire(context.Background()); err != nil {
					_ = handlerConn.Close()
					continue
				}
			}
			server.wg.Add(1)
			go func() {
				defer server.wg.Done()
				if pool != nil && !pool.IsUnbounded() {
					defer pool.Release()
				}
				child := e.childForCallback()
				defer child.Cleanup()
				defer child.startDebugThread("net connection")()
				connHandle := e.registerNetHandle(&netHandle{conn: handlerConn})
				streamHandle := &ioStreamHandle{name: "net socket", reader: handlerConn, writer: handlerConn, closer: handlerConn}
				streamValue := e.registerIOStream(streamHandle)
				entries := map[string]runtime.DictEntry{}
				putDict(entries, "handle", connHandle)
				putDict(entries, "stream", streamValue)
				putDict(entries, "localAddr", runtime.String{Value: handlerConn.LocalAddr().String()})
				putDict(entries, "remoteAddr", runtime.String{Value: handlerConn.RemoteAddr().String()})
				socketDict := runtime.Dict{Entries: entries}
				// Per-connection isolation unless shareHandler: clone the handler so concurrent connections don't share/race its captured state (parity with the bytecode VM).
				callbackHandler := handler
				if handler.Native == nil && !share {
					callbackHandler = runtime.CloneFunction(handler)
				}
				_, _ = child.applyFunction(callbackHandler, []runtime.Value{socketDict})
			}()
		}
	}()
	entries := map[string]runtime.DictEntry{}
	putDict(entries, "handle", runtime.NewInt64(id))
	putDict(entries, "localAddr", runtime.String{Value: listener.Addr().String()})
	return runtime.Dict{Entries: entries}, nil
}

// netCloseListener stops an accept-loop and joins the goroutine so
// callers can rely on reads of callback-touched state to happen
// after the last handler invocation.
func (e *Evaluator) netCloseListener(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects (server handle)", call.Callee.String())
	}
	id, ok := native.AsInt64(args[0])
	if !ok {
		return nil, fmt.Errorf("%s server handle must be int", call.Callee.String())
	}
	e.netServerMu.Lock()
	server, ok := e.netServers[id]
	if ok {
		delete(e.netServers, id)
	}
	e.netServerMu.Unlock()
	if !ok {
		if e.parent != nil {
			return e.parent.netCloseListener(call, args)
		}
		return runtime.Null{}, nil
	}
	return runtime.Null{}, stopNetServerHandle(server)
}

func stopNetServerHandle(server *netServerHandle) error {
	if server == nil || server.stopped {
		return nil
	}
	server.stopped = true
	err := server.listener.Close()
	server.wg.Wait()
	if err != nil && !errors.Is(err, net.ErrClosed) {
		return err
	}
	return nil
}

func setNetHandleDeadline(handle *netHandle, deadline time.Time) error {
	type deadliner interface {
		SetDeadline(time.Time) error
	}
	switch {
	case handle.listener != nil:
		deadlineListener, ok := handle.listener.(deadliner)
		if !ok {
			return fmt.Errorf("listener does not support deadlines")
		}
		return deadlineListener.SetDeadline(deadline)
	case handle.conn != nil:
		return handle.conn.SetDeadline(deadline)
	case handle.packet != nil:
		return handle.packet.SetDeadline(deadline)
	default:
		return nil
	}
}
