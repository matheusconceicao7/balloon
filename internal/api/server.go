// Package api serves the browser frontend and the endpoints it calls.
//
// The server is deliberately stateless: the browser holds the drawing and posts
// it back for each operation. A single-user desktop-style tool gains nothing
// from a database here, and statelessness means the whole application is one
// binary you can run and close without leaving anything behind.
package api

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/dhwanikher/balloon/internal/demo"
	"github.com/dhwanikher/balloon/internal/dimension"
	"github.com/dhwanikher/balloon/internal/export"
	"github.com/dhwanikher/balloon/internal/layout"
	"github.com/dhwanikher/balloon/internal/model"
	"github.com/dhwanikher/balloon/internal/render"
)

// maxBody caps request size. A dense multi-page drawing's text layer is large
// but nowhere near this; the limit is here so a malformed client cannot exhaust
// memory.
const maxBody = 32 << 20

// Server wires the handlers to the embedded frontend.
type Server struct {
	mux *http.ServeMux
}

// New builds the server. assets is the embedded web directory.
func New(assets fs.FS) *Server {
	s := &Server{mux: http.NewServeMux()}

	s.mux.HandleFunc("GET /api/demo", s.handleDemo)
	s.mux.HandleFunc("POST /api/build", s.handleBuild)
	s.mux.HandleFunc("POST /api/parse", s.handleParse)
	s.mux.HandleFunc("POST /api/layout", s.handleLayout)
	s.mux.HandleFunc("POST /api/delete", s.handleDelete)
	s.mux.HandleFunc("POST /api/add-manual", s.handleAddManual)
	s.mux.HandleFunc("POST /api/undo-delete", s.handleUndoDelete)
	s.mux.HandleFunc("POST /api/export.xlsx", s.handleExportXLSX)
	s.mux.HandleFunc("POST /api/export.svg", s.handleExportSVG)
	s.mux.Handle("/", http.FileServer(http.FS(assets)))

	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// handleDemo serves the same fixture the CLI renders, so the frontend has
// something to show without anyone needing to find a drawing first.
func (s *Server) handleDemo(w http.ResponseWriter, r *http.Request) {
	d, texts := demo.Drawing()
	model.Build(d, texts)
	writeJSON(w, http.StatusOK, map[string]any{"drawing": d, "texts": texts})
}

// buildRequest is what the browser sends after extracting a PDF's text layer.
type buildRequest struct {
	Drawing model.Drawing    `json:"drawing"`
	Texts   []model.TextItem `json:"texts"`
	// Tolerances overrides the title block defaults, keyed by decimal places.
	Tolerances map[string]float64 `json:"tolerances,omitempty"`
	Angular    float64            `json:"angular,omitempty"`
	Unit       string             `json:"unit,omitempty"`
}

func (s *Server) handleBuild(w http.ResponseWriter, r *http.Request) {
	var req buildRequest
	if !decode(w, r, &req) {
		return
	}

	d := &req.Drawing
	d.Options = optionsFrom(req)

	// Every piece of text on the sheet is an obstacle, not just the ballooned
	// ones: a balloon that covers the title block or a note it did not balloon
	// is just as unreadable. The browser already has these boxes, so this costs
	// nothing to honour.
	for i := range d.Pages {
		// A re-read posts the existing drawing, whose obstacles already include
		// the text layer. Add missing boxes without accumulating duplicates.
		seen := make(map[layout.Rect]bool, len(d.Pages[i].Obstacles))
		for _, box := range d.Pages[i].Obstacles {
			seen[box] = true
		}
		for _, box := range textObstacles(req.Texts, d.Pages[i].Index) {
			if !seen[box] {
				d.Pages[i].Obstacles = append(d.Pages[i].Obstacles, box)
				seen[box] = true
			}
		}
	}

	model.Build(d, req.Texts)
	writeJSON(w, http.StatusOK, d)
}

func textObstacles(texts []model.TextItem, page int) []layout.Rect {
	var out []layout.Rect
	for _, t := range texts {
		if t.Page == page && strings.TrimSpace(t.Text) != "" {
			out = append(out, t.Box)
		}
	}
	return out
}

type parseRequest struct {
	Text       string             `json:"text"`
	Tolerances map[string]float64 `json:"tolerances,omitempty"`
	Angular    float64            `json:"angular,omitempty"`
	Unit       string             `json:"unit,omitempty"`
}

// handleParse re-parses a single callout, so the editor can show the effect of
// an edit without rebuilding the whole drawing.
func (s *Server) handleParse(w http.ResponseWriter, r *http.Request) {
	var req parseRequest
	if !decode(w, r, &req) {
		return
	}
	c := dimension.Parse(req.Text, optionsFrom(buildRequest{
		Tolerances: req.Tolerances, Angular: req.Angular, Unit: req.Unit,
	}))
	writeJSON(w, http.StatusOK, map[string]any{
		"characteristic": c,
		"requirement":    c.Requirement(),
		"limits":         c.Limits(),
		"designator":     c.Designator(),
		"inspectable":    c.Inspectable(),
	})
}

type layoutRequest struct {
	Drawing model.Drawing `json:"drawing"`
	Page    int           `json:"page"`
}

// handleLayout re-solves balloon positions for one page, for the "tidy up"
// action after a user has dragged things around.
func (s *Server) handleLayout(w http.ResponseWriter, r *http.Request) {
	var req layoutRequest
	if !decode(w, r, &req) {
		return
	}

	var page *model.Page
	for i := range req.Drawing.Pages {
		if req.Drawing.Pages[i].Index == req.Page {
			page = &req.Drawing.Pages[i]
		}
	}
	if page == nil {
		fail(w, http.StatusBadRequest, "no page %d", req.Page)
		return
	}

	var anchors []layout.Anchor
	var idx []int
	for i, it := range req.Drawing.Items {
		if it.Page != req.Page {
			continue
		}
		anchors = append(anchors, layout.Anchor{
			ID: it.ID, Number: it.Number, At: it.Source.Box.Center(), Avoid: it.Source.Box,
		})
		idx = append(idx, i)
	}

	cfg := layout.DefaultConfig(layout.Rect{W: page.Width, H: page.Height})
	for n, p := range layout.Solve(anchors, page.Obstacles, cfg) {
		i := idx[n]
		req.Drawing.Items[i].Balloon = p.Balloon
		req.Drawing.Items[i].Leader = p.Leader
		req.Drawing.Items[i].Clean = p.Clean
		req.Drawing.Items[i].Issues = p.Issues
	}
	writeJSON(w, http.StatusOK, &req.Drawing)
}

type deleteRequest struct {
	Drawing model.Drawing `json:"drawing"`
	ID      string        `json:"id,omitempty"`
	IDs     []string      `json:"ids,omitempty"`
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	var req deleteRequest
	if !decode(w, r, &req) {
		return
	}
	if req.ID != "" && len(req.IDs) > 0 {
		fail(w, http.StatusBadRequest, "use id or ids, not both")
		return
	}
	ids := req.IDs
	if req.ID != "" {
		ids = []string{req.ID}
	}
	if err := req.Drawing.DeleteMany(ids); err != nil {
		fail(w, http.StatusBadRequest, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, &req.Drawing)
}

func (s *Server) handleUndoDelete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Drawing model.Drawing `json:"drawing"`
	}
	if !decode(w, r, &req) {
		return
	}
	if _, err := req.Drawing.UndoDelete(); err != nil {
		fail(w, http.StatusBadRequest, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, &req.Drawing)
}

type exportRequest struct {
	OverlayOnly bool          `json:"overlay_only"`
	Drawing     model.Drawing `json:"drawing"`
	Meta        export.Meta   `json:"meta"`
	Page        int           `json:"page"`
}

func (s *Server) handleExportXLSX(w http.ResponseWriter, r *http.Request) {
	var req exportRequest
	if !decode(w, r, &req) {
		return
	}
	name := filename(req.Drawing, "xlsx")
	w.Header().Set("Content-Type",
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+name+"\"")
	if err := export.XLSX(w, &req.Drawing, req.Meta); err != nil {
		// The status is already committed by the time this can fail, so the
		// error goes to the log rather than the response.
		fmt.Printf("export: %v\n", err)
	}
}

func (s *Server) handleExportSVG(w http.ResponseWriter, r *http.Request) {
	var req exportRequest
	if !decode(w, r, &req) {
		return
	}
	name := filename(req.Drawing, "svg")
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+name+"\"")
	style := render.DefaultStyle()
	style.OverlayOnly = req.OverlayOnly
	if err := render.SVG(w, &req.Drawing, req.Page, style); err != nil {
		fmt.Printf("render: %v\n", err)
	}
}

