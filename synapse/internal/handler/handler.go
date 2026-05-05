package handler

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"sync"

	"synapse/internal/db"
	synclib "synapse/internal/sync"
)

type Handler struct {
	db            *db.DB
	mu            sync.Mutex
	running       bool
	composePath   string
	kumaURL       string
	kumaUser      string
	kumaPass      string
	tmpl          *template.Template
	progressChans []chan sync.Progress
}

func New(database *db.DB, composePath, kumaURL, kumaUser, kumaPass string) *Handler {
	tmpl := template.Must(template.ParseGlob("web/templates/*.html"))
	return &Handler{
		db:          database,
		composePath: composePath,
		kumaURL:     kumaURL,
		kumaUser:    kumaUser,
		kumaPass:    kumaPass,
		tmpl:        tmpl,
	}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /", h.Index)
	mux.HandleFunc("GET /admin", h.Admin)
	mux.HandleFunc("POST /api/sync", h.StartSync)
	mux.HandleFunc("GET /api/sync/progress", h.ProgressSSE)
	mux.HandleFunc("GET /api/sync/history", h.SyncHistory)
	mux.HandleFunc("GET /api/monitors", h.MonitorList)
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("web/static"))))
}

func (h *Handler) Index(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/admin", http.StatusFound)
}

func (h *Handler) Admin(w http.ResponseWriter, r *http.Request) {
	run, _ := h.db.GetLatestSyncRun()
	count, _ := h.db.GetMonitorCount()

	h.tmpl.ExecuteTemplate(w, "admin", map[string]any{
		"Running":       h.running,
		"LatestRun":     run,
		"MonitorCount": count,
	})
}

func (h *Handler) StartSync(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	if h.running {
		h.mu.Unlock()
		http.Error(w, "sync already running", http.StatusConflict)
		return
	}
	h.running = true
	h.mu.Unlock()

	go func() {
		defer func() {
			h.mu.Lock()
			h.running = false
			h.progressChans = nil
			h.mu.Unlock()
		}()

		sync.RunSync(h.composePath, h.kumaURL, h.kumaUser, h.kumaPass, h.db, func(p sync.Progress) {
			h.mu.Lock()
			for _, ch := range h.progressChans {
				select {
				case ch <- p:
				default:
				}
			}
			h.mu.Unlock()
		})
	}()

	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"status": "started"})
}

func (h *Handler) ProgressSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := make(chan sync.Progress, 16)

	h.mu.Lock()
	h.progressChans = append(h.progressChans, ch)
	h.mu.Unlock()

	defer func() {
		h.mu.Lock()
		for i, c := range h.progressChans {
			if c == ch {
				h.progressChans = append(h.progressChans[:i], h.progressChans[i+1:]...)
				break
			}
		}
		h.mu.Unlock()
	}()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case p, ok := <-ch:
			if !ok {
				return
			}
			data, _ := json.Marshal(p)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}

func (h *Handler) SyncHistory(w http.ResponseWriter, r *http.Request) {
	runs, err := h.db.GetSyncRuns(50)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if runs == nil {
		runs = []db.SyncRun{}
	}

	if r.Header.Get("Accept") == "text/html" {
		tmpl := template.Must(template.New("history").Parse(`
		{{range .}}
		<tr class="border-b hover:bg-gray-50">
			<td class="p-2 text-sm">#{{.ID}}</td>
			<td class="p-2">
				<span class="px-2 py-1 text-xs rounded {{if eq .Status "completed"}}bg-green-100 text-green-800{{else if eq .Status "completed_with_errors"}}bg-yellow-100 text-yellow-800{{else if eq .Status "running"}}bg-blue-100 text-blue-800{{else}}bg-red-100 text-red-800{{end}}">
					{{.Status}}
				</span>
			</td>
			<td class="p-2 text-sm">{{.StartedAt.Format "Jan 02 15:04:05"}}</td>
			<td class="p-2 text-sm">{{if .FinishedAt}}{{.FinishedAt.Format "15:04:05"}}{{end}}</td>
			<td class="p-2 text-sm">{{.TotalServices}}</td>
			<td class="p-2 text-sm text-green-600">{{.Added}}</td>
			<td class="p-2 text-sm text-gray-500">{{.Skipped}}</td>
			<td class="p-2 text-sm text-red-600">{{.Failed}}</td>
			<td class="p-2 text-sm text-red-500">{{.ErrorMessage}}</td>
		</tr>
		{{end}}`))
		w.Header().Set("Content-Type", "text/html")
		tmpl.Execute(w, runs)
		return
	}

	json.NewEncoder(w).Encode(runs)
}

func (h *Handler) MonitorList(w http.ResponseWriter, r *http.Request) {
	monitors, err := h.db.GetMonitors()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if monitors == nil {
		monitors = []db.Monitor{}
	}
	json.NewEncoder(w).Encode(monitors)
}
