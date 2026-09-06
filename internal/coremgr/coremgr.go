// Package coremgr downloads and manages easytier-core versions from the
// official EasyTier GitHub releases. Every version is extracted into its own
// directory under the app-data "cores" root; the GUI then points the core
// paths at the chosen version (core.Paths.SetCoreOverride) and restarts.
package coremgr

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"easytier-pro-gui/internal/easytier"
)

// ReleasesAPI lists EasyTier releases (newest first).
const ReleasesAPI = "https://api.github.com/repos/EasyTier/EasyTier/releases"

// Asset is one downloadable release file.
type Asset struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	Size int64  `json:"size"`
}

// Release is one GitHub release trimmed to what the UI needs.
type Release struct {
	Tag    string  `json:"tag"`
	Date   string  `json:"date"`
	Assets []Asset `json:"assets"`

	body string // release notes, parsed for published sha256 sums
}

type ghRelease struct {
	TagName     string `json:"tag_name"`
	PublishedAt string `json:"published_at"`
	Body        string `json:"body"`
	Assets      []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
		Size               int64  `json:"size"`
	} `json:"assets"`
}

// FetchReleases returns the newest official releases, newest first.
func FetchReleases(ctx context.Context) ([]Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ReleasesAPI+"?per_page=10", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("github releases -> HTTP %d", resp.StatusCode)
	}
	var raw []ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	out := make([]Release, 0, len(raw))
	for _, r := range raw {
		rel := Release{Tag: r.TagName, Date: r.PublishedAt, body: r.Body}
		for _, a := range r.Assets {
			rel.Assets = append(rel.Assets, Asset{Name: a.Name, URL: a.BrowserDownloadURL, Size: a.Size})
		}
		out = append(out, rel)
	}
	return out, nil
}

// CoreBinaryName is the platform executable name of easytier-core.
func CoreBinaryName() string {
	if runtime.GOOS == "windows" {
		return "easytier-core.exe"
	}
	return "easytier-core"
}

// CliBinaryName is the platform executable name of easytier-cli.
func CliBinaryName() string {
	if runtime.GOOS == "windows" {
		return "easytier-cli.exe"
	}
	return "easytier-cli"
}

// assetAliases maps Go arch names to the ones EasyTier release assets use.
var assetAliases = map[string]string{
	"amd64": "x86_64",
	"386":   "i686",
	"arm64": "aarch64",
}

// platformAssetName builds the expected release asset for a platform:
// easytier-<os>-<arch>-v<version>.zip (linux arm uses the armv7 build).
func platformAssetName(goos, goarch, tag string) string {
	arch, ok := assetAliases[goarch]
	if !ok {
		arch = goarch
	}
	osName := goos
	switch goos {
	case "darwin":
		osName = "macos"
	case "linux":
		if goarch == "arm" {
			arch = "armv7"
		}
	case "windows":
	default:
		return ""
	}
	return fmt.Sprintf("easytier-%s-%s-%s.zip", osName, arch, tag)
}

// AssetFor returns the release asset matching the running platform.
func (r Release) AssetFor(goos, goarch string) *Asset {
	want := platformAssetName(goos, goarch, r.Tag)
	for i := range r.Assets {
		if r.Assets[i].Name == want {
			return &r.Assets[i]
		}
	}
	return nil
}

// Manager keeps downloaded core versions under a root directory, one
// subdirectory (named after the release tag) per version.
type Manager struct {
	root string
}

func NewManager(root string) *Manager { return &Manager{root: root} }

// VersionDir is the extraction directory of one release tag.
func (m *Manager) VersionDir(tag string) string {
	return filepath.Join(m.root, filepath.Base(tag)) // Base: tags come from remote data
}

// InstallInfo describes one downloaded core version.
type InstallInfo struct {
	Tag      string `json:"tag"`
	Core     string `json:"core"` // "2.6.4-8428a89d" style version token
	Cli      string `json:"cli"`
	CorePath string `json:"core_path"`
	Active   bool   `json:"active"`
}

// Installed scans the versions root; dirs without an easytier-core binary
// are ignored. Sorted newest tag first.
func (m *Manager) Installed(activeTag string) []InstallInfo {
	entries, err := os.ReadDir(m.root)
	if err != nil {
		return nil
	}
	out := []InstallInfo{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(m.root, e.Name())
		corePath := filepath.Join(dir, CoreBinaryName())
		if _, err := os.Stat(corePath); err != nil {
			continue
		}
		info := InstallInfo{
			Tag:      e.Name(),
			CorePath: corePath,
			Active:   e.Name() == activeTag,
		}
		info.Core = easytier.BinaryVersion(corePath)
		if cli, err := os.Stat(filepath.Join(dir, CliBinaryName())); err == nil {
			_ = cli
			info.Cli = easytier.BinaryVersion(filepath.Join(dir, CliBinaryName()))
		}
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Tag > out[j].Tag })
	return out
}

