package core

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

// Paths resolves the location of bundled easytier binaries and app data dirs.
type Paths struct {
	once sync.Once
	mu   sync.RWMutex

	execDir   string
	binDir    string
	drvDir    string
	appData   string
	configDir string
	logDir    string

	// coreDirOverride points the core/cli binaries at a downloaded EasyTier
	// release (coremgr version dir) instead of the bundled ones. Empty =
	// bundled. Set via SetCoreOverride.
	coreDirOverride string

	// overridden indicates init() should not clobber pre-set dirs.
	overridden bool
}

var DefaultPaths = &Paths{}

// SetOverride replaces paths with explicit non-empty values (used in tests and
// for embedded-resource extraction). Empty values keep the current/default.
func (p *Paths) SetOverride(bin, drv, appData, configDir, logDir string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if bin != "" {
		p.binDir = bin
	}
	if drv != "" {
		p.drvDir = drv
	}
	if appData != "" {
		p.appData = appData
	}
	if configDir != "" {
		p.configDir = configDir
	}
	if logDir != "" {
		p.logDir = logDir
	}
	p.overridden = true
}

func (p *Paths) init() {
	p.mu.Lock()
	defer p.mu.Unlock()
	// Preserve explicit overrides (tests / bundling).
	if p.overridden {
		_ = os.MkdirAll(p.appData, 0o755)
		_ = os.MkdirAll(p.configDir, 0o755)
		_ = os.MkdirAll(p.logDir, 0o755)
		return
	}

	exe, err := os.Executable()
	if err != nil {
		exe = ""
	}
	p.execDir = filepath.Dir(exe)

	// When running in dev (wails dev) the executable lives in a temp build dir,
	// so fall back to the project resources dir. On non-Windows the bundled
	// binary has no .exe suffix.
	coreName := "easytier-core.exe"
	if runtime.GOOS != "windows" {
		coreName = "easytier-core"
	}
	binFromExec := filepath.Join(p.execDir, "resources", "bin")
	if _, err := os.Stat(filepath.Join(binFromExec, coreName)); err == nil {
		p.binDir = binFromExec
	} else if _, err := os.Stat(filepath.Join(p.execDir, "bin")); err == nil {
		p.binDir = filepath.Join(p.execDir, "bin")
	} else {
		p.binDir = p.execDir
	}

	drvFromExec := filepath.Join(p.execDir, "resources", "drivers")
	if _, err := os.Stat(filepath.Join(drvFromExec, "wintun.dll")); err == nil {
		p.drvDir = drvFromExec
	} else {
		p.drvDir = p.execDir
	}

	var base string
	switch runtime.GOOS {
	case "windows":
		base = os.Getenv("APPDATA")
		if base == "" {
			home, _ := os.UserHomeDir()
			base = filepath.Join(home, "AppData", "Roaming")
		}
	case "darwin":
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, "Library", "Application Support")
	default:
		base = os.Getenv("XDG_DATA_HOME")
		if base == "" {
			home, _ := os.UserHomeDir()
			base = filepath.Join(home, ".local", "share")
		}
	}
	p.appData = filepath.Join(base, "easytier-pro-gui")
	p.configDir = filepath.Join(p.appData, "configs")
	p.logDir = filepath.Join(p.appData, "logs")
}

// ensure lazily initialises paths and creates required directories.
func (p *Paths) ensure() {
	p.once.Do(func() {
		p.init()
	})
}

func (p *Paths) BinDir() string {
	p.ensure()
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.binDir
}

func (p *Paths) DriverDir() string {
	p.ensure()
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.drvDir
}

// SetCoreOverride points CorePath/CliPath at the given directory (a coremgr
// version dir containing easytier-core[.exe]). Empty string reverts to the
// bundled binaries.
func (p *Paths) SetCoreOverride(dir string) {
	p.mu.Lock()
	p.coreDirOverride = dir
	p.mu.Unlock()
}

// CoreOverride returns the current core binary override directory.
func (p *Paths) CoreOverride() string {
	p.ensure()
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.coreDirOverride
}

func (p *Paths) CorePath() string {
	p.ensure()
	p.mu.RLock()
	bin := p.binDir
	over := p.coreDirOverride
	p.mu.RUnlock()
	if over != "" {
		bin = over
	}
	core := filepath.Join(bin, "easytier-core.exe")
	if runtime.GOOS != "windows" {
		core = filepath.Join(bin, "easytier-core")
	}
	return core
}

func (p *Paths) CliPath() string {
	p.ensure()
	p.mu.RLock()
	bin := p.binDir
	over := p.coreDirOverride
	p.mu.RUnlock()
	if over != "" {
		bin = over
	}
	cli := filepath.Join(bin, "easytier-cli.exe")
	if runtime.GOOS != "windows" {
		cli = filepath.Join(bin, "easytier-cli")
	}
	return cli
}

func (p *Paths) AppDataDir() string {
	p.ensure()
	p.mu.RLock()
	dir := p.appData
	p.mu.RUnlock()
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

func (p *Paths) ConfigDir() string {
	p.ensure()
	p.mu.RLock()
	dir := p.configDir
	p.mu.RUnlock()
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

func (p *Paths) LogDir() string {
	p.ensure()
	p.mu.RLock()
	dir := p.logDir
	p.mu.RUnlock()
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

// prepare working directory for core: drivers must sit next to the core binary.
// We do this at install time; here we just guarantee the dir exists.
func (p *Paths) ensureCoreWorkDir() string {
	p.ensure()
	p.mu.RLock()
	dir := filepath.Join(p.appData, "runtime")
	p.mu.RUnlock()
	_ = os.MkdirAll(dir, 0o755)
	return dir
}
