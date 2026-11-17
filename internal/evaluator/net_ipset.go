package evaluator

import (
	"fmt"
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"

	"geblang/internal/ast"
	"geblang/internal/native"
	"geblang/internal/runtime"
)

const ipSetKind = "NetIpSet"

// ipSet is a concurrent set of addresses and CIDR prefixes with optional per-entry expiry.
type ipSet struct {
	mu       sync.RWMutex
	prefixes map[netip.Prefix]time.Time
	lengths  map[int]int
	adds     int
}

func newIPSet() *ipSet {
	return &ipSet{prefixes: map[netip.Prefix]time.Time{}, lengths: map[int]int{}}
}

func parseIPSetEntry(s string) (netip.Prefix, error) {
	if strings.Contains(s, "/") {
		p, err := netip.ParsePrefix(s)
		if err != nil {
			return netip.Prefix{}, err
		}
		if p.Addr().Is4In6() && p.Bits() >= 96 {
			p = netip.PrefixFrom(p.Addr().Unmap(), p.Bits()-96)
		}
		return p.Masked(), nil
	}
	addr, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Prefix{}, err
	}
	addr = addr.Unmap()
	return netip.PrefixFrom(addr, addr.BitLen()), nil
}

func (s *ipSet) add(p netip.Prefix, ttl time.Duration) {
	var expires time.Time
	if ttl > 0 {
		expires = time.Now().Add(ttl)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.prefixes[p]; !ok {
		s.lengths[lengthKey(p)]++
	}
	s.prefixes[p] = expires
	s.adds++
	if s.adds%1024 == 0 {
		s.sweepLocked(time.Now())
	}
}

func (s *ipSet) remove(p netip.Prefix) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.deleteLocked(p)
}

func (s *ipSet) deleteLocked(p netip.Prefix) bool {
	if _, ok := s.prefixes[p]; !ok {
		return false
	}
	delete(s.prefixes, p)
	key := lengthKey(p)
	if s.lengths[key]--; s.lengths[key] == 0 {
		delete(s.lengths, key)
	}
	return true
}

func (s *ipSet) sweepLocked(now time.Time) {
	for p, expires := range s.prefixes {
		if !expires.IsZero() && !now.Before(expires) {
			s.deleteLocked(p)
		}
	}
}

// lengthKey separates IPv4 and IPv6 prefix lengths so a lookup only masks at lengths that exist for its family.
func lengthKey(p netip.Prefix) int {
	if p.Addr().Is4() {
		return p.Bits()
	}
	return 1000 + p.Bits()
}

func (s *ipSet) contains(addr netip.Addr) bool {
	addr = addr.Unmap()
	now := time.Now()
	s.mu.RLock()
	defer s.mu.RUnlock()
	base := 0
	if !addr.Is4() {
		base = 1000
	}
	for key := range s.lengths {
		bits := key - base
		if bits < 0 || bits > addr.BitLen() {
			continue
		}
		p, err := addr.Prefix(bits)
		if err != nil {
			continue
		}
		if expires, ok := s.prefixes[p]; ok && (expires.IsZero() || now.Before(expires)) {
			return true
		}
	}
	return false
}

func (s *ipSet) containsHost(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	addr, err := netip.ParseAddr(host)
	return err == nil && s.contains(addr)
}

func (s *ipSet) size() int {
	now := time.Now()
	s.mu.RLock()
	defer s.mu.RUnlock()
	n := 0
	for _, expires := range s.prefixes {
		if expires.IsZero() || now.Before(expires) {
			n++
		}
	}
	return n
}

func (s *ipSet) clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prefixes = map[netip.Prefix]time.Time{}
	s.lengths = map[int]int{}
}

var ipSetClass = buildIPSetClass()

