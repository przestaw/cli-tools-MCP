// Optional SSH transport for HTTP profiles behind a bastion (`"backend": "ssh-tunnel"`).
//
// Every TCP connection of the HTTP client is one `ssh -W host:port DESTINATION` child process: ssh forwards its
// stdin/stdout to the service, so no local port is opened and the process ends with the connection. TLS still runs
// end to end and is verified against the real hostname of the profile URL. The SSH key (ssh-agent, ~/.ssh/config)
// authenticates the tunnel; the profile token authenticates the request. workctl never passes a secret to ssh.

package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	"workctl/internal/proc"
)

// sshIdleTimeout closes idle tunnelled connections, and so their ssh processes, soon after the last request.
const sshIdleTimeout = 30 * time.Second

// useSSHTunnel makes the transport dial every connection through ssh. A proxy from the environment is dropped:
// the bastion is the proxy.
func useSSHTunnel(tr *http.Transport, destination string) {
	tr.Proxy = nil
	tr.IdleConnTimeout = sshIdleTimeout
	tr.DialContext = func(ctx context.Context, _, addr string) (net.Conn, error) {
		return dialSSH(ctx, destination, addr)
	}
}

// dialSSH starts `ssh -W addr destination` and returns its stdin/stdout as a connection.
// BatchMode keeps ssh from prompting for a password or host key: there is no terminal under MCP.
func dialSSH(ctx context.Context, destination, addr string) (net.Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// The process must outlive the dial context: keep-alive reuses the connection for later requests.
	cmd := proc.Command(context.Background(), "ssh", "-o", "BatchMode=yes", "-o", "LogLevel=ERROR", "-W", addr, "--", destination)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	c := &sshConn{in: stdin, out: stdout, cmd: cmd, destination: destination}
	cmd.Stderr = &c.stderr
	if err := cmd.Start(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, fmt.Errorf("ssh not found in PATH: install the OpenSSH client")
		}
		return nil, fmt.Errorf("start ssh: %w", err)
	}
	return c, nil
}

// sshConn is a net.Conn over the stdio of one ssh child process. Deadlines are not supported; the HTTP client's
// timeout closes the connection instead.
type sshConn struct {
	in          io.WriteCloser
	out         io.Reader
	cmd         *exec.Cmd
	destination string
	stderr      bytes.Buffer // read only after wait: until then ssh may still write to it
	waitOnce    sync.Once
}

// wait reaps the ssh process once; after it returns, stderr is complete.
func (c *sshConn) wait() { c.waitOnce.Do(func() { _ = c.cmd.Wait() }) }

// Read returns data from the service; when ssh failed, the error carries what ssh printed (for example
// "Permission denied (publickey)") instead of a bare EOF.
func (c *sshConn) Read(p []byte) (int, error) {
	n, err := c.out.Read(p)
	if err != nil {
		c.wait()
		if msg := strings.TrimSpace(c.stderr.String()); msg != "" {
			err = fmt.Errorf("ssh %s: %s", c.destination, msg)
		}
	}
	return n, err
}

// Write sends data to the service.
func (c *sshConn) Write(p []byte) (int, error) { return c.in.Write(p) }

// Close ends the ssh process. Killing is safe: it only forwards bytes, and the service may keep its side open.
func (c *sshConn) Close() error {
	_ = c.in.Close()
	_ = c.cmd.Process.Kill()
	c.wait()
	return nil
}

// LocalAddr is a placeholder: the connection has no local socket.
func (c *sshConn) LocalAddr() net.Addr { return sshAddr("ssh") }

// RemoteAddr names the SSH destination.
func (c *sshConn) RemoteAddr() net.Addr { return sshAddr(c.destination) }

// SetDeadline is a no-op (see sshConn).
func (c *sshConn) SetDeadline(time.Time) error { return nil }

// SetReadDeadline is a no-op (see sshConn).
func (c *sshConn) SetReadDeadline(time.Time) error { return nil }

// SetWriteDeadline is a no-op (see sshConn).
func (c *sshConn) SetWriteDeadline(time.Time) error { return nil }

// sshAddr is the net.Addr of an ssh connection.
type sshAddr string

// Network implements net.Addr.
func (a sshAddr) Network() string { return "ssh" }

// String implements net.Addr.
func (a sshAddr) String() string { return string(a) }
