package evaluator

import (
	"fmt"
	"geblang/internal/ast"
	"geblang/internal/runtime"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type syslogFraming int

const (
	framingDatagram syslogFraming = iota // udp / unixgram: one message per write
	framingOctet                         // tcp: RFC 6587 octet-counting
	framingLF                            // local unix stream: trailing newline
)

var syslogFacilities = map[string]int{
	"kern": 0, "user": 1, "mail": 2, "daemon": 3, "auth": 4, "syslog": 5,
	"lpr": 6, "news": 7, "uucp": 8, "cron": 9, "authpriv": 10, "ftp": 11,
	"local0": 16, "local1": 17, "local2": 18, "local3": 19,
	"local4": 20, "local5": 21, "local6": 22, "local7": 23,
}

// syslogSeverity maps the engine's four levels onto RFC 5424 severities.
func syslogSeverity(level string) int {
	switch level {
	case "error":
		return 3
	case "warn":
		return 4
	case "debug":
		return 7
	default:
		return 6
	}
}

type syslogSink struct {
	mu       sync.Mutex
	conn     net.Conn
	facility int
	app      string
	host     string
	pid      int
	framing  syslogFraming
}

// message builds the RFC 5424 SYSLOG-MSG: <PRI>1 TIMESTAMP HOST APP PID - - MSG.
func (s *syslogSink) message(level, line string) string {
	pri := s.facility*8 + syslogSeverity(level)
	ts := time.Now().Format(time.RFC3339Nano)
	return fmt.Sprintf("<%d>1 %s %s %s %d - - %s", pri, ts, s.host, s.app, s.pid, line)
}

// framed applies the transport framing the chosen network requires.
func (s *syslogSink) framed(level, line string) string {
	msg := s.message(level, line)
	switch s.framing {
	case framingOctet:
		return fmt.Sprintf("%d %s", len(msg), msg)
	case framingLF:
		return msg + "\n"
	default:
		return msg
	}
}

// WriteLevel sends line; send failures are swallowed (best-effort).
func (s *syslogSink) WriteLevel(level, line string) error {
	payload := s.framed(level, line)
	s.mu.Lock()
	defer s.mu.Unlock()
	_, _ = s.conn.Write([]byte(payload))
	return nil
}

func (s *syslogSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conn.Close()
}

func (e *Evaluator) logSyslog(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects a single options dict", call.Callee.String())
	}
	opts, ok := args[0].(runtime.Dict)
	if !ok {
		return nil, fmt.Errorf("%s options must be a dict", call.Callee.String())
	}
	network, err := syslogOpt(call, opts, "network", "udp")
	if err != nil {
		return nil, err
	}
	address, err := syslogOpt(call, opts, "address", "")
	if err != nil {
		return nil, err
	}
	facilityName, err := syslogOpt(call, opts, "facility", "user")
	if err != nil {
		return nil, err
	}
	facility, ok := syslogFacilities[facilityName]
	if !ok {
		return nil, fmt.Errorf("%s unknown syslog facility %q", call.Callee.String(), facilityName)
	}
	host, err := syslogOpt(call, opts, "hostname", defaultSyslogHost())
	if err != nil {
		return nil, err
	}
	app, err := syslogOpt(call, opts, "app", defaultSyslogApp())
	if err != nil {
		return nil, err
	}

	var conn net.Conn
	framing := framingDatagram
	switch network {
	case "udp", "tcp":
		if address == "" {
			return nil, fmt.Errorf("%s %s requires an address (host:port)", call.Callee.String(), network)
		}
		conn, err = net.Dial(network, address)
		if network == "tcp" {
			framing = framingOctet
		}
	case "local":
		conn, framing, err = dialLocalSyslog()
	default:
		return nil, fmt.Errorf("%s unknown syslog network %q (use udp, tcp, or local)", call.Callee.String(), network)
	}
	if err != nil {
		return nil, fmt.Errorf("%s connect failed: %w", call.Callee.String(), err)
	}

	sink := &syslogSink{
		conn:     conn,
		facility: facility,
		app:      syslogField(app),
		host:     syslogField(host),
		pid:      os.Getpid(),
		framing:  framing,
	}
	return e.registerLogger(e.callOwner(call), &loggerHandle{target: "syslog:" + network, leveled: sink, closer: sink}), nil
}

func syslogOpt(call *ast.CallExpression, opts runtime.Dict, key, def string) (string, error) {
	v, ok := dictField(opts, key)
	if !ok {
		return def, nil
	}
	s, ok := v.(runtime.String)
	if !ok {
		return "", fmt.Errorf("%s option %q must be a string", call.Callee.String(), key)
	}
	return s.Value, nil
}

// syslogField yields a single RFC 5424 token, or "-" (NILVALUE) when empty.
func syslogField(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "-"
	}
	return strings.ReplaceAll(s, " ", "_")
}

func defaultSyslogHost() string {
	if h, err := os.Hostname(); err == nil && h != "" {
		return h
	}
	return "-"
}

func defaultSyslogApp() string {
	if exe, err := os.Executable(); err == nil {
		return filepath.Base(exe)
	}
	return "-"
}
