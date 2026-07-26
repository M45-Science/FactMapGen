package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	factorioStableDownloadURL       = "https://factorio.com/get-download/stable/headless/linux64"
	factorioExperimentalDownloadURL = "https://factorio.com/get-download/experimental/headless/linux64"
	factorioLatestReleasesURL       = "https://factorio.com/api/latest-releases"
	factorioSHA256URL               = "https://www.factorio.com/download/sha256sums/"
	factorioUserAgent               = "FactMapGen"

	factorioVersionCacheTTL  = 15 * time.Minute
	factorioLatestCacheTTL   = 6 * time.Hour
	factorioVersionTimeout   = 8 * time.Second
	factorioQuickHTTPTimeout = 5 * time.Second
	factorioInstallTimeout   = 15 * time.Minute
)

type factorioReleaseChannel string

const (
	factorioReleaseStable         factorioReleaseChannel = "stable"
	factorioReleaseExperimental   factorioReleaseChannel = "experimental"
	factorioDefaultReleaseChannel                        = factorioReleaseExperimental
)

func parseFactorioReleaseChannel(value string) (factorioReleaseChannel, error) {
	channel := factorioReleaseChannel(strings.ToLower(strings.TrimSpace(value)))
	switch channel {
	case factorioReleaseStable, factorioReleaseExperimental:
		return channel, nil
	default:
		return "", fmt.Errorf("must be %q or %q", factorioReleaseStable, factorioReleaseExperimental)
	}
}

var (
	errFactorioInstallUnmanaged = errors.New("Factorio install management is only available for the configured -factorio-dir install")
	errFactorioInstallBusy      = errors.New("Factorio install is already running")
)

type factorioManager struct {
	mu sync.Mutex

	installDir     string
	installManaged bool
	releaseChannel factorioReleaseChannel
	previewer      *previewer

	downloadURL string
	latestURL   string
	sha256URL   string

	versionCache factorioVersionCache
	latestCache  factorioLatestCache
	installing   bool
}

type factorioVersionCache struct {
	bin       string
	modTime   time.Time
	version   string
	err       string
	checkedAt time.Time
}

type factorioLatestCache struct {
	version   string
	err       string
	checkedAt time.Time
}

type factorioStatus struct {
	PreviewEnabled  bool   `json:"previewEnabled"`
	Bin             string `json:"bin,omitempty"`
	InstallDir      string `json:"installDir,omitempty"`
	InstallManaged  bool   `json:"installManaged"`
	ReleaseChannel  string `json:"releaseChannel"`
	Version         string `json:"version,omitempty"`
	VersionCachedAt string `json:"versionCachedAt,omitempty"`
	LatestVersion   string `json:"latestVersion,omitempty"`
	UpdateAvailable bool   `json:"updateAvailable"`
	UpdateCheckedAt string `json:"updateCheckedAt,omitempty"`
	StatusError     string `json:"statusError,omitempty"`
	Installing      bool   `json:"installing"`
}

type factorioLatestBuilds struct {
	Headless string `json:"headless"`
}

type factorioLatestResponse struct {
	Stable       factorioLatestBuilds `json:"stable"`
	Experimental factorioLatestBuilds `json:"experimental"`
}

func newFactorioManager(
	installDir string,
	previewer *previewer,
	managed bool,
	releaseChannel factorioReleaseChannel,
) *factorioManager {
	if releaseChannel != factorioReleaseStable && releaseChannel != factorioReleaseExperimental {
		releaseChannel = factorioDefaultReleaseChannel
	}
	downloadURL := factorioExperimentalDownloadURL
	if releaseChannel == factorioReleaseStable {
		downloadURL = factorioStableDownloadURL
	}
	return &factorioManager{
		installDir:     filepath.Clean(strings.TrimSpace(installDir)),
		installManaged: managed,
		releaseChannel: releaseChannel,
		previewer:      previewer,
		downloadURL:    downloadURL,
		latestURL:      factorioLatestReleasesURL,
		sha256URL:      factorioSHA256URL,
	}
}

