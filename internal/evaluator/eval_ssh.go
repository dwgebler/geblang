package evaluator

import (
	"bytes"
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

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

// sshAuthFromOpts builds the ssh auth method list from the dict.
// Recognised keys: "password", "privateKey" (string PEM),
// "privateKeyFile" (path), "passphrase" (decrypts the key),
// "agent" (use SSH_AUTH_SOCK).
func sshAuthFromOpts(opts runtime.Dict) ([]ssh.AuthMethod, error) {
	var methods []ssh.AuthMethod
	if v, ok := dictField(opts, "password"); ok {
		s, ok := v.(runtime.String)
		if !ok {
			return nil, fmt.Errorf("ssh: options.password must be string")
		}
		methods = append(methods, ssh.Password(s.Value))
	}
	loadKey := func(pem []byte) error {
		var signer ssh.Signer
		var err error
		if v, ok := dictField(opts, "passphrase"); ok {
			pass, ok := v.(runtime.String)
			if !ok {
				return fmt.Errorf("ssh: options.passphrase must be string")
			}
			signer, err = ssh.ParsePrivateKeyWithPassphrase(pem, []byte(pass.Value))
		} else {
			signer, err = ssh.ParsePrivateKey(pem)
		}
		if err != nil {
			return err
		}
		methods = append(methods, ssh.PublicKeys(signer))
		return nil
	}
	if v, ok := dictField(opts, "privateKey"); ok {
		s, ok := v.(runtime.String)
		if !ok {
			return nil, fmt.Errorf("ssh: options.privateKey must be string")
		}
		if err := loadKey([]byte(s.Value)); err != nil {
			return nil, err
		}
	}
	if v, ok := dictField(opts, "privateKeyFile"); ok {
		s, ok := v.(runtime.String)
		if !ok {
			return nil, fmt.Errorf("ssh: options.privateKeyFile must be string")
		}
		pem, err := os.ReadFile(s.Value)
		if err != nil {
			return nil, fmt.Errorf("ssh: read private key: %w", err)
		}
		if err := loadKey(pem); err != nil {
			return nil, err
		}
	}
	if v, ok := dictField(opts, "agent"); ok {
		b, ok := v.(runtime.Bool)
		if !ok {
			return nil, fmt.Errorf("ssh: options.agent must be bool")
		}
		if b.Value {
			sock := os.Getenv("SSH_AUTH_SOCK")
			if sock == "" {
				return nil, fmt.Errorf("ssh: agent requested but SSH_AUTH_SOCK is empty")
			}
			conn, err := net.Dial("unix", sock)
			if err != nil {
				return nil, fmt.Errorf("ssh: dial agent: %w", err)
			}
			methods = append(methods, ssh.PublicKeysCallback(agent.NewClient(conn).Signers))
		}
	}
	if len(methods) == 0 {
		return nil, fmt.Errorf("ssh: no authentication method supplied (set password / privateKey / privateKeyFile / agent)")
	}
	return methods, nil
}

func sshHostKeyCallbackFromOpts(opts runtime.Dict) (ssh.HostKeyCallback, error) {
	if v, ok := dictField(opts, "insecureSkipHostKey"); ok {
		b, ok := v.(runtime.Bool)
		if !ok {
			return nil, fmt.Errorf("ssh: options.insecureSkipHostKey must be bool")
		}
		if b.Value {
			return ssh.InsecureIgnoreHostKey(), nil
		}
	}
	if v, ok := dictField(opts, "knownHostsFile"); ok {
		s, ok := v.(runtime.String)
		if !ok {
			return nil, fmt.Errorf("ssh: options.knownHostsFile must be string")
		}
		return knownhosts.New(s.Value)
	}
	// Default: read $HOME/.ssh/known_hosts if available, otherwise
	// fall back to InsecureIgnoreHostKey is wrong - require explicit
	// opt-in. Fail with a clear error.
	return nil, fmt.Errorf("ssh: host key verification not configured (set options.knownHostsFile or options.insecureSkipHostKey: true)")
}

// sshConnect dials an SSH server. target is "user@host" or just
// "host" (login from current user). Returns a dict with handle and
// remoteAddr.
func (e *Evaluator) sshConnect(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 || len(args) > 2 {
		return nil, fmt.Errorf("%s expects (target, options?)", call.Callee.String())
	}
	target, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s target must be string", call.Callee.String())
	}
	var opts runtime.Dict
	if len(args) == 2 {
		opts, ok = args[1].(runtime.Dict)
		if !ok {
			return nil, fmt.Errorf("%s options must be a dict", call.Callee.String())
		}
	} else {
		opts = runtime.DictFromEntries(map[string]runtime.DictEntry{}, nil)
	}
	user := os.Getenv("USER")
	host := target.Value
	if at := strings.Index(target.Value, "@"); at >= 0 {
		user = target.Value[:at]
		host = target.Value[at+1:]
	}
	port := int64(22)
	if v, ok := dictField(opts, "port"); ok {
		n, ok := native.AsInt64(v)
		if !ok {
			return nil, fmt.Errorf("%s options.port must be int", call.Callee.String())
		}
		port = n
	}
	authMethods, err := sshAuthFromOpts(opts)
	if err != nil {
		return nil, err
	}
	hostKeyCb, err := sshHostKeyCallbackFromOpts(opts)
	if err != nil {
		return nil, err
	}
	timeout := time.Duration(0)
	if v, ok := dictField(opts, "timeoutMs"); ok {
		n, ok := native.AsInt64(v)
		if !ok {
			return nil, fmt.Errorf("%s options.timeoutMs must be int", call.Callee.String())
		}
		timeout = time.Duration(n) * time.Millisecond
	}
	cfg := &ssh.ClientConfig{
		User:            user,
		Auth:            authMethods,
		HostKeyCallback: hostKeyCb,
		Timeout:         timeout,
	}
	addr := net.JoinHostPort(host, strconv.FormatInt(port, 10))
	client, err := ssh.Dial("tcp", addr, cfg)
	if err != nil {
		return nil, err
	}
	handle := &sshClientHandle{client: client}
	e.sshMu.Lock()
	e.nextSSHID++
	id := e.nextSSHID
	ensureMap(&e.sshClients)[id] = handle
	e.sshMu.Unlock()
	entries := map[string]runtime.DictEntry{}
	putDict(entries, "handle", runtime.NewInt64(id))
	putDict(entries, "user", runtime.String{Value: user})
	putDict(entries, "remoteAddr", runtime.String{Value: addr})
	return runtime.DictFromEntries(entries, nil), nil
}

