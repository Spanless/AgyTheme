package updater

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	agytheme "github.com/Spanless/AgyTheme"
	"github.com/Spanless/AgyTheme/internal/bundle"
	"github.com/Spanless/AgyTheme/internal/installer"
	"github.com/Spanless/AgyTheme/internal/shared"
)

const (
	DefaultAPIBaseURL = "https://api.github.com/repos/Spanless/AgyTheme"
	maxZipBytes       = 128 << 20
	maxSumsBytes      = 1 << 20
	maxEntryBytes     = 64 << 20
)

var semverRe = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$`)

var defaultAllowedHosts = []string{
	"api.github.com",
	"github.com",
	"objects.githubusercontent.com",
	"release-assets.githubusercontent.com",
	"github-releases.githubusercontent.com",
}

type CheckResult struct {
	CurrentVersion  string `json:"currentVersion"`
	LatestVersion   string `json:"latestVersion,omitempty"`
	UpdateAvailable bool   `json:"updateAvailable"`
	Status          string `json:"status"` // up_to_date | update_available | no_release | error
	ReleaseNote     string `json:"releaseNote,omitempty"`
	ReleaseURL      string `json:"releaseUrl,omitempty"`
	AssetName       string `json:"assetName,omitempty"`
	AssetURL        string `json:"-"`
	SumsURL         string `json:"-"`
	CheckedAt       int64  `json:"checkedAt"`
	Error           string `json:"error,omitempty"`
}

type Status struct {
	Phase           string `json:"phase"` // idle | checking | downloading | verifying | installing | restarting | done | error
	Percent         int    `json:"percent"`
	Message         string `json:"message,omitempty"`
	Error           string `json:"error,omitempty"`
	CurrentVersion  string `json:"currentVersion"`
	LatestVersion   string `json:"latestVersion,omitempty"`
	UpdateAvailable bool   `json:"updateAvailable"`
	CheckStatus     string `json:"checkStatus,omitempty"`
	ReleaseNote     string `json:"releaseNote,omitempty"`
	ReleaseURL      string `json:"releaseUrl,omitempty"`
	CheckedAt       int64  `json:"checkedAt,omitempty"`
}

type RollbackResult struct {
	Version     string `json:"version"`
	PrevVersion string `json:"prevVersion,omitempty"`
	RestoredTo  string `json:"restoredTo"`
}

type Config struct {
	CurrentVersion string
	PluginDir      string
	RootDir        string
	GOOS           string
	GOARCH         string
	APIBaseURL     string
	HTTPClient     *http.Client
	PostSwap       func(newBin, rootDir string)
}

type Updater struct {
	cfg      Config
	mu       sync.Mutex
	last     *CheckResult
	status   Status
	applying bool
}

func New(cfg Config) *Updater {
	if cfg.CurrentVersion == "" {
		cfg.CurrentVersion = agytheme.Version
	}
	if cfg.PluginDir == "" {
		cfg.PluginDir = installer.PluginDir(installer.DefaultPluginsDir())
	}
	if cfg.RootDir == "" {
		cfg.RootDir = installer.DefaultTargetDir()
	}
	if cfg.GOOS == "" {
		cfg.GOOS = runtime.GOOS
	}
	if cfg.GOARCH == "" {
		cfg.GOARCH = runtime.GOARCH
	}
	if cfg.APIBaseURL == "" {
		cfg.APIBaseURL = DefaultAPIBaseURL
	}
	u := &Updater{
		cfg: cfg,
		status: Status{
			Phase:          "idle",
			CurrentVersion: normalizeVer(cfg.CurrentVersion),
		},
	}
	return u
}

func (u *Updater) Status() Status {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.status
}

func (u *Updater) PrevVersion() string {
	prev := installer.PluginPrevBinary(u.cfg.PluginDir)
	if !shared.FileExists(prev) {
		return ""
	}
	var st bundle.InstallState
	if shared.ReadJSONFile(filepath.Join(u.cfg.RootDir, "state.json"), &st) && st.PrevVersion != "" {
		return normalizeVer(st.PrevVersion)
	}
	return ""
}

func (u *Updater) RollbackAvailable() bool {
	return shared.FileExists(installer.PluginPrevBinary(u.cfg.PluginDir))
}

func (u *Updater) client() *http.Client {
	base := u.cfg.HTTPClient
	if base == nil {
		base = &http.Client{Timeout: 90 * time.Second}
	}
	clone := *base
	clone.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("too many redirects")
		}
		return u.validateURL(req.URL.String())
	}
	return &clone
}

func (u *Updater) validateURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid url: %w", err)
	}
	customBase, _ := url.Parse(u.cfg.APIBaseURL)
	if customBase != nil && customBase.Host != "" && strings.EqualFold(parsed.Host, customBase.Host) {
		if parsed.Scheme == "https" || (customBase.Scheme == "http" && parsed.Scheme == "http") {
			return nil
		}
		return fmt.Errorf("unsupported scheme %q", parsed.Scheme)
	}
	if parsed.Scheme != "https" {
		return fmt.Errorf("non-https download url rejected: %s", parsed.Scheme)
	}
	host := strings.ToLower(parsed.Hostname())
	for _, allowed := range defaultAllowedHosts {
		if host == allowed {
			return nil
		}
	}
	return fmt.Errorf("untrusted host %q", host)
}

type ghRelease struct {
	TagName string    `json:"tag_name"`
	Body    string    `json:"body"`
	HTMLURL string    `json:"html_url"`
	Assets  []ghAsset `json:"assets"`
}

type ghAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

func (u *Updater) Check(ctx context.Context) CheckResult {
	cur := normalizeVer(u.cfg.CurrentVersion)
	now := time.Now().Unix()

	u.mu.Lock()
	if !u.applying {
		u.status.Phase = "checking"
		u.status.Error = ""
		u.status.Message = "正在检查更新…"
	}
	u.mu.Unlock()

	endpoint := strings.TrimRight(u.cfg.APIBaseURL, "/") + "/releases/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return u.finishCheck(CheckResult{CurrentVersion: cur, Status: "error", CheckedAt: now, Error: err.Error()})
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "AgyTheme/"+cur)

	resp, err := u.client().Do(req)
	if err != nil {
		return u.finishCheck(CheckResult{CurrentVersion: cur, Status: "error", CheckedAt: now, Error: "网络请求失败"})
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return u.finishCheck(CheckResult{CurrentVersion: cur, Status: "no_release", CheckedAt: now})
	}
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
		return u.finishCheck(CheckResult{CurrentVersion: cur, Status: "error", CheckedAt: now, Error: "请求受限，请稍后重试"})
	}
	if resp.StatusCode != http.StatusOK {
		return u.finishCheck(CheckResult{CurrentVersion: cur, Status: "error", CheckedAt: now, Error: fmt.Sprintf("检查失败 (HTTP %d)", resp.StatusCode)})
	}

	var rel ghRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxSumsBytes)).Decode(&rel); err != nil {
		return u.finishCheck(CheckResult{CurrentVersion: cur, Status: "error", CheckedAt: now, Error: "解析版本信息失败"})
	}

	latest := normalizeVer(rel.TagName)
	if latest == "" {
		return u.finishCheck(CheckResult{CurrentVersion: cur, Status: "no_release", CheckedAt: now})
	}
	if !semverRe.MatchString(latest) {
		return u.finishCheck(CheckResult{CurrentVersion: cur, Status: "error", CheckedAt: now, Error: "版本标签格式无效"})
	}

	avail := CompareVersion(latest, cur) > 0
	assetName, assetURL, sumsURL := pickReleaseAssets(rel.Assets, latest, u.cfg.GOOS)
	status := "up_to_date"
	if avail {
		status = "update_available"
	}
	return u.finishCheck(CheckResult{
		CurrentVersion:  cur,
		LatestVersion:   latest,
		UpdateAvailable: avail,
		Status:          status,
		ReleaseNote:     summarizeReleaseBody(rel.Body),
		ReleaseURL:      rel.HTMLURL,
		AssetName:       assetName,
		AssetURL:        assetURL,
		SumsURL:         sumsURL,
		CheckedAt:       now,
	})
}

func (u *Updater) finishCheck(res CheckResult) CheckResult {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.last = &res
	if !u.applying {
		u.status.Phase = "idle"
		u.status.Percent = 0
		u.status.Message = ""
		u.status.Error = res.Error
		u.status.CurrentVersion = res.CurrentVersion
		u.status.LatestVersion = res.LatestVersion
		u.status.UpdateAvailable = res.UpdateAvailable
		u.status.CheckStatus = res.Status
		u.status.ReleaseNote = res.ReleaseNote
		u.status.ReleaseURL = res.ReleaseURL
		u.status.CheckedAt = res.CheckedAt
	}
	return res
}

func (u *Updater) StartApply() Status {
	u.mu.Lock()
	if u.applying {
		st := u.status
		u.mu.Unlock()
		return st
	}
	u.applying = true
	u.status.Phase = "downloading"
	u.status.Percent = 5
	u.status.Message = "准备更新…"
	u.status.Error = ""
	st := u.status
	u.mu.Unlock()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		_ = u.runApply(ctx)
	}()
	return st
}

func (u *Updater) Apply(ctx context.Context) error {
	u.mu.Lock()
	if u.applying {
		u.mu.Unlock()
		return errors.New("update already in progress")
	}
	u.applying = true
	u.status.Phase = "downloading"
	u.status.Percent = 5
	u.status.Message = "准备更新…"
	u.status.Error = ""
	u.mu.Unlock()
	return u.runApply(ctx)
}

func (u *Updater) setProgress(phase string, pct int, msg string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.status.Phase = phase
	u.status.Percent = pct
	u.status.Message = msg
}

func (u *Updater) failApply(err error) error {
	u.mu.Lock()
	u.applying = false
	u.status.Phase = "error"
	u.status.Error = err.Error()
	u.status.Message = err.Error()
	u.mu.Unlock()
	return err
}

func (u *Updater) runApply(ctx context.Context) error {
	u.mu.Lock()
	cached := u.last
	u.mu.Unlock()

	var rel CheckResult
	if cached != nil && cached.UpdateAvailable && cached.AssetURL != "" {
		rel = *cached
	} else {
		rel = u.Check(ctx)
		if rel.Status == "error" {
			return u.failApply(errors.New(rel.Error))
		}
		if !rel.UpdateAvailable {
			u.mu.Lock()
			u.applying = false
			u.status.Phase = "idle"
			u.status.Percent = 0
			u.status.Message = "已是最新版本"
			u.mu.Unlock()
			return nil
		}
	}

	if rel.AssetURL == "" || rel.AssetName == "" {
		return u.failApply(errors.New("未找到当前平台安装包"))
	}
	if filepath.Base(rel.AssetName) != rel.AssetName || strings.ContainsAny(rel.AssetName, `/\`) {
		return u.failApply(errors.New("安装包文件名无效"))
	}
	if rel.SumsURL == "" {
		return u.failApply(errors.New("发布包缺少校验文件"))
	}
	if err := u.validateURL(rel.AssetURL); err != nil {
		return u.failApply(err)
	}
	if err := u.validateURL(rel.SumsURL); err != nil {
		return u.failApply(err)
	}

	tmpDir, err := os.MkdirTemp("", "agytheme-update-*")
	if err != nil {
		return u.failApply(fmt.Errorf("创建临时目录失败: %w", err))
	}
	defer os.RemoveAll(tmpDir)

	u.setProgress("downloading", 10, "正在下载更新包…")
	zipPath := filepath.Join(tmpDir, rel.AssetName)
	if err := u.downloadFile(ctx, rel.AssetURL, zipPath, maxZipBytes, func(done, total int64) {
		if total > 0 {
			pct := 10 + int((done*55)/total)
			if pct > 65 {
				pct = 65
			}
			u.setProgress("downloading", pct, "正在下载更新包…")
		}
	}); err != nil {
		return u.failApply(fmt.Errorf("下载失败: %w", err))
	}

	u.setProgress("verifying", 68, "正在校验安装包…")
	sumsBytes, err := u.downloadBytes(ctx, rel.SumsURL, maxSumsBytes)
	if err != nil {
		return u.failApply(fmt.Errorf("获取校验信息失败: %w", err))
	}
	expectedHash, ok := ParseSHA256Sums(string(sumsBytes), rel.AssetName)
	if !ok {
		return u.failApply(errors.New("校验清单中缺少当前安装包条目"))
	}
	actualHash, err := fileSHA256(zipPath)
	if err != nil {
		return u.failApply(fmt.Errorf("校验计算失败: %w", err))
	}
	if subtle.ConstantTimeCompare([]byte(strings.ToLower(actualHash)), []byte(strings.ToLower(expectedHash))) != 1 {
		return u.failApply(errors.New("安装包完整性校验失败"))
	}

	u.setProgress("installing", 78, "正在替换程序…")
	if err := u.installFromZip(zipPath, rel.LatestVersion); err != nil {
		return u.failApply(err)
	}

	u.setProgress("restarting", 92, "正在重启服务…")
	bin := u.pluginBinary()
	if _, err := installer.WriteSidecar(u.cfg.PluginDir, bin, u.cfg.RootDir); err != nil {
		return u.failApply(fmt.Errorf("更新 sidecar 配置失败: %w", err))
	}
	u.triggerPostSwap(bin)

	u.mu.Lock()
	u.applying = false
	u.cfg.CurrentVersion = rel.LatestVersion
	u.status.Phase = "done"
	u.status.Percent = 100
	u.status.Message = "已更新到 v" + rel.LatestVersion
	u.status.CurrentVersion = rel.LatestVersion
	u.status.LatestVersion = rel.LatestVersion
	u.status.UpdateAvailable = false
	u.status.CheckStatus = "up_to_date"
	u.status.Error = ""
	u.mu.Unlock()
	return nil
}

func (u *Updater) Rollback() (RollbackResult, error) {
	u.mu.Lock()
	if u.applying {
		u.mu.Unlock()
		return RollbackResult{}, errors.New("正在更新中，请稍候")
	}
	u.mu.Unlock()

	bin := u.pluginBinary()
	prev := u.pluginPrevBinary()
	if !shared.FileExists(prev) {
		return RollbackResult{}, errors.New("没有可回滚的备份版本")
	}

	tmp := bin + ".rollback"
	_ = os.Remove(tmp)
	if shared.FileExists(bin) {
		if err := os.Rename(bin, tmp); err != nil {
			return RollbackResult{}, fmt.Errorf("无法移动当前程序: %w", err)
		}
	}
	if err := os.Rename(prev, bin); err != nil {
		_ = os.Rename(tmp, bin)
		return RollbackResult{}, fmt.Errorf("无法恢复备份程序: %w", err)
	}
	if shared.FileExists(tmp) {
		_ = os.Rename(tmp, prev)
	}

	curVer, prevVer := u.swapStateVersions()
	if _, err := installer.WriteSidecar(u.cfg.PluginDir, bin, u.cfg.RootDir); err != nil {
		return RollbackResult{}, fmt.Errorf("重写 sidecar 配置失败: %w", err)
	}
	u.triggerPostSwap(bin)

	u.mu.Lock()
	if curVer != "" {
		u.cfg.CurrentVersion = curVer
		u.status.CurrentVersion = curVer
	}
	u.mu.Unlock()

	return RollbackResult{Version: curVer, PrevVersion: prevVer, RestoredTo: bin}, nil
}

func (u *Updater) pluginBinary() string {
	name := "agytheme"
	if u.cfg.GOOS == "windows" {
		name = "agytheme.exe"
	}
	return filepath.Join(u.cfg.PluginDir, "bin", name)
}

func (u *Updater) pluginPrevBinary() string {
	return u.pluginBinary() + ".prev"
}

func (u *Updater) triggerPostSwap(bin string) {
	if u.cfg.PostSwap != nil {
		u.cfg.PostSwap(bin, u.cfg.RootDir)
		return
	}
	cmd := exec.Command(bin, "daemon", "--once", "--root", u.cfg.RootDir)
	if err := cmd.Start(); err == nil {
		go func() { _ = cmd.Wait() }()
	}
}

func (u *Updater) installFromZip(zipPath, newVersion string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("打开更新包失败: %w", err)
	}
	defer zr.Close()

	wantBinName := fmt.Sprintf("agytheme-%s-%s", u.cfg.GOOS, u.cfg.GOARCH)
	if u.cfg.GOOS == "windows" {
		wantBinName += ".exe"
	}
	wantBinEntry := "agytheme/bin/" + wantBinName

	var binData []byte
	metaFiles := map[string][]byte{}
	metaMap := map[string]string{
		"agytheme/plugin.json":             "plugin.json",
		"agytheme/README.md":               "README.md",
		"agytheme/assets/logo.svg":         "assets/logo.svg",
		"agytheme/skills/agytheme/SKILL.md": "skills/agytheme/SKILL.md",
		"agytheme/SKILL.md":                "skills/agytheme/SKILL.md",
	}

	for _, f := range zr.File {
		name := filepath.ToSlash(strings.TrimPrefix(f.Name, "./"))
		if name == wantBinEntry {
			data, err := readZipEntry(f, maxEntryBytes)
			if err != nil {
				return fmt.Errorf("读取新版本程序失败: %w", err)
			}
			binData = data
		} else if dest, ok := metaMap[name]; ok {
			if _, exists := metaFiles[dest]; !exists {
				if data, err := readZipEntry(f, maxSumsBytes); err == nil {
					metaFiles[dest] = data
				}
			}
		}
	}
	if len(binData) == 0 {
		return fmt.Errorf("更新包内未找到适配当前平台的程序 (%s)", wantBinName)
	}

	bin := u.pluginBinary()
	prev := u.pluginPrevBinary()
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		return fmt.Errorf("创建插件目录失败: %w", err)
	}

	staged := bin + ".new"
	if err := os.WriteFile(staged, binData, 0o755); err != nil {
		return fmt.Errorf("写入新程序失败: %w", err)
	}
	_ = os.Chmod(staged, 0o755)

	hadOld := shared.FileExists(bin)
	if hadOld {
		if err := os.Remove(prev); err != nil && !os.IsNotExist(err) {
			_ = os.Rename(prev, fmt.Sprintf("%s.%d.old", prev, time.Now().Unix()))
		}
		if err := os.Rename(bin, prev); err != nil {
			_ = os.Remove(staged)
			return fmt.Errorf("备份当前程序失败: %w", err)
		}
	}
	if err := os.Rename(staged, bin); err != nil {
		_ = os.Remove(staged)
		if hadOld {
			_ = os.Rename(prev, bin)
		}
		return fmt.Errorf("替换程序失败: %w", err)
	}

	for rel, data := range metaFiles {
		dst := filepath.Join(u.cfg.PluginDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err == nil {
			_ = installer.WriteFileAtomic(dst, data, 0o644)
		}
	}

	u.recordStateVersion(newVersion, normalizeVer(u.cfg.CurrentVersion))
	return nil
}

func (u *Updater) recordStateVersion(newVer, prevVer string) {
	statePath := filepath.Join(u.cfg.RootDir, "state.json")
	var st bundle.InstallState
	shared.ReadJSONFile(statePath, &st)
	if prevVer != "" && prevVer != newVer {
		st.PrevVersion = prevVer
	}
	st.Version = newVer
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err == nil {
		_ = installer.WriteFileAtomic(statePath, shared.MarshalPretty(st, "  "), 0o644)
	}
}

func (u *Updater) swapStateVersions() (string, string) {
	statePath := filepath.Join(u.cfg.RootDir, "state.json")
	var st bundle.InstallState
	shared.ReadJSONFile(statePath, &st)
	cur := normalizeVer(st.Version)
	if cur == "" {
		cur = normalizeVer(u.cfg.CurrentVersion)
	}
	prev := normalizeVer(st.PrevVersion)
	st.Version = prev
	st.PrevVersion = cur
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err == nil {
		_ = installer.WriteFileAtomic(statePath, shared.MarshalPretty(st, "  "), 0o644)
	}
	return prev, cur
}

func (u *Updater) downloadFile(ctx context.Context, rawURL, dst string, limit int64, onProgress func(done, total int64)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "AgyTheme/"+normalizeVer(u.cfg.CurrentVersion))
	resp, err := u.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > limit {
		return errors.New("安装包体积超出限制")
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()

	buf := make([]byte, 32*1024)
	var written int64
	lr := io.LimitReader(resp.Body, limit+1)
	for {
		n, rerr := lr.Read(buf)
		if n > 0 {
			written += int64(n)
			if written > limit {
				return errors.New("安装包体积超出限制")
			}
			if _, werr := f.Write(buf[:n]); werr != nil {
				return werr
			}
			if onProgress != nil {
				onProgress(written, resp.ContentLength)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return rerr
		}
	}
	return f.Close()
}

func (u *Updater) downloadBytes(ctx context.Context, rawURL string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "AgyTheme/"+normalizeVer(u.cfg.CurrentVersion))
	resp, err := u.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("响应内容过大")
	}
	return data, nil
}

func readZipEntry(f *zip.File, limit int64) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("文件体积超出限制")
	}
	return data, nil
}

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

func pickReleaseAssets(assets []ghAsset, version, goos string) (name, url, sumsURL string) {
	wantPlatform := fmt.Sprintf("AgyTheme-%s-%s-universal.zip", version, goos)
	wantAll := fmt.Sprintf("AgyTheme-%s-all-platforms.zip", version)
	var fallbackName, fallbackURL string
	for _, a := range assets {
		switch a.Name {
		case wantPlatform:
			name, url = a.Name, a.BrowserDownloadURL
		case wantAll:
			fallbackName, fallbackURL = a.Name, a.BrowserDownloadURL
		case "SHA256SUMS":
			sumsURL = a.BrowserDownloadURL
		}
	}
	if name == "" {
		name, url = fallbackName, fallbackURL
	}
	return name, url, sumsURL
}

func ParseSHA256Sums(content, filename string) (string, bool) {
	for _, line := range strings.Split(content, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 2 {
			continue
		}
		hash := fields[0]
		target := strings.TrimPrefix(fields[len(fields)-1], "*")
		if len(hash) == 64 && target == filename {
			return hash, true
		}
	}
	return "", false
}

func summarizeReleaseBody(body string) string {
	var parts []string
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "```") || strings.HasPrefix(line, "|") {
			continue
		}
		line = strings.TrimLeft(line, "-*• ")
		line = strings.ReplaceAll(line, "`", "")
		line = strings.ReplaceAll(line, "**", "")
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts = append(parts, line)
		if len(parts) >= 2 {
			break
		}
	}
	joined := strings.Join(parts, " · ")
	runes := []rune(joined)
	if len(runes) > 64 {
		return string(runes[:63]) + "…"
	}
	return joined
}

func normalizeVer(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	v = strings.TrimPrefix(v, "V")
	return v
}

func CompareVersion(a, b string) int {
	pa, preA := splitVer(a)
	pb, preB := splitVer(b)
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			if pa[i] > pb[i] {
				return 1
			}
			return -1
		}
	}
	if preA == preB {
		return 0
	}
	if preA == "" {
		return 1
	}
	if preB == "" {
		return -1
	}
	if preA > preB {
		return 1
	}
	return -1
}

func splitVer(v string) ([3]int, string) {
	v = normalizeVer(v)
	var pre string
	if idx := strings.IndexByte(v, '-'); idx >= 0 {
		pre = v[idx+1:]
		v = v[:idx]
	}
	var out [3]int
	parts := strings.Split(v, ".")
	for i := 0; i < len(parts) && i < 3; i++ {
		n, _ := strconv.Atoi(parts[i])
		out[i] = n
	}
	return out, pre
}