func factorioInstallIsManaged(factorioDir, factorioBin string) bool {
	factorioDir = strings.TrimSpace(factorioDir)
	if factorioDir == "" {
		return false
	}
	factorioBin = strings.TrimSpace(factorioBin)
	if factorioBin == "" {
		return true
	}
	root := factorioRootFromBin(factorioBin)
	return root != "" && sameDirectory(root, factorioDir)
}

func (p *previewer) factorioBinary() string {
	if p == nil {
		return ""
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.factorioBin
}

func (p *previewer) setFactorioBinary(path string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.factorioBin = path
}

func (m *factorioManager) expectedBin() string {
	if strings.TrimSpace(m.installDir) == "" {
		return ""
	}
	return filepath.Join(m.installDir, "bin", "x64", "factorio")
}

func (m *factorioManager) status(ctx context.Context, includeLatest bool) factorioStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.statusLocked(ctx, includeLatest)
}

func (m *factorioManager) statusLocked(ctx context.Context, includeLatest bool) factorioStatus {
	bin := ""
	if m.previewer != nil {
		bin = m.previewer.factorioBinary()
	}
	status := factorioStatus{
		PreviewEnabled: bin != "",
		Bin:            bin,
		InstallDir:     m.installDir,
		InstallManaged: m.installManaged,
		ReleaseChannel: string(m.releaseChannel),
		Installing:     m.installing,
	}

	if bin == "" {
		if m.installManaged {
			status.Bin = m.expectedBin()
			status.StatusError = "Factorio is not installed."
		} else {
			status.StatusError = "Factorio binary is not configured."
		}
		m.applyLatestStatusLocked(ctx, &status, includeLatest)
		return status
	}

	info, err := os.Stat(bin)
	if err != nil {
		status.PreviewEnabled = false
		status.StatusError = err.Error()
		m.applyLatestStatusLocked(ctx, &status, includeLatest)
		return status
	}

	now := time.Now()
	if m.versionCache.bin != bin ||
		!m.versionCache.modTime.Equal(info.ModTime()) ||
		now.Sub(m.versionCache.checkedAt) > factorioVersionCacheTTL {
		versionCtx, cancel := context.WithTimeout(ctx, factorioVersionTimeout)
		version, versionErr := runFactorioVersion(versionCtx, bin)
		cancel()
		m.versionCache = factorioVersionCache{
			bin:       bin,
			modTime:   info.ModTime(),
			version:   version,
			checkedAt: now,
		}
		if versionErr != nil {
			m.versionCache.err = versionErr.Error()
		}
	}

	status.Version = m.versionCache.version
	status.VersionCachedAt = formatCacheTime(m.versionCache.checkedAt)
	if m.versionCache.err != "" {
		status.StatusError = m.versionCache.err
	}
	m.applyLatestStatusLocked(ctx, &status, includeLatest)
	return status
}

func (m *factorioManager) applyLatestStatusLocked(ctx context.Context, status *factorioStatus, includeLatest bool) {
	if !includeLatest {
		return
	}
	now := time.Now()
	latestTTL := factorioLatestCacheTTL
	if m.latestCache.err != "" {
		latestTTL = factorioVersionCacheTTL
	}
	if m.latestCache.checkedAt.IsZero() || now.Sub(m.latestCache.checkedAt) > latestTTL {
		latestCtx, cancel := context.WithTimeout(ctx, factorioQuickHTTPTimeout)
		latest, err := m.fetchLatestVersion(latestCtx)
		cancel()
		m.latestCache = factorioLatestCache{version: latest, checkedAt: now}
		if err != nil {
			m.latestCache.err = err.Error()
		}
	}

	status.LatestVersion = m.latestCache.version
	status.UpdateCheckedAt = formatCacheTime(m.latestCache.checkedAt)
	if status.StatusError == "" && m.latestCache.err != "" {
		status.StatusError = m.latestCache.err
	}
	if status.Version != "" && status.LatestVersion != "" {
		status.UpdateAvailable = factorioVersionNewer(status.LatestVersion, status.Version)
	}
}

