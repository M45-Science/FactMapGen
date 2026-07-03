package main

import (
	"archive/zip"
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	mapGenFile             = "map-gen-settings.json"
	mapSettingsFile        = "map-settings.json"
	previewJPEGQuality     = 85
	previewAVIFCRF         = 35
	previewAVIFCPUUsed     = 6
	maxPreviewOutputSize   = 4096
	maxFactorioPreviewSize = 16384
	previewImageRetention  = 30 * time.Minute
	maxPreviewImages       = 100
)

//go:embed web/* templates/*.json
var embedded embed.FS

type server struct {
	store        *store
	previewer    *previewer
	previewQueue *previewQueue
	auth         *authStore
	factorio     *factorioManager
	config       appConfig
}

type store struct {
	defaultRoot string
	customRoot  string
}

type previewer struct {
	mu          sync.RWMutex
	factorioBin string
	timeout     time.Duration
	images      map[string]previewImage
}

type previewImage struct {
	data        []byte
	contentType string
	createdAt   time.Time
	expiresAt   time.Time
}

type appConfig struct {
	PresetDir        string         `json:"presetDir"`
	DefaultPresetDir string         `json:"defaultPresetDir"`
	CustomPresetDir  string         `json:"customPresetDir"`
	PreviewEnabled   bool           `json:"previewEnabled"`
	FactorioBin      string         `json:"factorioBin,omitempty"`
	Factorio         factorioStatus `json:"factorio"`
}

type createProfileRequest struct {
	Name   string `json:"name"`
	Preset string `json:"preset"`
}

type saveProfileRequest struct {
	MapGen      json.RawMessage `json:"mapGen"`
	MapSettings json.RawMessage `json:"mapSettings"`
}

type downloadProfileRequest struct {
	MapGen      json.RawMessage `json:"mapGen"`
	MapSettings json.RawMessage `json:"mapSettings"`
}

type duplicateProfileRequest struct {
	Name string `json:"name"`
}

type previewRequest struct {
	Size     int             `json:"size"`
	Planet   string          `json:"planet"`
	Seed     string          `json:"seed"`
	Lossless bool            `json:"lossless"`
	Zoom     string          `json:"zoom"`
	MapGen   json.RawMessage `json:"mapGen"`
}

type previewResponse struct {
	URL         string `json:"url"`
	GeneratedAt string `json:"generatedAt"`
	Size        int    `json:"size"`
	Planet      string `json:"planet"`
	Output      string `json:"output"`
}

type profileSummary struct {
	Name             string `json:"name"`
	ID               string `json:"id"`
	Source           string `json:"source"`
	ReadOnly         bool   `json:"readOnly"`
	UpdatedAt        string `json:"updatedAt"`
	Directory        string `json:"directory"`
	HasMapGen        bool   `json:"hasMapGen"`
	HasMapSettings   bool   `json:"hasMapSettings"`
	MapGenBytes      int64  `json:"mapGenBytes"`
	MapSettingsBytes int64  `json:"mapSettingsBytes"`
}

type profileDocument struct {
	Name        string          `json:"name"`
	ID          string          `json:"id"`
	Source      string          `json:"source"`
	ReadOnly    bool            `json:"readOnly"`
	UpdatedAt   string          `json:"updatedAt"`
	Directory   string          `json:"directory"`
	MapGen      json.RawMessage `json:"mapGen"`
	MapSettings json.RawMessage `json:"mapSettings"`
}

var profileNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ._-]{0,63}$`)

const (
	profileSourceDefault = "default"
	profileSourceCustom  = "custom"
)

type profileRef struct {
	Source string
	Name   string
}

func (r profileRef) id() string {
	return r.Source + ":" + r.Name
}

func (r profileRef) readOnly() bool {
	return r.Source == profileSourceDefault
}

func main() {
	addr := flag.String("addr", ":8080", "HTTP listen address")
	customPresetDir := "presets"
	defaultPresetDir := "default-presets"
	flag.StringVar(&customPresetDir, "presets", customPresetDir, "server-side custom map preset directory")
	flag.StringVar(&customPresetDir, "custom-presets", customPresetDir, "server-side custom map preset directory")
	flag.StringVar(&customPresetDir, "data", customPresetDir, "deprecated alias for -presets")
	flag.StringVar(&defaultPresetDir, "default-presets", defaultPresetDir, "read-only default map preset directory")
	factorioBin := flag.String("factorio-bin", "", "optional path to a Factorio/headless binary for map previews")
	factorioDir := flag.String("factorio-dir", "tools/factorio", "directory used to discover Factorio headless")
	previewTimeout := flag.Duration("preview-timeout", 60*time.Second, "maximum time allowed for one map preview render")
	previewQueueSize := flag.Int("preview-queue", 8, "maximum number of queued map preview jobs")
	authDB := flag.String("auth-db", "factmapgen-auth.db", "SQLite database path for users, sessions, and audit logs")
	flag.Parse()

	if *factorioBin == "" {
		*factorioBin = discoverFactorioBin(*factorioDir)
	} else {
		*factorioBin = normalizeFactorioBin(*factorioBin)
	}
	if *factorioBin != "" {
		if _, err := os.Stat(*factorioBin); err != nil {
			log.Printf("Ignoring unavailable Factorio binary %q: %v", *factorioBin, err)
			*factorioBin = ""
		}
	}

	st := &store{defaultRoot: defaultPresetDir, customRoot: customPresetDir}
	if err := st.ensure(); err != nil {
		log.Fatalf("prepare preset directories: %v", err)
	}
	if *previewQueueSize < 1 {
		log.Fatalf("preview queue size must be at least 1")
	}
	auth, initialAdminPassword, err := openAuthStore(*authDB)
	if err != nil {
		log.Fatalf("prepare auth database: %v", err)
	}
	defer auth.close()

	uiFS, err := fs.Sub(embedded, "web")
	if err != nil {
		log.Fatalf("prepare embedded UI: %v", err)
	}

	p := &previewer{
		factorioBin: *factorioBin,
		timeout:     *previewTimeout,
	}
	factorio := newFactorioManager(*factorioDir, p, factorioInstallIsManaged(*factorioDir, *factorioBin))

	srv := &server{
		store:        st,
		previewer:    p,
		previewQueue: newPreviewQueue(*previewQueueSize),
		auth:         auth,
		factorio:     factorio,
		config: appConfig{
			PresetDir:        customPresetDir,
			DefaultPresetDir: defaultPresetDir,
			CustomPresetDir:  customPresetDir,
			PreviewEnabled:   p.factorioBin != "",
			FactorioBin:      *factorioBin,
		},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/config", srv.handleConfig)
	mux.HandleFunc("/api/session", srv.handleSession)
	mux.HandleFunc("/api/users", srv.handleUsers)
	mux.HandleFunc("/api/users/", srv.handleUser)
	mux.HandleFunc("/api/audit", srv.handleAudit)
	mux.HandleFunc("/api/factorio", srv.handleFactorio)
	mux.HandleFunc("/api/factorio/install", srv.handleFactorioInstall)
	mux.HandleFunc("/api/profiles", srv.handleProfiles)
	mux.HandleFunc("/api/profiles/", srv.handleProfile)
	mux.HandleFunc("/api/previews/", srv.handlePreviewImage)
	mux.Handle("/", http.FileServer(http.FS(uiFS)))

	httpServer := &http.Server{
		Addr:              *addr,
		Handler:           requestLogger(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("FactMapGen listening on %s", listenURL(*addr))
	log.Printf("Default preset directory: %s", st.defaultRoot)
	log.Printf("Custom preset directory: %s", st.customRoot)
	log.Printf("Auth database: %s", *authDB)
	if initialAdminPassword != "" {
		log.Printf("Created initial admin login: username=admin password=%s", initialAdminPassword)
	}
	if p.factorioBin != "" {
		log.Printf("Factorio preview binary: %s", p.factorioBin)
	} else {
		log.Printf("Factorio preview binary not configured; run scripts/install-factorio-headless.sh or start with -factorio-bin to enable previews")
	}
	if err := httpServer.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func listenURL(addr string) string {
	if strings.HasPrefix(addr, ":") {
		return "http://localhost" + addr
	}
	if strings.HasPrefix(addr, "0.0.0.0:") {
		return "http://localhost:" + strings.TrimPrefix(addr, "0.0.0.0:")
	}
	return "http://" + addr
}

func discoverFactorioBin(factorioDir string) string {
	if factorioDir != "" {
		candidate := filepath.Join(factorioDir, "bin", "x64", "factorio")
		if _, err := os.Stat(candidate); err == nil {
			return normalizeFactorioBin(candidate)
		}
	}
	if candidate, err := exec.LookPath("factorio"); err == nil {
		return normalizeFactorioBin(candidate)
	}
	return ""
}

func normalizeFactorioBin(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if found, err := exec.LookPath(path); err == nil {
		path = found
	}
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}

func factorioRootFromBin(path string) string {
	path = filepath.Clean(path)
	if filepath.Base(path) != "factorio" {
		return ""
	}
	x64Dir := filepath.Dir(path)
	binDir := filepath.Dir(x64Dir)
	if filepath.Base(x64Dir) == "x64" && filepath.Base(binDir) == "bin" {
		return filepath.Dir(binDir)
	}
	return ""
}

func clippedOutput(output string, limit int) string {
	output = strings.TrimSpace(output)
	if len(output) <= limit {
		return output
	}
	return output[:limit] + "\n... output clipped ..."
}

func factorioErrorSummary(output string) string {
	lines := strings.Split(output, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if strings.Contains(line, " Error ") ||
			strings.Contains(lower, "error:") ||
			strings.Contains(lower, "filesystem error") ||
			strings.Contains(lower, "no such file") {
			return clippedOutput(line, 900)
		}
	}
	return clippedOutput(output, 900)
}

func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
	})
}

func (s *server) handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	cfg := s.config
	if s.factorio != nil {
		cfg.Factorio = s.factorio.status(r.Context(), true)
		cfg.PreviewEnabled = cfg.Factorio.PreviewEnabled
		cfg.FactorioBin = cfg.Factorio.Bin
	} else if s.previewer != nil {
		cfg.FactorioBin = s.previewer.factorioBinary()
		cfg.PreviewEnabled = cfg.FactorioBin != ""
	}
	writeJSON(w, http.StatusOK, cfg)
}

func (s *server) handleFactorio(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	if s.factorio == nil {
		writeError(w, http.StatusFailedDependency, "Factorio install manager is not configured")
		return
	}
	writeJSON(w, http.StatusOK, s.factorio.status(r.Context(), true))
}

func (s *server) handleFactorioInstall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	actor, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	if s.factorio == nil {
		writeError(w, http.StatusFailedDependency, "Factorio install manager is not configured")
		return
	}
	status, err := s.factorio.installFresh(r.Context())
	if err != nil {
		writeFactorioError(w, err)
		return
	}
	s.auth.logAudit(actor, "install", "factorio", status.Version, auditDetail(map[string]any{"latest": status.LatestVersion, "installDir": status.InstallDir}))
	writeJSON(w, http.StatusOK, status)
}

func (s *server) handleProfiles(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		profiles, err := s.store.listProfiles()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"profiles": profiles})
	case http.MethodPost:
		actor, ok := s.requireUser(w, r)
		if !ok {
			return
		}
		var req createProfileRequest
		if err := decodeJSONRequest(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		doc, err := s.store.createProfile(req.Name, req.Preset)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		s.auth.logAudit(actor, "create", "profile", doc.ID, auditDetail(map[string]any{"preset": req.Preset}))
		writeJSON(w, http.StatusCreated, doc)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *server) handleProfile(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/profiles/")
	if rest == "" {
		writeError(w, http.StatusNotFound, "profile not found")
		return
	}

	parts := strings.Split(rest, "/")
	name, err := url.PathUnescape(parts[0])
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid profile name")
		return
	}

	if len(parts) == 2 && parts[1] == "preview" {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		user, _ := s.currentUser(r)
		var req previewRequest
		if err := decodeJSONRequest(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		resp, err := s.renderPreview(r.Context(), name, req, previewPriorityForUser(user))
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}

	if len(parts) == 2 && parts[1] == "download.zip" {
		switch r.Method {
		case http.MethodGet:
			if err := s.store.writeProfileZip(w, name); err != nil {
				writeStoreError(w, err)
			}
		case http.MethodPost:
			var req downloadProfileRequest
			if err := decodeJSONRequest(r, &req); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			if err := s.store.writeProfileZipFromRequest(w, name, req); err != nil {
				writeStoreError(w, err)
			}
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}

	if len(parts) == 2 && parts[1] == "duplicate" {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		actor, ok := s.requireUser(w, r)
		if !ok {
			return
		}
		var req duplicateProfileRequest
		if err := decodeJSONRequest(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		doc, err := s.store.duplicateProfile(name, req.Name)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		s.auth.logAudit(actor, "duplicate", "profile", doc.ID, auditDetail(map[string]any{"source": name}))
		writeJSON(w, http.StatusCreated, doc)
		return
	}

	if len(parts) != 1 {
		writeError(w, http.StatusNotFound, "profile route not found")
		return
	}

	switch r.Method {
	case http.MethodGet:
		doc, err := s.store.readProfile(name)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, doc)
	case http.MethodPut:
		actor, ok := s.requireUser(w, r)
		if !ok {
			return
		}
		var req saveProfileRequest
		if err := decodeJSONRequest(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		doc, err := s.store.saveProfile(name, req.MapGen, req.MapSettings)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		s.auth.logAudit(actor, "update", "profile", doc.ID, "saved map-gen-settings.json and map-settings.json")
		writeJSON(w, http.StatusOK, doc)
	case http.MethodDelete:
		actor, ok := s.requireUser(w, r)
		if !ok {
			return
		}
		if err := s.store.deleteProfile(name); err != nil {
			writeStoreError(w, err)
			return
		}
		s.auth.logAudit(actor, "delete", "profile", name, "")
		w.WriteHeader(http.StatusNoContent)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func decodeJSONRequest(r *http.Request, out any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, 4<<20))
	dec.UseNumber()
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("decode JSON body: %w", err)
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		return errors.New("request body must contain one JSON document")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("write JSON response: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{
		"error": message,
	})
}

func writeStoreError(w http.ResponseWriter, err error) {
	var status int
	switch {
	case errors.Is(err, errInvalidProfileName):
		status = http.StatusBadRequest
	case errors.Is(err, errProfileExists):
		status = http.StatusConflict
	case errors.Is(err, errProfileNotFound):
		status = http.StatusNotFound
	case errors.Is(err, errReadOnlyProfile):
		status = http.StatusForbidden
	case errors.Is(err, errPreviewUnavailable):
		status = http.StatusFailedDependency
	case errors.Is(err, errPreviewQueueFull):
		status = http.StatusTooManyRequests
	default:
		status = http.StatusInternalServerError
	}
	writeError(w, status, err.Error())
}

var (
	errInvalidProfileName = errors.New("profile names must be 1-64 characters and use letters, numbers, spaces, dots, underscores, or hyphens")
	errProfileExists      = errors.New("profile already exists")
	errProfileNotFound    = errors.New("profile not found")
	errReadOnlyProfile    = errors.New("default presets are read-only; duplicate this preset before saving changes")
	errPreviewUnavailable = errors.New("Factorio preview is not configured")
)

func (s *store) ensure() error {
	if sameDirectory(s.defaultRoot, s.customRoot) {
		return errors.New("default and custom preset directories must be different")
	}
	if err := os.MkdirAll(s.defaultRoot, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(s.customRoot, 0o755); err != nil {
		return err
	}
	return s.seedDefaultProfiles()
}

func sameDirectory(a, b string) bool {
	absA, errA := filepath.Abs(filepath.Clean(a))
	absB, errB := filepath.Abs(filepath.Clean(b))
	if errA == nil && errB == nil {
		return absA == absB
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

func (s *store) listProfiles() ([]profileSummary, error) {
	if err := s.ensure(); err != nil {
		return nil, err
	}

	profiles := []profileSummary{}
	for _, source := range []string{profileSourceDefault, profileSourceCustom} {
		root := s.rootForSource(source)
		entries, err := os.ReadDir(root)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			ref := profileRef{Source: source, Name: entry.Name()}
			if err := validateProfileName(ref.Name); err != nil {
				continue
			}
			summary, err := s.profileSummary(ref)
			if err != nil {
				return nil, err
			}
			profiles = append(profiles, summary)
		}
	}
	sort.Slice(profiles, func(i, j int) bool {
		if profiles[i].Name == profiles[j].Name {
			return profiles[i].Source < profiles[j].Source
		}
		return profiles[i].Name < profiles[j].Name
	})

	return profiles, nil
}

func (s *store) createProfile(name, preset string) (profileDocument, error) {
	name = strings.TrimSpace(name)
	if err := validateProfileName(name); err != nil {
		return profileDocument{}, err
	}
	ref := profileRef{Source: profileSourceCustom, Name: name}
	if _, err := os.Stat(s.profileDir(ref)); err == nil {
		return profileDocument{}, errProfileExists
	} else if !errors.Is(err, os.ErrNotExist) {
		return profileDocument{}, err
	}

	mapGen, mapSettings, err := presetDocuments(preset)
	if err != nil {
		return profileDocument{}, err
	}
	return s.saveProfile(name, mapGen, mapSettings)
}

func (s *store) duplicateProfile(sourceName, targetName string) (profileDocument, error) {
	source, err := s.readProfile(sourceName)
	if err != nil {
		return profileDocument{}, err
	}
	targetName = strings.TrimSpace(targetName)
	if err := validateProfileName(targetName); err != nil {
		return profileDocument{}, err
	}
	target := profileRef{Source: profileSourceCustom, Name: targetName}
	if _, err := os.Stat(s.profileDir(target)); err == nil {
		return profileDocument{}, errProfileExists
	} else if !errors.Is(err, os.ErrNotExist) {
		return profileDocument{}, err
	}
	return s.saveProfile(targetName, source.MapGen, source.MapSettings)
}

func (s *store) readProfile(identifier string) (profileDocument, error) {
	ref, err := s.resolveProfile(identifier)
	if err != nil {
		return profileDocument{}, err
	}
	dir := s.profileDir(ref)

	mapGen, err := readNormalizedJSON(filepath.Join(dir, mapGenFile))
	if err != nil {
		return profileDocument{}, fmt.Errorf("%s: %w", mapGenFile, err)
	}
	mapSettings, err := readNormalizedJSON(filepath.Join(dir, mapSettingsFile))
	if err != nil {
		return profileDocument{}, fmt.Errorf("%s: %w", mapSettingsFile, err)
	}
	summary, err := s.profileSummary(ref)
	if err != nil {
		return profileDocument{}, err
	}

	return profileDocument{
		Name:        ref.Name,
		ID:          ref.id(),
		Source:      ref.Source,
		ReadOnly:    ref.readOnly(),
		UpdatedAt:   summary.UpdatedAt,
		Directory:   filepath.ToSlash(dir),
		MapGen:      mapGen,
		MapSettings: mapSettings,
	}, nil
}

func (s *store) saveProfile(identifier string, mapGen, mapSettings json.RawMessage) (profileDocument, error) {
	ref, err := writableProfileRef(identifier)
	if err != nil {
		return profileDocument{}, err
	}
	if ref.readOnly() {
		return profileDocument{}, errReadOnlyProfile
	}

	normalizedMapGen, err := normalizeJSON(mapGen)
	if err != nil {
		return profileDocument{}, fmt.Errorf("%s is invalid JSON: %w", mapGenFile, err)
	}
	normalizedMapSettings, err := normalizeJSON(mapSettings)
	if err != nil {
		return profileDocument{}, fmt.Errorf("%s is invalid JSON: %w", mapSettingsFile, err)
	}

	if err := s.writeProfileFiles(ref, normalizedMapGen, normalizedMapSettings); err != nil {
		return profileDocument{}, err
	}
	return s.readProfile(ref.id())
}

func (s *store) writeProfileFiles(ref profileRef, mapGen, mapSettings []byte) error {
	dir := s.profileDir(ref)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := writeFileAtomic(filepath.Join(dir, mapGenFile), append(mapGen, '\n')); err != nil {
		return err
	}
	if err := writeFileAtomic(filepath.Join(dir, mapSettingsFile), append(mapSettings, '\n')); err != nil {
		return err
	}
	return nil
}

func (s *store) deleteProfile(identifier string) error {
	ref, err := writableProfileRef(identifier)
	if err != nil {
		return err
	}
	if ref.readOnly() {
		return errReadOnlyProfile
	}
	dir := s.profileDir(ref)
	info, err := os.Stat(dir)
	if errors.Is(err, os.ErrNotExist) || (err == nil && !info.IsDir()) {
		return errProfileNotFound
	}
	if err != nil {
		return err
	}
	return os.RemoveAll(dir)
}

func (s *store) writeProfileZip(w http.ResponseWriter, identifier string) error {
	ref, err := s.resolveProfile(identifier)
	if err != nil {
		return err
	}
	dir := s.profileDir(ref)
	files := map[string][]byte{}
	for _, filename := range []string{mapGenFile, mapSettingsFile} {
		body, err := os.ReadFile(filepath.Join(dir, filename))
		if err != nil {
			return err
		}
		files[filename] = body
	}
	return writeZipFiles(w, ref.Name, files)
}

func (s *store) writeProfileZipFromRequest(w http.ResponseWriter, identifier string, req downloadProfileRequest) error {
	ref, err := s.resolveProfile(identifier)
	if err != nil {
		return err
	}
	mapGen, err := normalizeJSON(req.MapGen)
	if err != nil {
		return fmt.Errorf("%s is invalid JSON: %w", mapGenFile, err)
	}
	mapSettings, err := normalizeJSON(req.MapSettings)
	if err != nil {
		return fmt.Errorf("%s is invalid JSON: %w", mapSettingsFile, err)
	}
	return writeZipFiles(w, ref.Name, map[string][]byte{
		mapGenFile:      append(mapGen, '\n'),
		mapSettingsFile: append(mapSettings, '\n'),
	})
}

func writeZipFiles(w http.ResponseWriter, profileName string, files map[string][]byte) error {
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, downloadZipFilename(profileName)))
	zw := zip.NewWriter(w)
	for _, filename := range []string{mapGenFile, mapSettingsFile} {
		fw, err := zw.Create(filename)
		if err != nil {
			_ = zw.Close()
			return err
		}
		if _, err := fw.Write(files[filename]); err != nil {
			_ = zw.Close()
			return err
		}
	}
	return zw.Close()
}

func downloadZipFilename(profileName string) string {
	base := strings.TrimSpace(profileName)
	var b strings.Builder
	for _, r := range base {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '.', r == '_', r == '-':
			b.WriteRune(r)
		case r == ' ':
			b.WriteByte('-')
		}
	}
	if b.Len() == 0 {
		b.WriteString("preset")
	}
	return fmt.Sprintf("%s-%s.zip", b.String(), time.Now().Format("0102-150405"))
}

func (s *store) rootForSource(source string) string {
	if source == profileSourceDefault {
		return s.defaultRoot
	}
	return s.customRoot
}

func (s *store) profileDir(ref profileRef) string {
	return filepath.Join(s.rootForSource(ref.Source), ref.Name)
}

func (s *store) resolveProfile(identifier string) (profileRef, error) {
	ref, explicit, err := parseProfileIdentifier(identifier)
	if err != nil {
		return profileRef{}, err
	}
	if explicit {
		if err := s.ensureProfileExists(ref); err != nil {
			return profileRef{}, err
		}
		return ref, nil
	}

	for _, source := range []string{profileSourceCustom, profileSourceDefault} {
		candidate := profileRef{Source: source, Name: ref.Name}
		if err := s.ensureProfileExists(candidate); err == nil {
			return candidate, nil
		} else if !errors.Is(err, errProfileNotFound) {
			return profileRef{}, err
		}
	}
	return profileRef{}, errProfileNotFound
}

func (s *store) ensureProfileExists(ref profileRef) error {
	info, err := os.Stat(s.profileDir(ref))
	if errors.Is(err, os.ErrNotExist) || (err == nil && !info.IsDir()) {
		return errProfileNotFound
	}
	return err
}

func parseProfileIdentifier(identifier string) (profileRef, bool, error) {
	identifier = strings.TrimSpace(identifier)
	for _, source := range []string{profileSourceDefault, profileSourceCustom} {
		prefix := source + ":"
		if strings.HasPrefix(identifier, prefix) {
			name := strings.TrimSpace(strings.TrimPrefix(identifier, prefix))
			if err := validateProfileName(name); err != nil {
				return profileRef{}, false, err
			}
			return profileRef{Source: source, Name: name}, true, nil
		}
	}
	if err := validateProfileName(identifier); err != nil {
		return profileRef{}, false, err
	}
	return profileRef{Source: profileSourceCustom, Name: identifier}, false, nil
}

func writableProfileRef(identifier string) (profileRef, error) {
	ref, explicit, err := parseProfileIdentifier(identifier)
	if err != nil {
		return profileRef{}, err
	}
	if explicit && ref.Source == profileSourceDefault {
		return profileRef{}, errReadOnlyProfile
	}
	ref.Source = profileSourceCustom
	return ref, nil
}

var bundledPresetProfiles = []struct {
	Name string
	Key  string
}{
	{Name: "Default", Key: "default"},
	{Name: "No-Biters", Key: "no-biters"},
	{Name: "Railworld", Key: "rail-world"},
	{Name: "Deathworld", Key: "death-world"},
	{Name: "Rich-Peaceful", Key: "peaceful-rich"},
	{Name: "Island", Key: "island"},
	{Name: "Ribbon-World", Key: "ribbon-world"},
	{Name: "Empty-Sandbox", Key: "empty-sandbox"},
	{Name: "Marathon-Frontier", Key: "marathon-frontier"},
	{Name: "Dense-Forest", Key: "dense-forest"},
	{Name: "Desert-Scarcity", Key: "desert-scarcity"},
	{Name: "Cliffside-Lakes", Key: "cliffside-lakes"},
	{Name: "Oil-Baron", Key: "oil-baron"},
	{Name: "Tiny-Death-Spiral", Key: "tiny-death-spiral"},
}

func (s *store) seedDefaultProfiles() error {
	for _, preset := range bundledPresetProfiles {
		ref := profileRef{Source: profileSourceDefault, Name: preset.Name}
		if _, err := os.Stat(s.profileDir(ref)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		mapGen, mapSettings, err := presetDocuments(preset.Key)
		if err != nil {
			return err
		}
		if err := s.writeProfileFiles(ref, mapGen, mapSettings); err != nil {
			return err
		}
	}
	return nil
}

func previewPriorityForUser(user *authUser) int {
	if user == nil {
		return previewPriorityGuest
	}
	if user.IsAdmin {
		return previewPriorityAdmin
	}
	return previewPriorityUser
}

func (s *server) renderPreview(ctx context.Context, name string, req previewRequest, priority int) (previewResponse, error) {
	if s.previewer == nil || s.previewer.factorioBinary() == "" {
		return previewResponse{}, errPreviewUnavailable
	}
	run := func(ctx context.Context) (previewResponse, error) {
		return s.renderPreviewNow(ctx, name, req)
	}
	if s.previewQueue == nil {
		return run(ctx)
	}
	return s.previewQueue.submit(ctx, priority, run)
}

func (s *server) renderPreviewNow(ctx context.Context, name string, req previewRequest) (previewResponse, error) {
	ref, err := s.store.resolveProfile(name)
	if err != nil {
		return previewResponse{}, err
	}
	mapGenPath, cleanup, err := s.previewMapGenPath(ref, req)
	if err != nil {
		return previewResponse{}, err
	}
	defer cleanup()
	return s.previewer.render(ctx, ref, mapGenPath, req)
}

func (s *server) previewMapGenPath(ref profileRef, req previewRequest) (string, func(), error) {
	cleanup := func() {}
	if len(bytes.TrimSpace(req.MapGen)) == 0 {
		return absolutePath(filepath.Join(s.store.profileDir(ref), mapGenFile)), cleanup, nil
	}

	normalized, err := normalizePreviewMapGen(req.MapGen)
	if err != nil {
		return "", cleanup, fmt.Errorf("%s is invalid JSON: %w", mapGenFile, err)
	}

	tmp, err := os.CreateTemp("", "factmapgen-map-gen-*.json")
	if err != nil {
		return "", cleanup, err
	}
	tmpPath := tmp.Name()
	cleanup = func() { _ = os.Remove(tmpPath) }

	if _, err := tmp.Write(append(normalized, '\n')); err != nil {
		_ = tmp.Close()
		cleanup()
		return "", func() {}, err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return "", func() {}, err
	}
	return absolutePath(tmpPath), cleanup, nil
}

func (s *server) handlePreviewImage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.previewer == nil {
		writeStoreError(w, errPreviewUnavailable)
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/api/previews/")
	img, ok := s.previewer.getPreviewImage(name)
	if !ok {
		writeError(w, http.StatusNotFound, "preview not found")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="%s"`, name))
	w.Header().Set("Content-Type", img.contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(img.data)))
	_, _ = w.Write(img.data)
}