// Install downloads the platform asset of the release (through the optional
// mirror prefix), verifies the published sha256 when the release notes carry
// one, and extracts core+cli into the version dir. progress, when non-nil,
// receives (loaded, total) during the download.
func (m *Manager) Install(ctx context.Context, rel Release, mirror string, progress func(loaded, total int64)) (string, error) {
	asset := rel.AssetFor(runtime.GOOS, runtime.GOARCH)
	if asset == nil {
		return "", fmt.Errorf("release %s has no asset for %s/%s", rel.Tag, runtime.GOOS, runtime.GOARCH)
	}
	url := asset.URL
	if mirror != "" {
		url = strings.TrimRight(mirror, "/") + "/" + strings.TrimLeft(url, "/")
	}

	tmp, err := os.CreateTemp("", "easytier-core-dl-*.zip")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := download(ctx, url, tmp, progress); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}

	// Verify against the checksum published in the release notes (best
	// effort: older releases may not carry one).
	if want := checksumFor(rel.body, asset.Name); want != "" {
		if got, err := fileSHA256(tmpPath); err == nil && got != want {
			return "", fmt.Errorf("sha256 mismatch for %s: got %s want %s", asset.Name, got, want)
		}
	}

	dir := m.VersionDir(rel.Tag)
	_ = os.RemoveAll(dir) // re-install = fresh extract
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if err := extractCore(tmpPath, dir); err != nil {
		_ = os.RemoveAll(dir)
		return "", err
	}
	meta := fmt.Sprintf(`{"tag":%q,"asset":%q,"date":%q}`, rel.Tag, asset.Name, rel.Date)
	_ = os.WriteFile(filepath.Join(dir, "meta.json"), []byte(meta), 0o644)
	return dir, nil
}

// download streams url into w, reporting progress when non-nil.
func download(ctx context.Context, url string, w io.Writer, progress func(loaded, total int64)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("download %s -> HTTP %d", url, resp.StatusCode)
	}
	if progress == nil {
		_, err = io.Copy(w, resp.Body)
		return err
	}
	buf := make([]byte, 256*1024)
	var loaded int64
	total := resp.ContentLength
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return werr
			}
			loaded += int64(n)
			progress(loaded, total)
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// extractCore pulls easytier-core/easytier-cli out of the release zip into
// dst. Zip entries may sit inside a top-level folder; anything else (docs,
// systemd units, drivers) is ignored — the bundled TUN drivers stay in
// charge on Windows.
func extractCore(zipPath, dst string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()
	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}
		base := filepath.Base(f.Name)
		if base != CoreBinaryName() && base != CliBinaryName() {
			continue
		}
		src, err := f.Open()
		if err != nil {
			return err
		}
		dstPath := filepath.Join(dst, base)
		out, err := os.Create(dstPath)
		if err != nil {
			src.Close()
			return err
		}
		_, err = io.Copy(out, src)
		src.Close()
		out.Close()
		if err != nil {
			return err
		}
		if err := os.Chmod(dstPath, 0o755); err != nil {
			return err
		}
	}
	if _, err := os.Stat(filepath.Join(dst, CoreBinaryName())); err != nil {
		return fmt.Errorf("release zip does not contain %s", CoreBinaryName())
	}
	return nil
}

// shaLine matches the "sha256:<64 hex>" checksum lines in release notes.
var shaLine = regexp.MustCompile(`(?i)^sha256:\s*([0-9a-f]{64})\s*$`)

// checksumFor parses the release notes looking for
//
//	<asset name>
//	sha256:<hex>
//
// pairs and returns the checksum for asset (lowercase hex, "" when absent).
func checksumFor(body, asset string) string {
	last := ""
	for _, ln := range strings.Split(body, "\n") {
		ln = strings.TrimSpace(ln)
		if m := shaLine.FindStringSubmatch(ln); m != nil {
			if last == asset {
				return strings.ToLower(m[1])
			}
			continue
		}
		if ln != "" {
			last = ln
		}
	}
	return ""
}

// fileSHA256 returns the lowercase hex sha256 of a file.
func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
