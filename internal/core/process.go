package core

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// errInvalidPID is returned by killByPID for non-positive PIDs.
var errInvalidPID = errors.New("invalid pid")

// Process manages the easytier-core subprocess lifecycle.
type Process struct {
	mu       sync.Mutex
	cmd      *exec.Cmd
	running  bool
	stopping bool // set when Stop() is called; suppresses the exit-error state
	started  time.Time

	onExit   func(error)
	onLog    func(string)
	onStatus func(bool)

	// argsOverride, when set, replaces the default shared config-dir launch
	// arguments (used by the per-instance Group).
	argsOverride []string
}

func NewProcess() *Process {
	return &Process{}
}

func (p *Process) SetExitCallback(fn func(error))  { p.onExit = fn }
func (p *Process) SetLogCallback(fn func(string))  { p.onLog = fn }
func (p *Process) SetStatusCallback(fn func(bool)) { p.onStatus = fn }

// Running reports whether the core process is alive.
func (p *Process) Running() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.running
}

// SetArgs overrides the launch arguments (per-instance group mode).
func (p *Process) SetArgs(args []string) { p.argsOverride = args }

// effectiveArgs returns the override when present, else the shared default.
func (p *Process) effectiveArgs() []string {
	if p.argsOverride != nil {
		return p.argsOverride
	}
	return p.Args()
}

// Args returns the command line used to launch the core.
func (p *Process) Args() []string {
	return []string{
		"--config-dir", DefaultPaths.ConfigDir(),
		"--rpc-portal", RpcPortal,
		"--file-log-dir", DefaultPaths.LogDir(),
		"--file-log-level", "info",
	}
}

// Start spawns easytier-core as a child process.
func (p *Process) Start() error {
	p.mu.Lock()
	if p.running {
		p.mu.Unlock()
		return fmt.Errorf("core process already running")
	}

	corePath := DefaultPaths.CorePath()
	if _, err := os.Stat(corePath); err != nil {
		p.mu.Unlock()
		return fmt.Errorf("easytier-core not found: %w", err)
	}

	workdir := DefaultPaths.ensureCoreWorkDir()
	ensureDrivers(workdir)

	cmd := exec.Command(corePath, p.effectiveArgs()...)
	cmd.Dir = workdir
	hideWindow(cmd) // Windows: do not show a console window for the child

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		p.mu.Unlock()
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		p.mu.Unlock()
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		p.mu.Unlock()
		return fmt.Errorf("failed to start easytier-core: %w", err)
	}

	p.cmd = cmd
	p.running = true
	p.stopping = false
	p.started = time.Now()
	p.mu.Unlock() // release before invoking callbacks to avoid lock ordering issues

	go p.scanLogs(stdout)
	go p.scanLogs(stderr)

	go func() {
		err := cmd.Wait()
		p.mu.Lock()
		p.running = false
		p.cmd = nil
		intentional := p.stopping
		p.mu.Unlock()
		if p.onStatus != nil {
			p.onStatus(false)
		}
		if p.onExit != nil {
			if intentional {
				err = nil // an intentional stop is not an error
			}
			p.onExit(err)
		}
	}()

	// Only report "running" if the process has not already exited (avoids a
	// race where a core that dies instantly flips the state back first).
	p.mu.Lock()
	stillRunning := p.running
	p.mu.Unlock()
	if stillRunning && p.onStatus != nil {
		p.onStatus(true)
	}
	return nil
}

// Stop terminates the core process. Managed children and cores started outside
// this GUI (service mode / manual) are both handled; the RPC portal port is
// used to locate an external core. A graceful close is attempted first.
func (p *Process) Stop() error {
	p.mu.Lock()
	cmd := p.cmd
	running := p.running
	p.stopping = true
	p.mu.Unlock()

	if running && cmd != nil && cmd.Process != nil {
		if pid := cmd.Process.Pid; pid > 0 {
			if err := killByPID(pid); err != nil {
				return err
			}
		} else if err := cmd.Process.Kill(); err != nil && !strings.Contains(err.Error(), "process finished") {
			return err
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for {
			if !p.Running() {
				return nil
			}
			select {
			case <-ctx.Done():
				return fmt.Errorf("core process did not stop in time")
			case <-time.After(50 * time.Millisecond):
			}
		}
	}

	// Core not spawned by us: kill the process owning the RPC portal.
	if PingRpc(RpcPortal) {
		pid := pidListeningOn(RpcPortal)
		if pid > 0 {
			if err := killByPID(pid); err != nil {
				return fmt.Errorf("failed to stop external core (pid %d): %w", pid, err)
			}
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				if !PingRpc(RpcPortal) {
					break
				}
				time.Sleep(100 * time.Millisecond)
			}
		}
	}
	return nil
}

// Restart stops and starts the core, waiting for the RPC port to be released
// before spawning the new process to avoid bind conflicts.
func (p *Process) Restart() error {
	if err := p.Stop(); err != nil {
		return err
	}
	// Wait until the RPC portal is no longer reachable (old process fully gone).
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if !PingRpc(RpcPortal) {
			break
		}
		time.Sleep(150 * time.Millisecond)
	}
	return p.Start()
}

// Uptime returns how long the core has been running.
func (p *Process) Uptime() time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.running {
		return 0
	}
	return time.Since(p.started)
}

func (p *Process) scanLogs(r io.Reader) {
	br := bufio.NewReader(r)
	for {
		line, err := br.ReadString('\n')
		if len(line) > 0 && p.onLog != nil {
			p.onLog(strings.TrimRight(line, "\r\n"))
		}
		if err != nil {
			return
		}
	}
}

// ensureDrivers copies bundled TUN drivers into the core workdir.
func ensureDrivers(workdir string) {
	src := DefaultPaths.DriverDir()
	for _, name := range []string{"wintun.dll", "WinDivert64.sys", "Packet.dll"} {
		s := filepath.Join(src, name)
		if _, err := os.Stat(s); err != nil {
			continue
		}
		d := filepath.Join(workdir, name)
		if _, err := os.Stat(d); err == nil {
			continue
		}
		data, err := os.ReadFile(s)
		if err != nil {
			continue
		}
		_ = os.WriteFile(d, data, 0o755)
	}
}