func (p *previewer) render(ctx context.Context, ref profileRef, mapGenPath string, req previewRequest) (previewResponse, error) {
	factorioBin := p.factorioBinary()
	if factorioBin == "" {
		return previewResponse{}, errPreviewUnavailable
	}
	if _, err := os.Stat(factorioBin); err != nil {
		return previewResponse{}, fmt.Errorf("%w: %s", errPreviewUnavailable, err)
	}

	size := req.Size
	if size == 0 {
		size = 768
	}
	if size < 256 || size > maxPreviewOutputSize {
		return previewResponse{}, fmt.Errorf("preview size must be between 256 and %d pixels", maxPreviewOutputSize)
	}
	planet := strings.TrimSpace(req.Planet)
	if planet == "" {
		planet = "nauvis"
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`).MatchString(planet) {
		return previewResponse{}, errors.New("preview planet must use letters, numbers, underscores, or hyphens")
	}
	zoom, err := previewZoomSpec(req.Zoom, size)
	if err != nil {
		return previewResponse{}, err
	}

	tmp, err := os.CreateTemp("", "factmapgen-preview-*.png")
	if err != nil {
		return previewResponse{}, err
	}
	tmpPath := tmp.Name()
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return previewResponse{}, err
	}
	if err := os.Remove(tmpPath); err != nil {
		return previewResponse{}, err
	}
	defer os.Remove(tmpPath)
	runCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	args := []string{
		"--generate-map-preview", tmpPath,
		"--map-gen-settings", mapGenPath,
		"--map-preview-size", strconv.Itoa(zoom.renderSize),
		"--map-preview-planet", planet,
	}
	if seed := strings.TrimSpace(req.Seed); seed != "" {
		if !regexp.MustCompile(`^[0-9]+$`).MatchString(seed) {
			return previewResponse{}, errors.New("preview seed override must be an unsigned integer")
		}
		args = append(args, "--map-gen-seed", seed)
	}

	cmd := exec.CommandContext(runCtx, factorioBin, args...)
	if root := factorioRootFromBin(factorioBin); root != "" {
		cmd.Dir = root
	}

	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		factorioOutput := output.String()
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return previewResponse{}, fmt.Errorf("Factorio preview timed out after %s", p.timeout)
		}
		log.Printf("Factorio preview failed for %q: %v\n%s", ref.id(), err, clippedOutput(factorioOutput, 12000))
		return previewResponse{}, fmt.Errorf("Factorio preview failed: %w: %s", err, factorioErrorSummary(factorioOutput))
	}

	data, contentType, ext, err := previewImageBytes(runCtx, tmpPath, req.Lossless, zoom, size)
	if err != nil {
		return previewResponse{}, err
	}
	previewName, err := p.storePreviewImage(data, contentType, ext)
	if err != nil {
		return previewResponse{}, err
	}

	generatedAt := time.Now().UTC().Format(time.RFC3339)
	return previewResponse{
		URL:         "/api/previews/" + url.PathEscape(previewName) + "?ts=" + url.QueryEscape(generatedAt),
		GeneratedAt: generatedAt,
		Size:        size,
		Planet:      planet,
		Output:      clippedOutput(output.String(), 6000),
	}, nil
}

type previewZoom struct {
	mode       string
	factor     int
	renderSize int
}

func previewZoomSpec(value string, outputSize int) (previewZoom, error) {
	if value == "" || value == "1" || value == "1x" {
		return previewZoom{mode: "normal", factor: 1, renderSize: outputSize}, nil
	}
	parts := strings.Split(value, "-")
	if len(parts) != 2 || parts[0] != "out" && parts[0] != "in" {
		return previewZoom{}, errors.New("preview zoom must be 1x, out-2, out-3, out-4, in-2, in-3, or in-4")
	}
	factor, err := strconv.Atoi(parts[1])
	if err != nil || factor < 2 || factor > 4 {
		return previewZoom{}, errors.New("preview zoom factor must be 2, 3, or 4")
	}
	if parts[0] == "out" {
		renderSize := outputSize * factor
		if renderSize > maxFactorioPreviewSize {
			return previewZoom{}, fmt.Errorf("preview zoom out %dx requires a %d pixel render; choose a smaller preview size", factor, renderSize)
		}
		return previewZoom{mode: "out", factor: factor, renderSize: renderSize}, nil
	}
	if outputSize%factor != 0 {
		return previewZoom{}, fmt.Errorf("preview zoom in %dx requires an output size divisible by %d for integer scaling", factor, factor)
	}
	return previewZoom{mode: "in", factor: factor, renderSize: outputSize}, nil
}

func previewImageBytes(ctx context.Context, srcPath string, lossless bool, zoom previewZoom, outputSize int) ([]byte, string, string, error) {
	if lossless && zoom.mode == "normal" {
		data, err := os.ReadFile(srcPath)
		if err != nil {
			return nil, "", "", err
		}
		return data, "image/png", ".png", nil
	}
	img, err := transformedPreviewImage(srcPath, zoom, outputSize)
	if err != nil {
		return nil, "", "", err
	}
	if lossless {
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			return nil, "", "", err
		}
		return buf.Bytes(), "image/png", ".png", nil
	}
	data, err := encodeAVIFImage(ctx, img)
	if err == nil {
		return data, "image/avif", ".avif", nil
	}
	log.Printf("AVIF preview encode failed, falling back to JPEG: %v", err)
	data, err = encodeJPEGImage(img, previewJPEGQuality)
	if err != nil {
		return nil, "", "", err
	}
	return data, "image/jpeg", ".jpg", nil
}

func transformedPreviewImage(srcPath string, zoom previewZoom, outputSize int) (image.Image, error) {
	src, err := os.Open(srcPath)
	if err != nil {
		return nil, err
	}
	defer src.Close()

	img, _, err := image.Decode(src)
	if err != nil {
		return nil, err
	}
	switch zoom.mode {
	case "normal":
		return img, nil
	case "out":
		return downscalePreviewImage(img, outputSize, zoom.factor), nil
	case "in":
		return integerZoomPreviewImage(img, outputSize, zoom.factor), nil
	default:
		return nil, errors.New("invalid preview zoom mode")
	}
}

func encodeAVIFImage(ctx context.Context, img image.Image) ([]byte, error) {
	var pngBytes bytes.Buffer
	if err := png.Encode(&pngBytes, img); err != nil {
		return nil, err
	}
	out, err := os.CreateTemp("", "factmapgen-preview-*.avif")
	if err != nil {
		return nil, err
	}
	outPath := out.Name()
	if err := out.Close(); err != nil {
		_ = os.Remove(outPath)
		return nil, err
	}
	defer os.Remove(outPath)

	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-hide_banner",
		"-loglevel", "error",
		"-y",
		"-f", "png_pipe",
		"-i", "pipe:0",
		"-frames:v", "1",
		"-c:v", "libaom-av1",
		"-still-picture", "1",
		"-crf", strconv.Itoa(previewAVIFCRF),
		"-cpu-used", strconv.Itoa(previewAVIFCPUUsed),
		outPath,
	)
	cmd.Stdin = &pngBytes
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("AVIF encode timed out: %w", ctx.Err())
		}
		return nil, fmt.Errorf("ffmpeg AVIF encode failed: %w: %s", err, clippedOutput(stderr.String(), 2000))
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, errors.New("ffmpeg AVIF encode produced no output")
	}
	return data, nil
}

func encodeJPEGImage(img image.Image, quality int) ([]byte, error) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func downscalePreviewImage(src image.Image, outputSize int, factor int) image.Image {
	bounds := src.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, outputSize, outputSize))
	count := uint32(factor * factor)
	for y := 0; y < outputSize; y++ {
		for x := 0; x < outputSize; x++ {
			var rr, gg, bb, aa uint32
			for yy := 0; yy < factor; yy++ {
				for xx := 0; xx < factor; xx++ {
					r, g, b, a := src.At(bounds.Min.X+x*factor+xx, bounds.Min.Y+y*factor+yy).RGBA()
					rr += r
					gg += g
					bb += b
					aa += a
				}
			}
			out.Set(x, y, color.RGBA{R: uint8((rr / count) >> 8), G: uint8((gg / count) >> 8), B: uint8((bb / count) >> 8), A: uint8((aa / count) >> 8)})
		}
	}
	return out
}

func integerZoomPreviewImage(src image.Image, outputSize int, factor int) image.Image {
	bounds := src.Bounds()
	cropSize := outputSize / factor
	cropMinX := bounds.Min.X + (bounds.Dx()-cropSize)/2
	cropMinY := bounds.Min.Y + (bounds.Dy()-cropSize)/2
	out := image.NewRGBA(image.Rect(0, 0, outputSize, outputSize))
	for y := 0; y < outputSize; y++ {
		for x := 0; x < outputSize; x++ {
			out.Set(x, y, src.At(cropMinX+x/factor, cropMinY+y/factor))
		}
	}
	return out
}

func (p *previewer) storePreviewImage(data []byte, contentType string, ext string) (string, error) {
	if ext != ".avif" && ext != ".jpg" && ext != ".png" {
		return "", errors.New("invalid preview image extension")
	}
	token, err := randomToken(18)
	if err != nil {
		return "", err
	}
	name := "preview-" + token + ext
	now := time.Now().UTC()

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.images == nil {
		p.images = map[string]previewImage{}
	}
	p.prunePreviewImagesLocked(now, maxPreviewImages-1)
	p.images[name] = previewImage{data: data, contentType: contentType, createdAt: now, expiresAt: now.Add(previewImageRetention)}
	return name, nil
}

func (p *previewer) prunePreviewImagesLocked(now time.Time, maxRemaining int) {
	for key, image := range p.images {
		if now.After(image.expiresAt) {
			delete(p.images, key)
		}
	}
	for len(p.images) > maxRemaining {
		var oldestKey string
		var oldestTime time.Time
		for key, image := range p.images {
			if oldestKey == "" || image.createdAt.Before(oldestTime) {
				oldestKey = key
				oldestTime = image.createdAt
			}
		}
		if oldestKey == "" {
			return
		}
		delete(p.images, oldestKey)
	}
}

func (p *previewer) getPreviewImage(name string) (previewImage, bool) {
	if !isPreviewImageName(name) {
		return previewImage{}, false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	img, ok := p.images[name]
	if !ok {
		return previewImage{}, false
	}
	if time.Now().UTC().After(img.expiresAt) {
		delete(p.images, name)
		return previewImage{}, false
	}
	return img, true
}

func isPreviewImageName(filename string) bool {
	if !strings.HasPrefix(filename, "preview-") {
		return false
	}
	stem := strings.TrimPrefix(filename, "preview-")
	switch {
	case strings.HasSuffix(stem, ".avif"):
		stem = strings.TrimSuffix(stem, ".avif")
	case strings.HasSuffix(stem, ".jpg"):
		stem = strings.TrimSuffix(stem, ".jpg")
	case strings.HasSuffix(stem, ".png"):
		stem = strings.TrimSuffix(stem, ".png")
	default:
		return false
	}
	if stem == "" || len(stem) > 128 {
		return false
	}
	for _, r := range stem {
		if r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func absolutePath(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}

func (s *store) profileSummary(ref profileRef) (profileSummary, error) {
	dir := s.profileDir(ref)
	dirInfo, err := os.Stat(dir)
	if err != nil {
		return profileSummary{}, err
	}

	summary := profileSummary{
		Name:      ref.Name,
		ID:        ref.id(),
		Source:    ref.Source,
		ReadOnly:  ref.readOnly(),
		UpdatedAt: dirInfo.ModTime().UTC().Format(time.RFC3339),
		Directory: filepath.ToSlash(dir),
	}

	for _, item := range []struct {
		filename string
		apply    func(os.FileInfo)
	}{
		{mapGenFile, func(info os.FileInfo) {
			summary.HasMapGen = true
			summary.MapGenBytes = info.Size()
		}},
		{mapSettingsFile, func(info os.FileInfo) {
			summary.HasMapSettings = true
			summary.MapSettingsBytes = info.Size()
		}},
	} {
		info, err := os.Stat(filepath.Join(dir, item.filename))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return profileSummary{}, err
		}
		item.apply(info)
		if info.ModTime().After(dirInfo.ModTime()) {
			summary.UpdatedAt = info.ModTime().UTC().Format(time.RFC3339)
		}
	}

	return summary, nil
}

func validateProfileName(name string) error {
	name = strings.TrimSpace(name)
	if name == "." || name == ".." || !profileNamePattern.MatchString(name) {
		return errInvalidProfileName
	}
	return nil
}

func readNormalizedJSON(path string) ([]byte, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return normalizeJSON(body)
}

func normalizeJSON(raw json.RawMessage) ([]byte, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, errors.New("empty JSON document")
	}

	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return nil, err
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		return nil, errors.New("contains more than one JSON document")
	}
	if _, ok := value.(map[string]any); !ok {
		return nil, errors.New("top-level JSON value must be an object")
	}
	return json.MarshalIndent(value, "", "  ")
}

func writeFileAtomic(path string, body []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func defaultTemplate(filename string) ([]byte, error) {
	return embedded.ReadFile(filepath.ToSlash(filepath.Join("templates", filename)))
}

func presetDocuments(preset string) ([]byte, []byte, error) {
	preset = strings.TrimSpace(strings.ToLower(preset))
	if preset == "" {
		preset = "default"
	}

	mapGenRaw, err := defaultTemplate("map-gen-settings.example.json")
	if err != nil {
		return nil, nil, err
	}
	mapSettingsRaw, err := defaultTemplate("map-settings.example.json")
	if err != nil {
		return nil, nil, err
	}

	var mapGen map[string]any
	if err := decodeObject(mapGenRaw, &mapGen); err != nil {
		return nil, nil, err
	}
	var mapSettings map[string]any
	if err := decodeObject(mapSettingsRaw, &mapSettings); err != nil {
		return nil, nil, err
	}

	switch preset {
	case "default":
	case "no-biters":
		applyNoBitersPreset(mapGen, mapSettings)
	case "rail-world":
		setAutoplace(mapGen, []string{"coal", "stone", "copper-ore", "iron-ore", "uranium-ore", "crude-oil"}, 0.45, 2.2, 1.4)
		setAutoplace(mapGen, []string{"water"}, 0.35, 0.65, 0)
		setAutoplace(mapGen, []string{"enemy-base"}, 0.5, 0.5, 0)
		mapGen["starting_area"] = 1.5
	case "death-world":
		mapGen["starting_area"] = 0.75
		mapGen["peaceful_mode"] = false
		setAutoplace(mapGen, []string{"enemy-base"}, 2.0, 2.0, 0)
		setNested(mapSettings, []string{"enemy_evolution", "time_factor"}, 0.00002)
		setNested(mapSettings, []string{"enemy_evolution", "destroy_factor"}, 0.003)
		setNested(mapSettings, []string{"enemy_evolution", "pollution_factor"}, 0.0000012)
		setNested(mapSettings, []string{"enemy_expansion", "min_expansion_cooldown"}, 3600)
		setNested(mapSettings, []string{"enemy_expansion", "max_expansion_cooldown"}, 72000)
	case "peaceful-rich":
		mapGen["starting_area"] = 2
		setAutoplace(mapGen, []string{"coal", "stone", "copper-ore", "iron-ore", "uranium-ore", "crude-oil"}, 1.4, 1.8, 3.0)
		applyNoBitersPreset(mapGen, mapSettings)
	case "island":
		mapGen["starting_area"] = 1.5
		setNested(mapGen, []string{"property_expression_names", "elevation"}, "elevation_island")
		setAutoplace(mapGen, []string{"water"}, 1.2, 1.8, 0)
	case "ribbon-world":
		mapGen["height"] = 128
		mapGen["width"] = 0
		mapGen["starting_area"] = 2
		setAutoplace(mapGen, []string{"coal", "stone", "copper-ore", "iron-ore", "uranium-ore", "crude-oil"}, 1, 1.8, 1.5)
	case "empty-sandbox":
		setAutoplace(mapGen, []string{"coal", "stone", "copper-ore", "iron-ore", "uranium-ore", "crude-oil", "water", "trees"}, 1, 0, 0)
		setNested(mapGen, []string{"cliff_settings", "richness"}, 0)
		setNested(mapSettings, []string{"pollution", "enabled"}, false)
		applyNoBitersPreset(mapGen, mapSettings)
	case "marathon-frontier":
		mapGen["starting_area"] = 0.85
		setAutoplace(mapGen, []string{"coal", "stone", "copper-ore", "iron-ore"}, 0.55, 1.25, 0.7)
		setAutoplace(mapGen, []string{"uranium-ore", "crude-oil"}, 0.45, 1.0, 0.6)
		setAutoplace(mapGen, []string{"enemy-base"}, 1.35, 1.2, 0)
		setNested(mapSettings, []string{"difficulty_settings", "technology_price_multiplier"}, 4)
		setNested(mapSettings, []string{"enemy_evolution", "time_factor"}, 0.000008)
		setNested(mapSettings, []string{"enemy_expansion", "min_expansion_cooldown"}, 7200)
	case "dense-forest":
		mapGen["starting_area"] = 1.25
		setAutoplace(mapGen, []string{"trees"}, 2.8, 2.5, 0)
		setAutoplace(mapGen, []string{"water"}, 1.25, 1.35, 0)
		setAutoplace(mapGen, []string{"coal", "stone", "copper-ore", "iron-ore", "uranium-ore", "crude-oil"}, 0.8, 0.9, 1.1)
		setNested(mapGen, []string{"cliff_settings", "richness"}, 2.2)
		setNested(mapGen, []string{"property_expression_names", "control:moisture:bias"}, "0.35")
		setNested(mapGen, []string{"property_expression_names", "control:moisture:frequency"}, "0.65")
	case "desert-scarcity":
		mapGen["starting_area"] = 0.75
		setAutoplace(mapGen, []string{"water"}, 0.25, 0.35, 0)
		setAutoplace(mapGen, []string{"trees"}, 0.12, 0.25, 0)
		setAutoplace(mapGen, []string{"coal", "stone", "copper-ore", "iron-ore"}, 0.45, 0.75, 0.8)
		setAutoplace(mapGen, []string{"uranium-ore", "crude-oil"}, 0.35, 0.55, 0.7)
		setNested(mapGen, []string{"property_expression_names", "control:moisture:bias"}, "-0.75")
		setNested(mapGen, []string{"property_expression_names", "control:aux:bias"}, "0.45")
		setNested(mapSettings, []string{"pollution", "ageing"}, 0.75)
	case "cliffside-lakes":
		mapGen["starting_area"] = 1.75
		setAutoplace(mapGen, []string{"water"}, 2.0, 1.8, 0)
		setAutoplace(mapGen, []string{"trees"}, 1.4, 1.2, 0)
		setAutoplace(mapGen, []string{"enemy-base"}, 0.8, 1.1, 0)
		setNested(mapGen, []string{"cliff_settings", "cliff_elevation_interval"}, 24)
		setNested(mapGen, []string{"cliff_settings", "richness"}, 3.5)
		setNested(mapGen, []string{"property_expression_names", "control:moisture:bias"}, "0.25")
	case "oil-baron":
		mapGen["starting_area"] = 1.2
		setAutoplace(mapGen, []string{"crude-oil"}, 2.5, 2.2, 4.0)
		setAutoplace(mapGen, []string{"coal"}, 0.55, 0.8, 0.8)
		setAutoplace(mapGen, []string{"stone", "copper-ore", "iron-ore"}, 0.85, 1.0, 1.0)
		setAutoplace(mapGen, []string{"uranium-ore"}, 0.7, 0.8, 1.3)
		setAutoplace(mapGen, []string{"enemy-base"}, 1.2, 1.1, 0)
		setNested(mapSettings, []string{"pollution", "diffusion_ratio"}, 0.035)
		setNested(mapSettings, []string{"enemy_evolution", "pollution_factor"}, 0.0000012)
	case "tiny-death-spiral":
		mapGen["width"] = 768
		mapGen["height"] = 768
		mapGen["starting_area"] = 0.55
		setAutoplace(mapGen, []string{"coal", "stone", "copper-ore", "iron-ore", "uranium-ore", "crude-oil"}, 1.35, 1.15, 1.4)
		setAutoplace(mapGen, []string{"water"}, 0.75, 0.9, 0)
		setAutoplace(mapGen, []string{"enemy-base"}, 2.5, 2.3, 0)
		setNested(mapSettings, []string{"enemy_evolution", "time_factor"}, 0.00002)
		setNested(mapSettings, []string{"enemy_evolution", "destroy_factor"}, 0.004)
		setNested(mapSettings, []string{"enemy_expansion", "min_expansion_cooldown"}, 1800)
		setNested(mapSettings, []string{"enemy_expansion", "max_expansion_cooldown"}, 18000)
	default:
		return nil, nil, fmt.Errorf("unknown preset %q", preset)
	}

	mapGenOut, err := json.MarshalIndent(mapGen, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	mapSettingsOut, err := json.MarshalIndent(mapSettings, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	return mapGenOut, mapSettingsOut, nil
}

func applyNoBitersPreset(mapGen, mapSettings map[string]any) {
	mapGen["peaceful_mode"] = true
	setAutoplace(mapGen, []string{"enemy-base"}, 0, 0, 0)
	setNested(mapSettings, []string{"enemy_evolution", "enabled"}, false)
	setNested(mapSettings, []string{"enemy_expansion", "enabled"}, false)
}

func normalizePreviewMapGen(raw json.RawMessage) ([]byte, error) {
	normalized, err := normalizeJSON(raw)
	if err != nil {
		return nil, err
	}
	var mapGen map[string]any
	if err := decodeObject(normalized, &mapGen); err != nil {
		return nil, err
	}
	sanitizePreviewAutoplaceFrequencies(mapGen)
	return json.MarshalIndent(mapGen, "", "  ")
}

func sanitizePreviewAutoplaceFrequencies(mapGen map[string]any) {
	controls, ok := mapGen["autoplace_controls"].(map[string]any)
	if !ok {
		return
	}
	for _, rawControl := range controls {
		control, ok := rawControl.(map[string]any)
		if !ok {
			continue
		}
		frequency, ok := jsonNumberFloat(control["frequency"])
		if !ok || frequency <= 0 {
			control["frequency"] = 0.1
		}
	}
}

func jsonNumberFloat(value any) (float64, bool) {
	switch v := value.(type) {
	case json.Number:
		n, err := v.Float64()
		return n, err == nil
	case float64:
		return v, true
	case int:
		return float64(v), true
	default:
		return 0, false
	}
}

func decodeObject(raw []byte, out any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(out); err != nil {
		return err
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		return errors.New("template contains more than one JSON document")
	}
	return nil
}

func setAutoplace(root map[string]any, names []string, frequency, size, richness float64) {
	controls := objectAt(root, "autoplace_controls")
	for _, name := range names {
		control := objectAt(controls, name)
		control["frequency"] = frequency
		control["size"] = size
		if _, ok := control["richness"]; ok || richness != 0 {
			control["richness"] = richness
		}
	}
}

func setNested(root map[string]any, path []string, value any) {
	if len(path) == 0 {
		return
	}
	current := root
	for _, key := range path[:len(path)-1] {
		current = objectAt(current, key)
	}
	current[path[len(path)-1]] = value
}

func objectAt(root map[string]any, key string) map[string]any {
	if existing, ok := root[key].(map[string]any); ok {
		return existing
	}
	next := make(map[string]any)
	root[key] = next
	return next
}