func (m *factorioManager) installFresh(ctx context.Context) (factorioStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.installManaged {
		return m.statusLocked(ctx, false), errFactorioInstallUnmanaged
	}
	installDir, err := safeFactorioInstallDir(m.installDir)
	if err != nil {
		return m.statusLocked(ctx, false), err
	}
	if m.installing {
		return m.statusLocked(ctx, false), errFactorioInstallBusy
	}
	m.installing = true
	fail := func(err error) (factorioStatus, error) {
		m.installing = false
		return m.statusLocked(ctx, false), err
	}

	runCtx, cancel := context.WithTimeout(ctx, factorioInstallTimeout)
	defer cancel()

	latest, err := m.fetchLatestVersion(runCtx)
	if err != nil {
		return fail(fmt.Errorf("checking latest Factorio release: %w", err))
	}
	archivePath, archiveHash, cleanup, err := m.downloadArchive(runCtx)
	if err != nil {
		return fail(fmt.Errorf("downloading Factorio: %w", err))
	}
	defer cleanup()

	expectedHash, err := m.fetchSHA256(runCtx, latest)
	if err != nil {
		return fail(fmt.Errorf("checking Factorio checksum: %w", err))
	}
	if !strings.EqualFold(archiveHash, expectedHash) {
		return fail(errors.New("downloaded Factorio archive checksum did not match"))
	}
	if err := validateFactorioArchive(runCtx, archivePath); err != nil {
		return fail(fmt.Errorf("validating Factorio archive: %w", err))
	}

	if err := os.RemoveAll(installDir); err != nil {
		return fail(fmt.Errorf("removing current Factorio install: %w", err))
	}
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		return fail(fmt.Errorf("creating Factorio install directory: %w", err))
	}
	if err := extractFactorioArchive(runCtx, archivePath, installDir); err != nil {
		return fail(fmt.Errorf("extracting Factorio archive: %w", err))
	}

	bin := filepath.Join(installDir, "bin", "x64", "factorio")
	if _, err := os.Stat(bin); err != nil {
		return fail(fmt.Errorf("installed Factorio binary was not found: %w", err))
	}
	m.previewer.setFactorioBinary(normalizeFactorioBin(bin))
	m.versionCache = factorioVersionCache{}
	m.latestCache = factorioLatestCache{version: latest, checkedAt: time.Now()}
	m.installing = false
	return m.statusLocked(ctx, true), nil
}

func (m *factorioManager) fetchLatestVersion(ctx context.Context) (string, error) {
	body, err := httpGet(ctx, m.latestURL, factorioQuickHTTPTimeout)
	if err != nil {
		return "", err
	}
	var latest factorioLatestResponse
	if err := json.Unmarshal(body, &latest); err != nil {
		return "", err
	}
	version := strings.TrimSpace(latest.Experimental.Headless)
	if m.releaseChannel == factorioReleaseStable {
		version = strings.TrimSpace(latest.Stable.Headless)
	}
	if _, err := parseFactorioVersionNumber(version); err != nil {
		return "", err
	}
	return version, nil
}

func (m *factorioManager) fetchSHA256(ctx context.Context, version string) (string, error) {
	body, err := httpGet(ctx, m.sha256URL, factorioQuickHTTPTimeout)
	if err != nil {
		return "", err
	}
	wantName := fmt.Sprintf("factorio-headless_linux_%s.tar.xz", version)
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == wantName {
			if matched, _ := regexp.MatchString(`^[A-Fa-f0-9]{64}$`, fields[0]); !matched {
				return "", fmt.Errorf("invalid checksum for %s", wantName)
			}
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("checksum for %s not found", wantName)
}

func (m *factorioManager) downloadArchive(ctx context.Context) (string, string, func(), error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.downloadURL, nil)
	if err != nil {
		return "", "", func() {}, err
	}
	req.Header.Set("User-Agent", factorioUserAgent)
	client := &http.Client{Timeout: factorioInstallTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", func() {}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", func() {}, fmt.Errorf("%d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	}

	tmp, err := os.CreateTemp("", "factorio-headless-*.tar.xz")
	if err != nil {
		return "", "", func() {}, err
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }

	hash := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, hash), resp.Body); err != nil {
		_ = tmp.Close()
		cleanup()
		return "", "", func() {}, err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return "", "", func() {}, err
	}
	return tmpPath, fmt.Sprintf("%x", hash.Sum(nil)), cleanup, nil
}

