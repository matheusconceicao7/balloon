package api

import (
	"bytes"
	"strings"
	"testing"

	"github.com/dhwanikher/balloon/internal/layout"
	"github.com/dhwanikher/balloon/internal/model"
	"github.com/xuri/excelize/v2"
)

func TestManualAPIExportsAndRebuild(t *testing.T) {
	d := model.Drawing{Pages: []model.Page{{Index: 0, Width: 500, Height: 400}}}
	source := model.TextItem{Text: "145 ±2", Box: layout.Rect{X: 100, Y: 100, W: 40, H: 12}}
	d = editDrawing(t, "/api/add-manual", map[string]any{"drawing": d, "source": source})
	if len(d.Items) != 1 || !d.Items[0].Manual {
		t.Fatal("missing manual item")
	}
	id := d.Items[0].ID
	d = editDrawing(t, "/api/layout", map[string]any{"drawing": d, "page": 0})
	d = editDrawing(t, "/api/build", map[string]any{"drawing": d, "texts": []model.TextItem{}})
	if len(d.Items) != 1 || d.Items[0].ID != id || d.Items[0].Char.Upper != 2 {
		t.Fatal("manual item lost")
	}
	svg := do(t, "POST", "/api/export.svg", map[string]any{"drawing": d, "page": 0})
	if svg.Code != 200 || !strings.Contains(svg.Body.String(), "145") {
		t.Fatal("manual source missing from SVG")
	}
	xlsx := do(t, "POST", "/api/export.xlsx", map[string]any{"drawing": d})
	f, err := excelize.OpenReader(bytes.NewReader(xlsx.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	found := false
	for _, sheet := range f.GetSheetList() {
		rows, err := f.GetRows(sheet)
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			for _, cell := range row {
				if strings.Contains(cell, "145") {
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatal("manual requirement missing from workbook")
	}
	d = editDrawing(t, "/api/delete", map[string]any{"drawing": d, "id": id})
	d = editDrawing(t, "/api/build", map[string]any{"drawing": d, "texts": []model.TextItem{}})
	if len(d.Items) != 0 {
		t.Fatal("deleted manual item resurrected")
	}
	d = editDrawing(t, "/api/undo-delete", map[string]any{"drawing": d})
	if len(d.Items) != 1 || d.Items[0].ID != id {
		t.Fatal("manual undo failed")
	}
	bad := do(t, "POST", "/api/add-manual", map[string]any{"drawing": d, "source": model.TextItem{Text: "invalid", Box: source.Box}})
	if bad.Code != 400 {
		t.Fatal("invalid manual callout accepted")
	}
}
