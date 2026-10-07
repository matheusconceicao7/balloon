package model

import (
	"github.com/dhwanikher/balloon/internal/layout"
	"testing"
)

func TestManualSurvivesRebuildAndDeletionUndo(t *testing.T) {
	original := at("20.0", 200, 100)
	d := build(original)
	source := TextItem{Text: "145 ±2", Page: 0, Box: layout.Rect{X: 100, Y: 100, W: 40, H: 12}}
	if err := d.AddManual(source); err != nil {
		t.Fatal(err)
	}
	assertSequence(t, d, source, original)
	id := d.Items[0].ID
	if !d.Items[0].Manual || d.Items[0].Char.Nominal != 145 || d.Items[0].Char.Upper != 2 {
		t.Fatal("manual value not parsed")
	}
	d.Items[0].Include = false
	Build(d, []TextItem{original})
	assertSequence(t, d, source, original)
	if d.Items[0].ID != id || d.Items[0].Include {
		t.Fatal("manual identity or inspection choice lost")
	}
	if err := d.Delete(id); err != nil {
		t.Fatal(err)
	}
	Build(d, []TextItem{original})
	assertSequence(t, d, original)
	if _, err := d.UndoDelete(); err != nil {
		t.Fatal(err)
	}
	assertSequence(t, d, source, original)
	if !d.Items[0].Manual {
		t.Fatal("manual flag lost on undo")
	}
}

func TestManualRejectsInvalidRegionOrCalloutWithoutMutation(t *testing.T) {
	for _, source := range []TextItem{
		{Text: "SEE NOTE 3", Box: layout.Rect{X: 100, Y: 100, W: 40, H: 12}},
		{Text: "10 ±1", Page: 99, Box: layout.Rect{X: 100, Y: 100, W: 40, H: 12}},
		{Text: "10 ±1", Box: layout.Rect{X: -1, Y: 100, W: 40, H: 12}},
		{Text: "10 ±1", Box: layout.Rect{X: 100, Y: 100, W: 0, H: 12}},
	} {
		d := build(at("20.0", 200, 100))
		if err := d.AddManual(source); err == nil {
			t.Errorf("accepted invalid source %+v", source)
		}
		if len(d.Items) != 1 {
			t.Fatal("invalid addition mutated drawing")
		}
	}
}

func TestManualIdentityAllowsUndoWhenAnotherManualItemHasSameCallout(t *testing.T) {
	d := build()
	source := TextItem{Text: "10 ±1", Box: layout.Rect{X: 100, Y: 100, W: 40, H: 12}}
	if err := d.AddManual(source); err != nil {
		t.Fatal(err)
	}
	id := d.Items[0].ID
	if err := d.Delete(id); err != nil {
		t.Fatal(err)
	}
	if err := d.AddManual(source); err != nil {
		t.Fatal(err)
	}
	if _, err := d.UndoDelete(); err != nil {
		t.Fatal(err)
	}
	if len(d.Items) != 2 || d.Items[0].ID == d.Items[1].ID {
		t.Fatal("manual identity must remain unique")
	}
}

func TestRebuildSolvesManualAndDetectedBalloonsTogether(t *testing.T) {
	detected := at("20.0", 200, 100)
	d := build(detected)
	source := TextItem{Text: "145 ±2", Box: layout.Rect{X: 100, Y: 100, W: 40, H: 12}}
	if err := d.AddManual(source); err != nil {
		t.Fatal(err)
	}
	// Simulate a retained manual balloon dragged onto an automatic placement.
	d.Items[0].Balloon = d.Items[1].Balloon
	Build(d, []TextItem{detected})
	a, b := d.Items[0].Balloon, d.Items[1].Balloon
	dx, dy := a.C.X-b.C.X, a.C.Y-b.C.Y
	if dx*dx+dy*dy < (a.R+b.R)*(a.R+b.R) {
		t.Fatal("rebuild left overlapping manual and detected balloons")
	}
}
