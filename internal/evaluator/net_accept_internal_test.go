package evaluator

import (
	"net"
	"syscall"
	"testing"
	"time"
)

type flakyListener struct {
	net.Listener
	failures int
	conns    []net.Conn
}

func (l *flakyListener) Accept() (net.Conn, error) {
	if l.failures > 0 {
		l.failures--
		return nil, &net.OpError{Op: "accept", Net: "tcp", Err: syscall.EMFILE}
	}
	if len(l.conns) > 0 {
		c := l.conns[0]
		l.conns = l.conns[1:]
		return c, nil
	}
	return nil, net.ErrClosed
}

func TestAcceptLoopSurvivesTemporaryErrors(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	listener := &flakyListener{failures: 3, conns: []net.Conn{a}}
	var got []net.Conn
	done := make(chan struct{})
	go func() {
		acceptLoop(listener, func(c net.Conn) { got = append(got, c) })
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("accept loop did not stop on a closed listener")
	}
	if len(got) != 1 || got[0] != a {
		t.Fatalf("accepted %d connections after transient errors, want 1", len(got))
	}
}

func TestAcceptBackoffCapsAtOneSecond(t *testing.T) {
	var d time.Duration
	seen := []time.Duration{}
	for i := 0; i < 12; i++ {
		d = acceptBackoff(d)
		seen = append(seen, d)
	}
	if seen[0] != 5*time.Millisecond || seen[1] != 10*time.Millisecond || seen[len(seen)-1] != time.Second {
		t.Fatalf("backoff sequence %v", seen)
	}
}
