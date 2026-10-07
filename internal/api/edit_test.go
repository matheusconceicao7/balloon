package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/dhwanikher/balloon/internal/layout"
	"github.com/dhwanikher/balloon/internal/model"
	"github.com/xuri/excelize/v2"
)

func editDrawing(t *testing.T, path string, body any) model.Drawing {
	t.Helper()
	w := do(t, http.MethodPost, path, body)
	if w.Code != http.StatusOK {
		t.Fatalf("%s returned %d: %s", path, w.Code, w.Body)
	}
	var d model.Drawing
	if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestDeleteUndoThroughLayoutRebuildAndExports(t *testing.T) {
	texts := []model.TextItem{
		{Text: "123.4", Page: 0, Box: layout.Rect{X: 100, Y: 100, W: 30, H: 10}},
		{Text: "20.0", Page: 0, Box: layout.Rect{X: 100, Y: 200, W: 30, H: 10}},
		{Text: "30.0", Page: 1, Box: layout.Rect{X: 100, Y: 100, W: 30, H: 10}},
	}
	d := editDrawing(t, "/api/build", buildRequest{Drawing: model.Drawing{Pages: []model.Page{
		{Index: 0, Width: 842, Height: 595}, {Index: 1, Width: 842, Height: 595},
	}}, Texts: texts})
	obstacles := len(d.Pages[0].Obstacles)
	deletedID := d.Items[0].ID
	d = editDrawing(t, "/api/delete", map[string]any{"drawing": d, "id": deletedID})
	d = editDrawing(t, "/api/layout", layoutRequest{Drawing: d, Page: 0})
	d = editDrawing(t, "/api/build", buildRequest{Drawing: d, Texts: texts, Tolerances: map[string]float64{"1": 0.3}})
	if len(d.Pages[0].Obstacles) != obstacles {
		t.Error("re-reading duplicated text obstacles")
	}
	if len(d.Items) != 2 || len(d.Deleted) != 1 {
		t.Fatalf("deletion lost through layout/build: %+v", d)
	}
	if d.Items[0].Source.Text != "20.0" || d.Items[0].Number != 1 || d.Items[1].Number != 2 {
		t.Error("survivors not renumbered across sheets")
	}
	checkEditedExports(t, d, false)
	d = editDrawing(t, "/api/undo-delete", map[string]any{"drawing": d})
	if len(d.Items) != 3 || len(d.Deleted) != 0 || d.Items[0].ID != deletedID {
		t.Error("undo did not restore original identity and clear history")
	}
	for i, it := range d.Items {
		if it.Number != i+1 {
			t.Error("undo left nonsequential labels")
		}
	}
	if d.Items[0].Char.Upper != 0.3 || d.Items[0].Char.Lower != -0.3 {
		t.Errorf("undo restored stale defaults: %s", d.Items[0].LimitsText)
	}
	checkEditedExports(t, d, true)
}

func checkEditedExports(t *testing.T, d model.Drawing, restored bool) {
	t.Helper()
	w := do(t, "POST", "/api/export.svg", exportRequest{Drawing: d, Page: 0})
	if w.Code != 200 {
		t.Fatalf("SVG export failed: %s", w.Body)
	}
	if strings.Contains(w.Body.String(), ">123.4</text>") != restored {
		t.Error("SVG does not match deleted/restored state")
	}
	// Page zero has two original items and one survivor after deleting the first.
	want := "1"
	if restored {
		want = "2"
	}
	if !strings.Contains(w.Body.String(), ">"+want+"</text>") {
		t.Error("SVG missing renumbered balloon label")
	}
	w = do(t, "POST", "/api/export.xlsx", exportRequest{Drawing: d})
	if w.Code != 200 {
		t.Fatalf("XLSX export failed: %s", w.Body)
	}
	f, err := excelize.OpenReader(bytes.NewReader(w.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rows, err := f.GetRows("Form 3")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	count := 0
	table := false
	for _, r := range rows {
		if len(r) == 0 {
			continue
		}
		if strings.HasPrefix(r[0], "Char.") {
			table = true
			continue
		}
		if !table {
			continue
		}
		n, err := strconv.Atoi(r[0])
		if err != nil {
			continue
		}
		count++
		if n != count {
			t.Errorf("workbook row numbered %d, want %d", n, count)
		}
		if len(r) > 3 && strings.HasPrefix(r[3], "123.4") {
			found = true
		}
	}
	if found != restored || count != len(d.Items) {
		t.Errorf("workbook rows=%d deleted present=%v", count, found)
	}
}

func TestEditEndpointsRejectInvalidActions(t *testing.T) {
	for _, tc := range []struct {
		path string
		body any
	}{
		{"/api/delete", map[string]any{"drawing": model.Drawing{}, "id": "missing"}},
		{"/api/undo-delete", map[string]any{"drawing": model.Drawing{}}},
	} {
		w := do(t, "POST", tc.path, tc.body)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s returned %d, want 400: %s", tc.path, w.Code, w.Body)
		}
	}
}
