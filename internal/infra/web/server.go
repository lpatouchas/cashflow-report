package web

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lpatouchas/cashflow-report/internal/app/report"
	"github.com/lpatouchas/cashflow-report/internal/domain/transaction"
	"github.com/lpatouchas/cashflow-report/internal/infra/config"
	"github.com/lpatouchas/cashflow-report/internal/infra/csv"
	"github.com/lpatouchas/cashflow-report/internal/infra/html"
)

// maxUploadBytes caps the in-memory portion of a multipart upload.
const maxUploadBytes = 32 << 20 // 32 MiB

// Server serves the local web UI: upload CSVs, edit exclusion rules, generate
// and view the report.
type Server struct {
	configPath string
	mux        *http.ServeMux
}

// New builds a Server backed by the exclusion-rules file at configPath.
func New(configPath string) *Server {
	s := &Server{configPath: configPath, mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /", s.handleIndex)
	s.mux.HandleFunc("POST /generate", s.handleGenerate)
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) handleIndex(w http.ResponseWriter, _ *http.Request) {
	cfg, err := config.Load(s.configPath)
	if err != nil {
		http.Error(w, "Couldn't load exclusion rules: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	payload := struct {
		Reconcile reconcileView
		Rules     []ruleView
	}{toReconcileView(cfg.VisaReconcile), toRuleViews(cfg.Exclusions)}
	if err := indexTmpl.Execute(w, payload); err != nil {
		slog.Error("rendering index", "error", err)
	}
}

func (s *Server) handleGenerate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		http.Error(w, "Couldn't read the upload: "+err.Error(), http.StatusBadRequest)
		return
	}
	// Uploads above maxUploadBytes spill to disk; the stdlib leaves cleanup to us.
	defer func() { _ = r.MultipartForm.RemoveAll() }()
	files := r.MultipartForm.File["files"]
	if len(files) == 0 {
		http.Error(w, "Please upload at least one CSV file.", http.StatusBadRequest)
		return
	}

	specs, err := parseRules(r.MultipartForm)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	rc, err := parseReconcile(r.MultipartForm)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	cfg, err := config.Load(s.configPath)
	if err != nil {
		http.Error(w, "Couldn't load rules: "+err.Error(), http.StatusInternalServerError)
		return
	}
	cfg.Exclusions = specs
	cfg.VisaReconcile = rc
	if r.FormValue("save") != "" {
		if err := config.Save(s.configPath, cfg); err != nil {
			http.Error(w, "Couldn't save rules: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	tmpDir, err := os.MkdirTemp("", "pf-uploads-*")
	if err != nil {
		http.Error(w, "Server error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer os.RemoveAll(tmpDir)

	for _, fh := range files {
		if err := saveUpload(tmpDir, fh); err != nil {
			http.Error(w, "Couldn't save "+fh.Filename+": "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	var buf bytes.Buffer
	svc := report.NewService(csv.New(tmpDir), html.NewWriter(&buf), transaction.CompileRules(specs), cfg.VisaReconcile)
	if err := svc.GenerateReport(context.Background()); err != nil {
		http.Error(w, "Couldn't generate the report: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(buf.Bytes())
}

// saveUpload writes one uploaded file into dir under its base filename.
func saveUpload(dir string, fh *multipart.FileHeader) error {
	src, err := fh.Open()
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.Create(filepath.Join(dir, filepath.Base(fh.Filename)))
	if err != nil {
		return err
	}
	defer dst.Close()

	_, err = io.Copy(dst, src)
	return err
}

// browserURL derives a clickable localhost URL from a listen address such as
// ":8080" or "localhost:8080".
func browserURL(addr string) string {
	if strings.HasPrefix(addr, ":") {
		return "http://localhost" + addr
	}
	return "http://" + addr
}

// Run starts the HTTP server on addr, optionally opening the browser. It
// blocks until the server stops.
func (s *Server) Run(addr string, open bool) error {
	url := browserURL(addr)
	if open {
		go func() {
			time.Sleep(300 * time.Millisecond)
			_ = openBrowser(url)
		}()
	}
	slog.Info("serving", "url", url)
	return http.ListenAndServe(addr, s)
}