func buildIPSetClass() *runtime.Class {
	class := &runtime.Class{Name: "IpSet", Module: "net", Fields: []runtime.Field{{Name: "__handle"}}, Methods: map[string][]runtime.Function{}}
	method := func(name string, fn func(set *ipSet, args []runtime.Value) (runtime.Value, error)) {
		class.Methods[strings.ToLower(name)] = []runtime.Function{{Name: name, Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
			set, ok := ipSetFromValue(this)
			if !ok {
				return nil, fmt.Errorf("net.IpSet.%s: invalid set", name)
			}
			return fn(set, args)
		}}}
	}
	entryArg := func(name string, args []runtime.Value, i int) (netip.Prefix, error) {
		s, ok := args[i].(runtime.String)
		if !ok {
			return netip.Prefix{}, fmt.Errorf("net.IpSet.%s expects an IP or CIDR string", name)
		}
		p, err := parseIPSetEntry(s.Value)
		if err != nil {
			return netip.Prefix{}, runtime.ClassifiedError{Class: "ValueError", Message: fmt.Sprintf("net.IpSet.%s: invalid IP or CIDR %q", name, s.Value)}
		}
		return p, nil
	}
	method("add", func(set *ipSet, args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 && len(args) != 2 {
			return nil, fmt.Errorf("net.IpSet.add expects an IP or CIDR and optional ttlSeconds")
		}
		p, err := entryArg("add", args, 0)
		if err != nil {
			return nil, err
		}
		var ttl time.Duration
		if len(args) == 2 {
			n, ok := native.AsInt64(args[1])
			if !ok || n < 0 {
				return nil, fmt.Errorf("net.IpSet.add ttlSeconds must be a non-negative int")
			}
			ttl = time.Duration(n) * time.Second
		}
		set.add(p, ttl)
		return runtime.Null{}, nil
	})
	method("remove", func(set *ipSet, args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("net.IpSet.remove expects an IP or CIDR")
		}
		p, err := entryArg("remove", args, 0)
		if err != nil {
			return nil, err
		}
		return runtime.Bool{Value: set.remove(p)}, nil
	})
	method("contains", func(set *ipSet, args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("net.IpSet.contains expects an IP")
		}
		s, ok := args[0].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("net.IpSet.contains expects an IP string")
		}
		addr, err := netip.ParseAddr(s.Value)
		return runtime.Bool{Value: err == nil && set.contains(addr)}, nil
	})
	method("size", func(set *ipSet, args []runtime.Value) (runtime.Value, error) {
		return runtime.NewInt64(int64(set.size())), nil
	})
	method("clear", func(set *ipSet, args []runtime.Value) (runtime.Value, error) {
		set.clear()
		return runtime.Null{}, nil
	})
	return class
}

func ipSetFromValue(value runtime.Value) (*ipSet, bool) {
	instance, ok := value.(*runtime.Instance)
	if !ok {
		return nil, false
	}
	handle, ok := instance.Fields["__handle"].(runtime.NativeObject)
	if !ok || handle.Kind != ipSetKind {
		return nil, false
	}
	set, ok := handle.Payload.(*ipSet)
	return set, ok
}

func netIPSet(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) > 1 {
		return nil, fmt.Errorf("%s expects an optional list of IPs or CIDRs", call.Callee.String())
	}
	set := newIPSet()
	if len(args) == 1 {
		list, ok := args[0].(*runtime.List)
		if !ok {
			return nil, fmt.Errorf("%s expects a list of IPs or CIDRs", call.Callee.String())
		}
		for _, element := range list.Elements {
			s, ok := element.(runtime.String)
			if !ok {
				return nil, fmt.Errorf("%s entries must be strings", call.Callee.String())
			}
			p, err := parseIPSetEntry(s.Value)
			if err != nil {
				return nil, runtime.ClassifiedError{Class: "ValueError", Message: fmt.Sprintf("%s: invalid IP or CIDR %q", call.Callee.String(), s.Value)}
			}
			set.add(p, 0)
		}
	}
	return &runtime.Instance{Class: ipSetClass, Fields: map[string]runtime.Value{"__handle": runtime.NativeObject{Kind: ipSetKind, Payload: set}}}, nil
}

// parseIPBlocklist reads opts.ipBlocklist for http.serve/listen and net.serve.
func parseIPBlocklist(opts runtime.Value, label string) (*ipSet, error) {
	dict, ok := opts.(runtime.Dict)
	if !ok {
		return nil, nil
	}
	v, ok := dictField(dict, "ipBlocklist")
	if !ok {
		return nil, nil
	}
	if _, isNull := v.(runtime.Null); isNull {
		return nil, nil
	}
	set, ok := ipSetFromValue(v)
	if !ok {
		return nil, fmt.Errorf("%s opts.ipBlocklist must be a net.IpSet", label)
	}
	return set, nil
}

// blocklistListener closes connections from blocked peers before TLS or HTTP parsing starts.
type blocklistListener struct {
	net.Listener
	set *ipSet
}

func (l *blocklistListener) Accept() (net.Conn, error) {
	for {
		conn, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		if l.set.containsHost(conn.RemoteAddr().String()) {
			_ = conn.Close()
			continue
		}
		return conn, nil
	}
}

func withIPBlocklist(listener net.Listener, set *ipSet) net.Listener {
	if set == nil {
		return listener
	}
	return &blocklistListener{Listener: listener, set: set}
}