func filename(d model.Drawing, ext string) string {
	base := d.PartNumber
	if base == "" {
		base = d.Name
	}
	if base == "" {
		base = "drawing"
	}
	base = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`/\:*?"<>|`, r) {
			return '-'
		}
		return r
	}, base)
	if d.Revision != "" {
		base += "-rev" + d.Revision
	}
	return fmt.Sprintf("%s-FAIR-%s.%s", base, time.Now().Format("20060102"), ext)
}

func optionsFrom(req buildRequest) dimension.Options {
	opt := dimension.DefaultOptions()
	if len(req.Tolerances) > 0 {
		m := map[int]float64{}
		for k, v := range req.Tolerances {
			var places int
			if _, err := fmt.Sscanf(k, "%d", &places); err == nil {
				m[places] = v
			}
		}
		if len(m) > 0 {
			opt.DefaultTolerances = m
		}
	}
	if req.Angular > 0 {
		opt.DefaultAngular = req.Angular
	}
	switch strings.ToLower(req.Unit) {
	case "in", "inch":
		opt.Unit = dimension.UnitInch
	case "mm":
		opt.Unit = dimension.UnitMM
	}
	return opt
}

func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		fail(w, http.StatusBadRequest, "invalid request: %v", err)
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, status int, format string, args ...any) {
	writeJSON(w, status, map[string]string{"error": fmt.Sprintf(format, args...)})
}

func (s *Server) handleAddManual(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Drawing    model.Drawing      `json:"drawing"`
		Source     model.TextItem     `json:"source"`
		Tolerances map[string]float64 `json:"tolerances,omitempty"`
		Angular    float64            `json:"angular,omitempty"`
		Unit       string             `json:"unit,omitempty"`
	}
	if !decode(w, r, &req) {
		return
	}
	req.Drawing.Options = optionsFrom(buildRequest{Tolerances: req.Tolerances, Angular: req.Angular, Unit: req.Unit})
	if err := req.Drawing.AddManual(req.Source); err != nil {
		fail(w, http.StatusBadRequest, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, &req.Drawing)
}