func httpGet(ctx context.Context, url string, timeout time.Duration) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", factorioUserAgent)
	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		detail := strings.TrimSpace(string(body))
		if detail != "" {
			return nil, fmt.Errorf("%d %s: %s", resp.StatusCode, http.StatusText(resp.StatusCode), detail)
		}
		return nil, fmt.Errorf("%d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	}
	return io.ReadAll(resp.Body)
}

func runFactorioVersion(ctx context.Context, bin string) (string, error) {
	cmd := exec.CommandContext(ctx, bin, "--version")
	if root := factorioRootFromBin(bin); root != "" {
		cmd.Dir = root
	}
	output, err := cmd.CombinedOutput()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "", fmt.Errorf("Factorio version check timed out after %s", factorioVersionTimeout)
	}
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, factorioErrorSummary(string(output)))
	}
	return parseFactorioVersionOutput(string(output))
}

func parseFactorioVersionOutput(output string) (string, error) {
	re := regexp.MustCompile(`(?m)^Version:\s+([0-9]+(?:\.[0-9]+){2})\b`)
	matches := re.FindStringSubmatch(output)
	if len(matches) == 2 {
		return matches[1], nil
	}
	return "", errors.New("unable to parse Factorio version output")
}

type factorioVersionParts struct {
	major int
	minor int
	patch int
}

func parseFactorioVersionNumber(version string) (factorioVersionParts, error) {
	parts := strings.Split(strings.TrimSpace(version), ".")
	if len(parts) != 3 {
		return factorioVersionParts{}, fmt.Errorf("invalid Factorio version %q", version)
	}
	var parsed [3]int
	for i, part := range parts {
		if part == "" {
			return factorioVersionParts{}, fmt.Errorf("invalid Factorio version %q", version)
		}
		var value int
		for _, ch := range part {
			if ch < '0' || ch > '9' {
				return factorioVersionParts{}, fmt.Errorf("invalid Factorio version %q", version)
			}
			value = value*10 + int(ch-'0')
		}
		parsed[i] = value
	}
	return factorioVersionParts{major: parsed[0], minor: parsed[1], patch: parsed[2]}, nil
}

func factorioVersionNewer(candidate, current string) bool {
	a, errA := parseFactorioVersionNumber(candidate)
	b, errB := parseFactorioVersionNumber(current)
	if errA != nil || errB != nil {
		return false
	}
	if a.major != b.major {
		return a.major > b.major
	}
	if a.minor != b.minor {
		return a.minor > b.minor
	}
	return a.patch > b.patch
}

func validateFactorioArchive(ctx context.Context, archivePath string) error {
	cmd := exec.CommandContext(ctx, "tar", "-tf", archivePath)
	output, err := cmd.Output()
	if err != nil {
		return err
	}
	required := []string{
		"factorio/bin/x64/factorio",
		"factorio/data/core/info.json",
		"factorio/data/base/info.json",
	}
	for _, item := range required {
		if !bytes.Contains(output, []byte(item+"\n")) {
			return fmt.Errorf("%s missing from archive", item)
		}
	}
	return nil
}

func extractFactorioArchive(ctx context.Context, archivePath, installDir string) error {
	cmd := exec.CommandContext(ctx, "tar", "-xf", archivePath, "--strip-components=1", "-C", installDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, factorioErrorSummary(string(output)))
	}
	return nil
}

func safeFactorioInstallDir(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("Factorio install directory is empty")
	}
	abs, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	if abs == string(os.PathSeparator) {
		return "", errors.New("refusing to use filesystem root as Factorio install directory")
	}
	if wd, err := os.Getwd(); err == nil && sameDirectory(abs, wd) {
		return "", errors.New("refusing to use repository root as Factorio install directory")
	}
	if filepath.Base(abs) == "." || filepath.Base(abs) == string(os.PathSeparator) {
		return "", errors.New("invalid Factorio install directory")
	}
	return abs, nil
}

func formatCacheTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func writeFactorioError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, errFactorioInstallUnmanaged):
		status = http.StatusForbidden
	case errors.Is(err, errFactorioInstallBusy):
		status = http.StatusConflict
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		status = http.StatusGatewayTimeout
	}
	writeError(w, status, err.Error())
}
