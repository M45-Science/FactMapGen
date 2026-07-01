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
	"time"
)

const (
	mapGenFile      = "map-gen-settings.json"
	mapSettingsFile = "map-settings.json"
)

//go:embed web/* templates/*.json
var embedded embed.FS

type server struct {
	store     *store
	previewer *previewer
	auth      *authStore
	config    appConfig
}

type store struct {
	defaultRoot string
	customRoot  string
}

type previewer struct {
	factorioBin string
	outputRoot  string
	timeout     time.Duration
}

type appConfig struct {
	PresetDir        string `json:"presetDir"`
	DefaultPresetDir string `json:"defaultPresetDir"`
	CustomPresetDir  string `json:"customPresetDir"`
	PreviewDir       string `json:"previewDir"`
	PreviewEnabled   bool   `json:"previewEnabled"`
	FactorioBin      string `json:"factorioBin,omitempty"`
}

type createProfileRequest struct {
	Name   string `json:"name"`
	Preset string `json:"preset"`
}

type saveProfileRequest struct {
	MapGen      json.RawMessage `json:"mapGen"`
	MapSettings json.RawMessage `json:"mapSettings"`
}

type duplicateProfileRequest struct {
	Name string `json:"name"`
}

type previewRequest struct {
	Size   int             `json:"size"`
	Planet string          `json:"planet"`
	Seed   string          `json:"seed"`
	MapGen json.RawMessage `json:"mapGen"`
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
	previewDir := flag.String("previews", "previews", "server-side generated preview image directory")
	factorioBin := flag.String("factorio-bin", "", "optional path to a Factorio/headless binary for map previews")
	factorioDir := flag.String("factorio-dir", "tools/factorio", "directory used to discover Factorio headless")
	previewTimeout := flag.Duration("preview-timeout", 2*time.Minute, "maximum time allowed for one map preview render")
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
	if err := os.MkdirAll(*previewDir, 0o755); err != nil {
		log.Fatalf("prepare preview directory: %v", err)
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
		outputRoot:  *previewDir,
		timeout:     *previewTimeout,
	}

	srv := &server{
		store:     st,
		previewer: p,
		auth:      auth,
		config: appConfig{
			PresetDir:        customPresetDir,
			DefaultPresetDir: defaultPresetDir,
			CustomPresetDir:  customPresetDir,
			PreviewDir:       *previewDir,
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
	mux.HandleFunc("/api/profiles", srv.handleProfiles)
	mux.HandleFunc("/api/profiles/", srv.handleProfile)
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
	writeJSON(w, http.StatusOK, s.config)
}

func (s *server) handleProfiles(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireUser(w, r)
	if !ok {
		return
	}

	switch r.Method {
	case http.MethodGet:
		profiles, err := s.store.listProfiles()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"profiles": profiles})
	case http.MethodPost:
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

	actor, ok := s.requireUser(w, r)
	if !ok {
		return
	}

	if len(parts) == 2 && parts[1] == "preview" {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var req previewRequest
		if err := decodeJSONRequest(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		resp, err := s.renderPreview(r.Context(), name, req)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		s.auth.logAudit(actor, "preview", "profile", name, auditDetail(map[string]any{"size": resp.Size, "planet": resp.Planet}))
		writeJSON(w, http.StatusOK, resp)
		return
	}

	if len(parts) == 2 && parts[1] == "download.zip" {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if err := s.store.writeProfileZip(w, name); err != nil {
			writeStoreError(w, err)
		}
		return
	}

	if len(parts) == 2 && parts[1] == "preview.png" {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if err := s.servePreview(w, r, name); err != nil {
			writeStoreError(w, err)
		}
		return
	}

	if len(parts) == 2 && parts[1] == "duplicate" {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
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

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.zip"`, ref.Name))
	zw := zip.NewWriter(w)
	for _, filename := range []string{mapGenFile, mapSettingsFile} {
		body, err := os.ReadFile(filepath.Join(dir, filename))
		if err != nil {
			_ = zw.Close()
			return err
		}
		fw, err := zw.Create(filename)
		if err != nil {
			_ = zw.Close()
			return err
		}
		if _, err := fw.Write(body); err != nil {
			_ = zw.Close()
			return err
		}
	}
	return zw.Close()
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

func (s *server) renderPreview(ctx context.Context, name string, req previewRequest) (previewResponse, error) {
	if s.previewer == nil || s.previewer.factorioBin == "" {
		return previewResponse{}, errPreviewUnavailable
	}
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

	normalized, err := normalizeJSON(req.MapGen)
	if err != nil {
		return "", cleanup, fmt.Errorf("%s is invalid JSON: %w", mapGenFile, err)
	}

	tmpDir := filepath.Join(s.previewer.outputRoot, "_tmp")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return "", cleanup, err
	}
	tmp, err := os.CreateTemp(tmpDir, "map-gen-*.json")
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

func (s *server) servePreview(w http.ResponseWriter, r *http.Request, name string) error {
	if s.previewer == nil {
		return errPreviewUnavailable
	}
	ref, err := s.store.resolveProfile(name)
	if err != nil {
		return err
	}
	path := s.previewer.previewPath(ref)
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return errProfileNotFound
	} else if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, path)
	return nil
}

func (p *previewer) render(ctx context.Context, ref profileRef, mapGenPath string, req previewRequest) (previewResponse, error) {
	if p.factorioBin == "" {
		return previewResponse{}, errPreviewUnavailable
	}
	if _, err := os.Stat(p.factorioBin); err != nil {
		return previewResponse{}, fmt.Errorf("%w: %s", errPreviewUnavailable, err)
	}

	size := req.Size
	if size == 0 {
		size = 768
	}
	if size < 256 || size > 4096 {
		return previewResponse{}, errors.New("preview size must be between 256 and 4096 pixels")
	}
	planet := strings.TrimSpace(req.Planet)
	if planet == "" {
		planet = "nauvis"
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`).MatchString(planet) {
		return previewResponse{}, errors.New("preview planet must use letters, numbers, underscores, or hyphens")
	}

	outPath := absolutePath(p.previewPath(ref))
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return previewResponse{}, err
	}

	runCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	args := []string{
		"--generate-map-preview", outPath,
		"--map-gen-settings", mapGenPath,
		"--map-preview-size", strconv.Itoa(size),
		"--map-preview-planet", planet,
	}
	if seed := strings.TrimSpace(req.Seed); seed != "" {
		if !regexp.MustCompile(`^[0-9]+$`).MatchString(seed) {
			return previewResponse{}, errors.New("preview seed override must be an unsigned integer")
		}
		args = append(args, "--map-gen-seed", seed)
	}

	cmd := exec.CommandContext(runCtx, p.factorioBin, args...)
	if root := factorioRootFromBin(p.factorioBin); root != "" {
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

	generatedAt := time.Now().UTC().Format(time.RFC3339)
	return previewResponse{
		URL:         "/api/profiles/" + url.PathEscape(ref.id()) + "/preview.png?ts=" + url.QueryEscape(generatedAt),
		GeneratedAt: generatedAt,
		Size:        size,
		Planet:      planet,
		Output:      clippedOutput(output.String(), 6000),
	}, nil
}

func (p *previewer) previewPath(ref profileRef) string {
	return filepath.Join(p.outputRoot, ref.Source, ref.Name, "preview.png")
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
		setAutoplace(mapGen, []string{"coal", "stone", "copper-ore", "iron-ore", "uranium-ore", "crude-oil", "water", "trees", "enemy-base"}, 0, 0, 0)
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