func (e *Evaluator) sshClient(value runtime.Value) (*sshClientHandle, error) {
	id, ok := native.AsInt64(value)
	if !ok {
		return nil, fmt.Errorf("ssh handle must be int")
	}
	e.sshMu.Lock()
	handle, ok := e.sshClients[id]
	e.sshMu.Unlock()
	if !ok && e.parent != nil {
		return e.parent.sshClient(value)
	}
	if !ok {
		return nil, fmt.Errorf("unknown ssh handle %d", id)
	}
	if handle.closed {
		return nil, fmt.Errorf("ssh handle %d is closed", id)
	}
	return handle, nil
}

func (e *Evaluator) sshExec(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%s expects (handle, command)", call.Callee.String())
	}
	handle, err := e.sshClient(args[0])
	if err != nil {
		return nil, err
	}
	cmd, ok := args[1].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s command must be string", call.Callee.String())
	}
	session, err := handle.client.NewSession()
	if err != nil {
		return nil, err
	}
	defer session.Close()
	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr
	exitCode := int64(0)
	if err := session.Run(cmd.Value); err != nil {
		if ee, ok := err.(*ssh.ExitError); ok {
			exitCode = int64(ee.ExitStatus())
		} else {
			return nil, err
		}
	}
	entries := map[string]runtime.DictEntry{}
	putDict(entries, "stdout", runtime.String{Value: stdout.String()})
	putDict(entries, "stderr", runtime.String{Value: stderr.String()})
	putDict(entries, "exitCode", runtime.NewInt64(exitCode))
	return runtime.DictFromEntries(entries, nil), nil
}

