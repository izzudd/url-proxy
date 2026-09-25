package dashboard

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"strconv"

	"link-proxy/internal/database"
)

//go:embed templates/*
var templateFS embed.FS

type Handler struct {
	db      *database.DB
	tmpl    *template.Template
	baseURL string
}

func NewHandler(db *database.DB, baseURL string) (*Handler, error) {
	tmpl, err := template.ParseFS(templateFS, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}
	return &Handler{
		db:      db,
		tmpl:    tmpl,
		baseURL: baseURL,
	}, nil
}

func (h *Handler) ServeDashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = h.tmpl.ExecuteTemplate(w, "index.html", nil)
}

func (h *Handler) ServeDocs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = h.tmpl.ExecuteTemplate(w, "docs.html", nil)
}

func (h *Handler) ServeStats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.db.GetStats(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `
	<div class="bg-slate-800/60 border border-slate-700/60 rounded-xl p-6 shadow-sm">
		<p class="text-sm font-medium text-slate-400">Total Registered Files</p>
		<p class="text-3xl font-bold mt-2 text-white">%d</p>
	</div>
	<div class="bg-slate-800/60 border border-slate-700/60 rounded-xl p-6 shadow-sm">
		<p class="text-sm font-medium text-slate-400">Total Bandwidth Streamed</p>
		<p class="text-3xl font-bold mt-2 text-indigo-400">%s</p>
	</div>
	<div class="bg-slate-800/60 border border-slate-700/60 rounded-xl p-6 shadow-sm">
		<p class="text-sm font-medium text-slate-400">Total Stream Requests</p>
		<p class="text-3xl font-bold mt-2 text-emerald-400">%d</p>
	</div>
	`, stats.TotalFiles, formatBytes(stats.TotalBytesServed), stats.TotalAccessCount)
}

func (h *Handler) ServeChart(w http.ResponseWriter, r *http.Request) {
	data, err := h.db.GetChartMetrics(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(data)
}

func (h *Handler) ServeFiles(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	search := r.URL.Query().Get("search")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	files, err := h.db.ListFiles(ctx, limit, 0, search)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if len(files) == 0 {
		fmt.Fprint(w, `<tr><td colspan="7" class="px-6 py-6 text-center text-slate-500">No files registered yet.</td></tr>`)
		return
	}

	for _, f := range files {
		proxyURL := fmt.Sprintf("%s/p/%s", h.baseURL, f.ID)
		fmt.Fprintf(w, `
		<tr class="hover:bg-slate-800/30 transition">
			<td class="px-6 py-4 font-mono text-xs text-blue-400 font-semibold">%s</td>
			<td class="px-6 py-4 max-w-sm">
				<div class="font-medium text-slate-200 truncate" title="%s">%s</div>
				<a href="%s" target="_blank" rel="noopener noreferrer" class="text-xs text-slate-400 hover:text-blue-400 flex items-center gap-1 mt-0.5 truncate group" title="%s">
					<span class="truncate">%s</span>
					<svg class="w-3 h-3 shrink-0 opacity-60 group-hover:opacity-100" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M10 6H6a2 2 0 00-2 2v10a2 2 0 002 2h10a2 2 0 002-2v-4M14 4h6m0 0v6m0-6L10 14"></path></svg>
				</a>
			</td>
			<td class="px-6 py-4 text-xs font-mono text-slate-400">%s</td>
			<td class="px-6 py-4 text-xs text-slate-400">%s</td>
			<td class="px-6 py-4 text-xs font-semibold text-slate-200">%d</td>
			<td class="px-6 py-4 text-xs text-slate-400">%s</td>
			<td class="px-6 py-4 text-right space-x-2">
				<a href="%s" target="_blank" class="text-xs bg-slate-700 hover:bg-slate-600 text-slate-200 px-2.5 py-1 rounded transition">Stream</a>
				<button onclick="navigator.clipboard.writeText('%s')" class="text-xs bg-blue-600/30 hover:bg-blue-600 text-blue-300 hover:text-white px-2.5 py-1 rounded transition">Copy Link</button>
			</td>
		</tr>
		`, f.ID, f.Filename, f.Filename, f.OriginalURL, f.OriginalURL, f.OriginalURL, f.ContentType, formatBytes(f.FileSize), f.AccessCount, formatBytes(f.BytesServed), proxyURL, proxyURL)
	}
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
