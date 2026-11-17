package evaluator

import (
	"net"
	"net/netip"
	"testing"
	"time"
)

func TestIPSetMatchesAddressesPrefixesAndExpiry(t *testing.T) {
	s := newIPSet()
	for _, entry := range []string{"10.0.0.0/8", "192.0.2.7", "2001:db8::/32", "::ffff:198.51.100.0/120"} {
		p, err := parseIPSetEntry(entry)
		if err != nil {
			t.Fatal(err)
		}
		s.add(p, 0)
	}
	cases := map[string]bool{"10.200.0.1": true, "11.0.0.1": false, "192.0.2.7": true, "192.0.2.8": false, "2001:db8:ffff::1": true, "2001:db9::1": false, "::ffff:10.1.1.1": true, "198.51.100.9": true}
	for ip, want := range cases {
		if got := s.contains(netip.MustParseAddr(ip)); got != want {
			t.Errorf("contains(%s) = %v, want %v", ip, got, want)
		}
	}
	p, _ := parseIPSetEntry("203.0.113.0/24")
	s.add(p, 20*time.Millisecond)
	if !s.contains(netip.MustParseAddr("203.0.113.5")) {
		t.Fatal("ttl entry missing before expiry")
	}
	time.Sleep(40 * time.Millisecond)
	if s.contains(netip.MustParseAddr("203.0.113.5")) || s.size() != 4 {
		t.Fatalf("expired entry still matched or counted, size %d", s.size())
	}
}

func TestBlocklistListenerClosesBlockedPeers(t *testing.T) {
	inner, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := newIPSet()
	p, _ := parseIPSetEntry("127.0.0.1")
	s.add(p, 0)
	listener := withIPBlocklist(inner, s)
	defer listener.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		if c, err := listener.Accept(); err == nil {
			accepted <- c
		}
	}()
	client, err := net.Dial("tcp", inner.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	_ = client.SetReadDeadline(time.Now().Add(time.Second))
	if n, err := client.Read(make([]byte, 1)); n != 0 || err == nil {
		t.Fatalf("blocked peer read %d bytes, err %v; want a closed connection", n, err)
	}
	select {
	case <-accepted:
		t.Fatal("blocked connection was handed to the server")
	default:
	}
}