func (e *Evaluator) sshSpawn(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 || len(args) > 3 {
		return nil, fmt.Errorf("%s expects (handle, command, options?)", call.Callee.String())
	}
	handle, err := e.sshClient(args[0])
	if err != nil {
		return nil, err
	}
	cmd, ok := args[1].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s command must be string", call.Callee.String())
	}
	session, err := handle.client.NewSession()
	if err != nil {
		return nil, err
	}
	stdin, err := session.StdinPipe()
	if err != nil {
		_ = session.Close()
		return nil, err
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		_ = session.Close()
		return nil, err
	}
	stderr, err := session.StderrPipe()
	if err != nil {
		_ = session.Close()
		return nil, err
	}
	if err := session.Start(cmd.Value); err != nil {
		_ = session.Close()
		return nil, err
	}
	sessionHandle := &sshSessionHandle{session: session, stdin: stdin, stdout: stdout, stderr: stderr}
	e.sshMu.Lock()
	e.nextSSHID++
	sid := e.nextSSHID
	ensureMap(&e.sshSessions)[sid] = sessionHandle
	e.sshMu.Unlock()
	stdinStream := e.registerIOStream(&ioStreamHandle{name: "ssh stdin", writer: stdin, closer: stdin})
	stdoutStream := e.registerIOStream(&ioStreamHandle{name: "ssh stdout", reader: stdout})
	stderrStream := e.registerIOStream(&ioStreamHandle{name: "ssh stderr", reader: stderr})
	entries := map[string]runtime.DictEntry{}
	putDict(entries, "handle", runtime.NewInt64(sid))
	putDict(entries, "stdin", stdinStream)
	putDict(entries, "stdout", stdoutStream)
	putDict(entries, "stderr", stderrStream)
	return runtime.DictFromEntries(entries, nil), nil
}

func (e *Evaluator) sshSession(value runtime.Value) (*sshSessionHandle, error) {
	id, ok := native.AsInt64(value)
	if !ok {
		return nil, fmt.Errorf("ssh session handle must be int")
	}
	e.sshMu.Lock()
	handle, ok := e.sshSessions[id]
	e.sshMu.Unlock()
	if !ok && e.parent != nil {
		return e.parent.sshSession(value)
	}
	if !ok {
		return nil, fmt.Errorf("unknown ssh session handle %d", id)
	}
	return handle, nil
}

func (e *Evaluator) sshSessionWait(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects (session handle)", call.Callee.String())
	}
	handle, err := e.sshSession(args[0])
	if err != nil {
		return nil, err
	}
	exitCode := int64(0)
	if err := handle.session.Wait(); err != nil {
		if ee, ok := err.(*ssh.ExitError); ok {
			exitCode = int64(ee.ExitStatus())
		} else {
			return nil, err
		}
	}
	return runtime.NewInt64(exitCode), nil
}

func (e *Evaluator) sshSessionKill(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 || len(args) > 2 {
		return nil, fmt.Errorf("%s expects (session handle, signal?)", call.Callee.String())
	}
	handle, err := e.sshSession(args[0])
	if err != nil {
		return nil, err
	}
	sig := ssh.SIGKILL
	if len(args) == 2 {
		s, ok := args[1].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("%s signal must be string", call.Callee.String())
		}
		sig = ssh.Signal(strings.TrimPrefix(s.Value, "SIG"))
	}
	return runtime.Null{}, handle.session.Signal(sig)
}

