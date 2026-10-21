package native

import (
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"

	"geblang/internal/runtime"
)

type ChannelHandle struct {
	ch     chan runtime.Value
	closed atomic.Bool
	cap    int
	// closeMu serialises the close + send race: send takes RLock,
	// close takes Lock. The atomic.Bool gate gives the fast path
	// without contention on the read side.
	closeMu sync.RWMutex
}

// channelNextID numbers channel handles. Each handle carries its
// *ChannelHandle as the payload, so the channel is freed with the handle.
var channelNextID atomic.Int64

func registerAsyncChannel(r *Registry) {
	r.Register("async.channel", "make", func(args []runtime.Value) (runtime.Value, error) {
		buf, err := channelBufSize(args)
		if err != nil {
			return nil, err
		}
		h := &ChannelHandle{ch: make(chan runtime.Value, buf), cap: buf}
		return runtime.NativeObject{Kind: "AsyncChannel", ID: channelNextID.Add(1), Payload: h}, nil
	})
	r.Register("async.channel", "send", func(args []runtime.Value) (runtime.Value, error) {
		h, v, err := channelSendArgs(args, "async.channel.send")
		if err != nil {
			return nil, err
		}
		return runtime.Null{}, sendChannel(h, v)
	})
	r.Register("async.channel", "recv", func(args []runtime.Value) (runtime.Value, error) {
		h, err := channelHandleFrom(args, "async.channel.recv")
		if err != nil {
			return nil, err
		}
		return recvChannel(h), nil
	})
	r.Register("async.channel", "tryRecv", func(args []runtime.Value) (runtime.Value, error) {
		h, err := channelHandleFrom(args, "async.channel.tryRecv")
		if err != nil {
			return nil, err
		}
		v, ok := tryRecvChannel(h)
		if !ok {
			return runtime.Null{}, nil
		}
		return v, nil
	})
	r.Register("async.channel", "trySend", func(args []runtime.Value) (runtime.Value, error) {
		h, v, err := channelSendArgs(args, "async.channel.trySend")
		if err != nil {
			return nil, err
		}
		sent, sendErr := trySendChannel(h, v)
		if sendErr != nil {
			return nil, sendErr
		}
		return runtime.Bool{Value: sent}, nil
	})
	r.Register("async.channel", "close", func(args []runtime.Value) (runtime.Value, error) {
		h, err := channelHandleFrom(args, "async.channel.close")
		if err != nil {
			return nil, err
		}
		return runtime.Null{}, closeChannel(h)
	})
	r.Register("async.channel", "isClosed", func(args []runtime.Value) (runtime.Value, error) {
		h, err := channelHandleFrom(args, "async.channel.isClosed")
		if err != nil {
			return nil, err
		}
		return runtime.Bool{Value: h.closed.Load()}, nil
	})
}

func channelBufSize(args []runtime.Value) (int, error) {
	if len(args) != 1 {
		return 0, fmt.Errorf("async.channel.make expects (buffer)")
	}
	n, ok := AsInt64(args[0])
	if !ok {
		return 0, fmt.Errorf("async.channel.make buffer must be int")
	}
	if n < 0 {
		return 0, fmt.Errorf("async.channel.make buffer must be >= 0")
	}
	return int(n), nil
}

func channelHandleFrom(args []runtime.Value, label string) (*ChannelHandle, error) {
	obj, err := singleHandleObject(args, "AsyncChannel", label)
	if err != nil {
		return nil, err
	}
	h, ok := obj.Payload.(*ChannelHandle)
	if !ok {
		return nil, fmt.Errorf("%s: unknown handle", label)
	}
	return h, nil
}

func channelSendArgs(args []runtime.Value, label string) (*ChannelHandle, runtime.Value, error) {
	if len(args) != 2 {
		return nil, nil, fmt.Errorf("%s expects (handle, value)", label)
	}
	obj, ok := args[0].(runtime.NativeObject)
	if !ok || obj.Kind != "AsyncChannel" {
		return nil, nil, fmt.Errorf("%s: argument is not a Channel handle", label)
	}
	h, ok := obj.Payload.(*ChannelHandle)
	if !ok {
		return nil, nil, fmt.Errorf("%s: unknown handle", label)
	}
	return h, args[1], nil
}

func sendChannel(h *ChannelHandle, v runtime.Value) error {
	h.closeMu.RLock()
	defer h.closeMu.RUnlock()
	if h.closed.Load() {
		return fmt.Errorf("send on closed channel")
	}
	h.ch <- v
	return nil
}

func recvChannel(h *ChannelHandle) runtime.Value {
	v, ok := <-h.ch
	if !ok {
		return runtime.Null{}
	}
	return v
}

func tryRecvChannel(h *ChannelHandle) (runtime.Value, bool) {
	select {
	case v, ok := <-h.ch:
		if !ok {
			return runtime.Null{}, true
		}
		return v, true
	default:
		return nil, false
	}
}

func trySendChannel(h *ChannelHandle, v runtime.Value) (bool, error) {
	h.closeMu.RLock()
	defer h.closeMu.RUnlock()
	if h.closed.Load() {
		return false, fmt.Errorf("send on closed channel")
	}
	select {
	case h.ch <- v:
		return true, nil
	default:
		return false, nil
	}
}

func closeChannel(h *ChannelHandle) error {
	h.closeMu.Lock()
	defer h.closeMu.Unlock()
	if !h.closed.CompareAndSwap(false, true) {
		return fmt.Errorf("close on already-closed channel")
	}
	close(h.ch)
	return nil
}

// SelectChannels runs reflect.Select over the given cases. Each case
// names a channel handle and either "recv" or "send" (with the value
// to send). When hasDefault is true a default case is appended; the
// returned index is -1 when default fires. For a recv case the
// returned value is the received value (or null on closed-drained).
// For a send case the returned value is always null.
func SelectChannels(handles []*ChannelHandle, kinds []string, sendValues []runtime.Value, hasDefault bool) (chosen int, value runtime.Value, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("send on closed channel")
		}
	}()
	cases := make([]reflect.SelectCase, 0, len(handles)+1)
	for i, h := range handles {
		rc := reflect.SelectCase{Chan: reflect.ValueOf(h.ch)}
		switch kinds[i] {
		case "recv":
			rc.Dir = reflect.SelectRecv
		case "send":
			rc.Dir = reflect.SelectSend
			rc.Send = reflect.ValueOf(sendValues[i])
		default:
			return 0, nil, fmt.Errorf("unknown select case kind %q", kinds[i])
		}
		cases = append(cases, rc)
	}
	if hasDefault {
		cases = append(cases, reflect.SelectCase{Dir: reflect.SelectDefault})
	}
	idx, recvVal, recvOk := reflect.Select(cases)
	if hasDefault && idx == len(handles) {
		return -1, runtime.Null{}, nil
	}
	if kinds[idx] == "recv" {
		if !recvOk {
			return idx, runtime.Null{}, nil
		}
		v, ok := recvVal.Interface().(runtime.Value)
		if !ok {
			return idx, runtime.Null{}, nil
		}
		return idx, v, nil
	}
	return idx, runtime.Null{}, nil
}

// ChannelHandleFromValue extracts the underlying *ChannelHandle from a
// NativeObject of kind "AsyncChannel". Used by the evaluator and the
// VM when dispatching a SelectStatement.
func ChannelHandleFromValue(v runtime.Value) (*ChannelHandle, bool) {
	obj, ok := v.(runtime.NativeObject)
	if !ok || obj.Kind != "AsyncChannel" {
		return nil, false
	}
	h, ok := obj.Payload.(*ChannelHandle)
	return h, ok
}
