package core

import (
	"net"
	"sync"
	"time"
)

// RpcPortal is the address easytier-core exposes its management RPC on.
// It is mutable so tests can isolate instances to distinct ports.
var RpcPortal = "127.0.0.1:15888"

var rpcMu sync.RWMutex

// SetRpcPortal overrides the RPC address (used by tests / multi-instance).
func SetRpcPortal(addr string) {
	rpcMu.Lock()
	RpcPortal = addr
	rpcMu.Unlock()
}

// PingRpc attempts a TCP connection to the RPC portal, returning true if reachable.
// On Windows a dial to an unbound port can block, so a short deadline is applied.
func PingRpc(addr string) bool {
	if addr == "" {
		rpcMu.RLock()
		addr = RpcPortal
		rpcMu.RUnlock()
	}
	conn, err := net.DialTimeout("tcp", addr, 400*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