// sshSftp returns the cached sftp.Client for a handle, creating it
// on first use.
func sshSftp(handle *sshClientHandle) (*sftp.Client, error) {
	handle.sftpMu.Lock()
	defer handle.sftpMu.Unlock()
	if handle.sftpCli != nil {
		return handle.sftpCli, nil
	}
	cli, err := sftp.NewClient(handle.client)
	if err != nil {
		return nil, err
	}
	handle.sftpCli = cli
	return cli, nil
}

func (e *Evaluator) sshUpload(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 3 {
		return nil, fmt.Errorf("%s expects (handle, localPath, remotePath)", call.Callee.String())
	}
	handle, err := e.sshClient(args[0])
	if err != nil {
		return nil, err
	}
	localPath, ok := args[1].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s localPath must be string", call.Callee.String())
	}
	remotePath, ok := args[2].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s remotePath must be string", call.Callee.String())
	}
	cli, err := sshSftp(handle)
	if err != nil {
		return nil, err
	}
	src, err := os.Open(localPath.Value)
	if err != nil {
		return nil, err
	}
	defer src.Close()
	dst, err := cli.Create(remotePath.Value)
	if err != nil {
		return nil, err
	}
	defer dst.Close()
	written, err := io.Copy(dst, src)
	if err != nil {
		return nil, err
	}
	return runtime.NewInt64(written), nil
}

func (e *Evaluator) sshDownload(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 3 {
		return nil, fmt.Errorf("%s expects (handle, remotePath, localPath)", call.Callee.String())
	}
	handle, err := e.sshClient(args[0])
	if err != nil {
		return nil, err
	}
	remotePath, ok := args[1].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s remotePath must be string", call.Callee.String())
	}
	localPath, ok := args[2].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s localPath must be string", call.Callee.String())
	}
	cli, err := sshSftp(handle)
	if err != nil {
		return nil, err
	}
	src, err := cli.Open(remotePath.Value)
	if err != nil {
		return nil, err
	}
	defer src.Close()
	dst, err := os.Create(localPath.Value)
	if err != nil {
		return nil, err
	}
	defer dst.Close()
	written, err := io.Copy(dst, src)
	if err != nil {
		return nil, err
	}
	return runtime.NewInt64(written), nil
}

func (e *Evaluator) sshSftpList(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%s expects (handle, remotePath)", call.Callee.String())
	}
	handle, err := e.sshClient(args[0])
	if err != nil {
		return nil, err
	}
	remotePath, ok := args[1].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s remotePath must be string", call.Callee.String())
	}
	cli, err := sshSftp(handle)
	if err != nil {
		return nil, err
	}
	infos, err := cli.ReadDir(remotePath.Value)
	if err != nil {
		return nil, err
	}
	out := make([]runtime.Value, 0, len(infos))
	for _, info := range infos {
		entries := map[string]runtime.DictEntry{}
		putDict(entries, "name", runtime.String{Value: info.Name()})
		putDict(entries, "size", runtime.NewInt64(info.Size()))
		putDict(entries, "mode", runtime.NewInt64(int64(info.Mode().Perm())))
		putDict(entries, "isDir", runtime.Bool{Value: info.IsDir()})
		putDict(entries, "modUnix", runtime.NewInt64(info.ModTime().Unix()))
		out = append(out, runtime.DictFromEntries(entries, nil))
	}
	return &runtime.List{Elements: out}, nil
}

func (e *Evaluator) sshSftpRemove(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%s expects (handle, remotePath)", call.Callee.String())
	}
	handle, err := e.sshClient(args[0])
	if err != nil {
		return nil, err
	}
	remotePath, ok := args[1].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s remotePath must be string", call.Callee.String())
	}
	cli, err := sshSftp(handle)
	if err != nil {
		return nil, err
	}
	return runtime.Null{}, cli.Remove(remotePath.Value)
}

