package bytecode_test

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
)

// fakeSMTPServer accepts connections and records the envelope commands it receives.
type fakeSMTPServer struct {
	ln       net.Listener
	mu       sync.Mutex
	commands []string
}

func startFakeSMTP(t *testing.T) *fakeSMTPServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s := &fakeSMTPServer{ln: ln}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			s.serve(conn)
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return s
}

func (s *fakeSMTPServer) serve(conn net.Conn) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	reply := func(line string) { fmt.Fprintf(conn, "%s\r\n", line) }
	reply("220 fake ESMTP")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		verb := strings.ToUpper(strings.SplitN(line, " ", 2)[0])
		switch verb {
		case "EHLO", "HELO":
			reply("250 fake")
		case "MAIL", "RCPT":
			s.mu.Lock()
			s.commands = append(s.commands, line)
			s.mu.Unlock()
			reply("250 ok")
		case "DATA":
			reply("354 go ahead")
			for {
				body, err := r.ReadString('\n')
				if err != nil || strings.TrimRight(body, "\r\n") == "." {
					break
				}
			}
			reply("250 queued")
		case "QUIT":
			reply("221 bye")
			return
		default:
			reply("250 ok")
		}
	}
}

func (s *fakeSMTPServer) recorded() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string{}, s.commands...)
}

func TestParitySMTPSendUsesBareEnvelopeAddresses(t *testing.T) {
	server := startFakeSMTP(t)
	port := server.ln.Addr().(*net.TCPAddr).Port
	source := fmt.Sprintf(`import io;
import smtp;

let res = smtp.send(
    {"host": "127.0.0.1", "port": %d, "username": "", "password": "", "startTLS": false, "tls": false},
    {"from": "App <noreply@example.com>", "to": ["Ada Lovelace <ada@example.com>"], "cc": ["grace@example.com"],
     "subject": "Hi", "text": "Hello"}
);
io.println(res["ok"]);
io.println(res["recipients"]);
`, port)
	runParityWithStdlib(t, source, "true\n2\n")

	want := []string{"MAIL FROM:<noreply@example.com>", "RCPT TO:<ada@example.com>", "RCPT TO:<grace@example.com>"}
	got := server.recorded()
	if len(got) != 2*len(want) {
		t.Fatalf("recorded %d envelope commands, want %d (one session per backend): %q", len(got), 2*len(want), got)
	}
	for i, cmd := range got {
		if !strings.HasPrefix(cmd, want[i%len(want)]) {
			t.Errorf("command %d = %q, want prefix %q", i, cmd, want[i%len(want)])
		}
	}
}
