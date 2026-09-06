package core

import (
	"fmt"
	"math/rand"
	"net"
	"time"
)

// FreePort returns a likely-available TCP port on 127.0.0.1, drawn from a wide
// random range to reduce collision between concurrently running test binaries.
func FreePort() (int, error) {
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	for i := 0; i < 50; i++ {
		port := 30000 + rng.Intn(20000)
		addr := fmt.Sprintf("127.0.0.1:%d", port)
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			continue
		}
		_ = ln.Close()
		return port, nil
	}
	// Fallback: ask the OS for a free port.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port, nil
}

// WithTestPort temporarily binds the RPC portal to a free port and restores
// the previous value when the returned func is called.
func WithTestPort() (string, func(), error) {
	port, err := FreePort()
	if err != nil {
		return "", nil, err
	}
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	old := RpcPortal
	SetRpcPortal(addr)
	return addr, func() { SetRpcPortal(old) }, nil
}