func (e *Evaluator) sshSftpMkdir(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 || len(args) > 3 {
		return nil, fmt.Errorf("%s expects (handle, remotePath, mode?)", call.Callee.String())
	}
	handle, err := e.sshClient(args[0])
	if err != nil {
		return nil, err
	}
	remotePath, ok := args[1].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s remotePath must be string", call.Callee.String())
	}
	cli, err := sshSftp(handle)
	if err != nil {
		return nil, err
	}
	if err := cli.MkdirAll(remotePath.Value); err != nil {
		return nil, err
	}
	if len(args) == 3 {
		mode, ok := native.AsInt64(args[2])
		if !ok {
			return nil, fmt.Errorf("%s mode must be int", call.Callee.String())
		}
		if err := cli.Chmod(remotePath.Value, os.FileMode(mode)); err != nil {
			return nil, err
		}
	}
	return runtime.Null{}, nil
}

func (e *Evaluator) sshSftpOpen(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 || len(args) > 3 {
		return nil, fmt.Errorf("%s expects (handle, remotePath, mode?)", call.Callee.String())
	}
	handle, err := e.sshClient(args[0])
	if err != nil {
		return nil, err
	}
	remotePath, ok := args[1].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s remotePath must be string", call.Callee.String())
	}
	mode := "r"
	if len(args) == 3 {
		s, ok := args[2].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("%s mode must be string", call.Callee.String())
		}
		mode = s.Value
	}
	cli, err := sshSftp(handle)
	if err != nil {
		return nil, err
	}
	var f *sftp.File
	switch mode {
	case "r":
		f, err = cli.Open(remotePath.Value)
	case "w":
		f, err = cli.Create(remotePath.Value)
	case "a":
		f, err = cli.OpenFile(remotePath.Value, os.O_WRONLY|os.O_APPEND|os.O_CREATE)
	default:
		return nil, fmt.Errorf("%s mode must be \"r\", \"w\", or \"a\"", call.Callee.String())
	}
	if err != nil {
		return nil, err
	}
	return e.registerIOStream(&ioStreamHandle{name: "ssh sftp file", reader: f, writer: f, closer: f}), nil
}

// sshForwardLocal binds localPort on 127.0.0.1 and forwards each
// accepted connection through the SSH server to remoteTarget
// ("host:port"). The returned tunnel handle can be used with
// tunnelClose to stop the accept-loop.
func (e *Evaluator) sshForwardLocal(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 3 {
		return nil, fmt.Errorf("%s expects (handle, localPort, remoteTarget)", call.Callee.String())
	}
	handle, err := e.sshClient(args[0])
	if err != nil {
		return nil, err
	}
	localPort, ok := native.AsInt64(args[1])
	if !ok {
		return nil, fmt.Errorf("%s localPort must be int", call.Callee.String())
	}
	remote, ok := args[2].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s remoteTarget must be string", call.Callee.String())
	}
	listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.FormatInt(localPort, 10)))
	if err != nil {
		return nil, err
	}
	tunnel := &sshTunnelHandle{listener: listener}
	e.sshMu.Lock()
	e.nextSSHID++
	tid := e.nextSSHID
	ensureMap(&e.sshTunnels)[tid] = tunnel
	e.sshMu.Unlock()
	tunnel.wg.Add(1)
	go func() {
		defer tunnel.wg.Done()
		for {
			local, err := listener.Accept()
			if err != nil {
				return
			}
			go func(local net.Conn) {
				defer local.Close()
				remoteConn, err := handle.client.Dial("tcp", remote.Value)
				if err != nil {
					return
				}
				defer remoteConn.Close()
				done := make(chan struct{}, 2)
				go func() { _, _ = io.Copy(remoteConn, local); done <- struct{}{} }()
				go func() { _, _ = io.Copy(local, remoteConn); done <- struct{}{} }()
				<-done
			}(local)
		}
	}()
	entries := map[string]runtime.DictEntry{}
	putDict(entries, "handle", runtime.NewInt64(tid))
	putDict(entries, "localAddr", runtime.String{Value: listener.Addr().String()})
	return runtime.DictFromEntries(entries, nil), nil
}

