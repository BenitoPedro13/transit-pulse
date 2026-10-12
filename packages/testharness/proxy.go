package testharness

import (
	"io"
	"net"
	"net/url"
	"sync"
	"testing"
)

// Proxy is a TCP forwarder with a fixed local address. A test that restarts the TimescaleDB
// container points its client at the proxy, because the container's mapped port can change on
// restart. The target is looked up on every new connection. While the target is down, new
// connections are closed at once, which a client sees as a refused or reset connection.
type Proxy struct {
	ln     net.Listener
	target func() (string, error)

	mu    sync.Mutex
	conns map[net.Conn]struct{}
}

// NewProxy listens on 127.0.0.1 and forwards to whatever target returns ("host:port"). It stops
// when t finishes.
func NewProxy(t testing.TB, target func() (string, error)) *Proxy {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("proxy listen: %v", err)
	}
	p := &Proxy{ln: ln, target: target, conns: map[net.Conn]struct{}{}}
	t.Cleanup(func() {
		_ = ln.Close()
		p.DropConnections()
	})
	go p.serve()
	return p
}

// Addr is "127.0.0.1:<port>".
func (p *Proxy) Addr() string { return p.ln.Addr().String() }

// DropConnections closes every connection in flight.
func (p *Proxy) DropConnections() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for c := range p.conns {
		_ = c.Close()
		delete(p.conns, c)
	}
}

// ConnString returns connString with its host replaced by the proxy's address.
func (p *Proxy) ConnString(t testing.TB, connString string) string {
	t.Helper()
	u, err := url.Parse(connString)
	if err != nil {
		t.Fatalf("parse connection string: %v", err)
	}
	u.Host = p.Addr()
	return u.String()
}

func (p *Proxy) serve() {
	for {
		c, err := p.ln.Accept()
		if err != nil {
			return
		}
		go p.forward(c)
	}
}

func (p *Proxy) forward(client net.Conn) {
	addr, err := p.target()
	if err != nil {
		_ = client.Close()
		return
	}
	upstream, err := net.Dial("tcp", addr)
	if err != nil {
		_ = client.Close()
		return
	}
	p.mu.Lock()
	p.conns[client], p.conns[upstream] = struct{}{}, struct{}{}
	p.mu.Unlock()
	done := make(chan struct{}, 2)
	pipe := func(dst, src net.Conn) {
		_, _ = io.Copy(dst, src)
		_ = dst.Close()
		_ = src.Close()
		done <- struct{}{}
	}
	go pipe(upstream, client)
	go pipe(client, upstream)
	<-done
	<-done
	p.mu.Lock()
	delete(p.conns, client)
	delete(p.conns, upstream)
	p.mu.Unlock()
}