func (e *Evaluator) sshForwardRemote(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 3 {
		return nil, fmt.Errorf("%s expects (handle, remotePort, localTarget)", call.Callee.String())
	}
	handle, err := e.sshClient(args[0])
	if err != nil {
		return nil, err
	}
	remotePort, ok := native.AsInt64(args[1])
	if !ok {
		return nil, fmt.Errorf("%s remotePort must be int", call.Callee.String())
	}
	local, ok := args[2].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s localTarget must be string", call.Callee.String())
	}
	listener, err := handle.client.Listen("tcp", net.JoinHostPort("0.0.0.0", strconv.FormatInt(remotePort, 10)))
	if err != nil {
		return nil, err
	}
	tunnel := &sshTunnelHandle{listener: listener}
	e.sshMu.Lock()
	e.nextSSHID++
	tid := e.nextSSHID
	ensureMap(&e.sshTunnels)[tid] = tunnel
	e.sshMu.Unlock()
	tunnel.wg.Add(1)
	go func() {
		defer tunnel.wg.Done()
		for {
			remoteConn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(remote net.Conn) {
				defer remote.Close()
				localConn, err := net.Dial("tcp", local.Value)
				if err != nil {
					return
				}
				defer localConn.Close()
				done := make(chan struct{}, 2)
				go func() { _, _ = io.Copy(localConn, remote); done <- struct{}{} }()
				go func() { _, _ = io.Copy(remote, localConn); done <- struct{}{} }()
				<-done
			}(remoteConn)
		}
	}()
	entries := map[string]runtime.DictEntry{}
	putDict(entries, "handle", runtime.NewInt64(tid))
	putDict(entries, "remoteAddr", runtime.String{Value: listener.Addr().String()})
	return runtime.DictFromEntries(entries, nil), nil
}

func (e *Evaluator) sshTunnelClose(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects (tunnel handle)", call.Callee.String())
	}
	id, ok := native.AsInt64(args[0])
	if !ok {
		return nil, fmt.Errorf("%s tunnel handle must be int", call.Callee.String())
	}
	e.sshMu.Lock()
	tunnel, ok := e.sshTunnels[id]
	if ok {
		delete(e.sshTunnels, id)
	}
	e.sshMu.Unlock()
	if !ok {
		if e.parent != nil {
			return e.parent.sshTunnelClose(call, args)
		}
		return runtime.Null{}, nil
	}
	return runtime.Null{}, stopSSHTunnelHandle(tunnel)
}

func stopSSHTunnelHandle(tunnel *sshTunnelHandle) error {
	if tunnel == nil || tunnel.stopped {
		return nil
	}
	tunnel.stopped = true
	err := tunnel.listener.Close()
	tunnel.wg.Wait()
	if err != nil &&
		!errors.Is(err, net.ErrClosed) &&
		!strings.Contains(err.Error(), "use of closed network connection") {
		return err
	}
	return nil
}

func (e *Evaluator) sshClose(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects (handle)", call.Callee.String())
	}
	id, ok := native.AsInt64(args[0])
	if !ok {
		return nil, fmt.Errorf("%s handle must be int", call.Callee.String())
	}
	e.sshMu.Lock()
	handle, ok := e.sshClients[id]
	if ok {
		delete(e.sshClients, id)
	}
	e.sshMu.Unlock()
	if !ok {
		if e.parent != nil {
			return e.parent.sshClose(call, args)
		}
		return runtime.Null{}, nil
	}
	return runtime.Null{}, closeSSHClientHandle(handle)
}

func closeSSHClientHandle(handle *sshClientHandle) error {
	if handle == nil || handle.closed {
		return nil
	}
	handle.closed = true
	if handle.sftpCli != nil {
		_ = handle.sftpCli.Close()
	}
	if err := handle.client.Close(); err != nil &&
		!errors.Is(err, net.ErrClosed) &&
		!strings.Contains(err.Error(), "use of closed network connection") {
		return err
	}
	return nil
}
