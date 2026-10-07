package model

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/dhwanikher/balloon/internal/dimension"
	"github.com/dhwanikher/balloon/internal/layout"
)

func assertSequence(t *testing.T, d *Drawing, sources ...TextItem) {
	t.Helper()
	if len(d.Items) != len(sources) {
		t.Fatalf("got %d items, want %d", len(d.Items), len(sources))
	}
	ids := map[string]bool{}
	for i, source := range sources {
		it := d.Items[i]
		if it.Source != source || it.Number != i+1 {
			t.Errorf("item %d = %s (#%d), want %s (#%d)", i, it.Source.Text, it.Number, source.Text, i+1)
		}
		if ids[it.ID] {
			t.Errorf("duplicate id %q", it.ID)
		}
		ids[it.ID] = true
	}
}

func TestDeleteRenumbersAcrossPagesWithoutChangingOtherItems(t *testing.T) {
	a, b := at("10.0", 100, 100), at("20.0", 200, 100)
	c := at("30.0", 100, 100)
	c.Page = 1
	d := &Drawing{Pages: []Page{page(), {Index: 1, Width: 842, Height: 595}}}
	Build(d, []TextItem{a, b, c})
	survivor := d.Items[1]
	id := d.Items[0].ID
	if err := d.Delete(id); err != nil {
		t.Fatal(err)
	}
	assertSequence(t, d, b, c)
	survivor.Number = 1
	if !reflect.DeepEqual(d.Items[0], survivor) {
		t.Error("deletion changed a surviving item's content, identity or placement")
	}
	if len(d.Deleted) != 1 || d.Deleted[0].Items[0].ID != id {
		t.Error("deleted item not saved for undo")
	}
}

func TestUndoRestoresMultipleDeletionsInReverseOrder(t *testing.T) {
	a, b, c := at("10.0", 100, 100), at("20.0", 200, 100), at("30.0", 300, 100)
	d := build(a, b, c)
	original := append([]Item(nil), d.Items...)
	d.Items[1].Include = false
	original[1].Include = false
	if err := d.Delete(d.Items[1].ID); err != nil {
		t.Fatal(err)
	}
	if err := d.Delete(d.Items[0].ID); err != nil {
		t.Fatal(err)
	}
	assertSequence(t, d, c)
	if _, err := d.UndoDelete(); err != nil {
		t.Fatal(err)
	}
	assertSequence(t, d, a, c)
	if _, err := d.UndoDelete(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(d.Items, original) {
		t.Error("undo did not restore the original items, inclusion flags and positions")
	}
	if len(d.Deleted) != 0 {
		t.Error("undo history not exhausted")
	}
}

func TestDeletionSurvivesJSONAndRebuildWithNewTolerances(t *testing.T) {
	a, b := at("10.0", 100, 100), at("10.0", 200, 100)
	d := build(a, b)
	if err := d.Delete(d.Items[0].ID); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	var restored Drawing
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	restored.Options = dimension.DefaultOptions()
	restored.Options.DefaultTolerances = map[int]float64{1: 0.8}
	// This newly recognised callout shifts the original generated sequence.
	added := at("5.0", 50, 50)
	Build(&restored, []TextItem{added, a, b})
	assertSequence(t, &restored, added, b)
	if _, err := restored.UndoDelete(); err != nil {
		t.Fatal(err)
	}
	assertSequence(t, &restored, added, a, b)
	if restored.Items[1].LimitsText != "9.2 / 10.8" {
		t.Errorf("undo restored stale tolerance: %s", restored.Items[1].LimitsText)
	}
}

func TestDeleteAndUndoRejectInvalidActionsWithoutChanges(t *testing.T) {
	d := build(at("10.0", 100, 100))
	before, _ := json.Marshal(d)
	if err := d.Delete("missing"); err == nil {
		t.Error("unknown id should fail")
	}
	if _, err := d.UndoDelete(); err == nil {
		t.Error("empty undo history should fail")
	}
	after, _ := json.Marshal(d)
	if string(before) != string(after) {
		t.Error("invalid actions changed drawing")
	}
}

func TestDeleteAllThenUndo(t *testing.T) {
	a := at("10.0", 100, 100)
	d := build(a)
	if err := d.Delete(d.Items[0].ID); err != nil {
		t.Fatal(err)
	}
	assertSequence(t, d)
	Build(d, []TextItem{a})
	assertSequence(t, d)
	if _, err := d.UndoDelete(); err != nil {
		t.Fatal(err)
	}
	assertSequence(t, d, a)
}

func TestUndoKeepsReadingOrderWhenRebuildAddsEarlierCallouts(t *testing.T) {
	a, b := at("10.0", 100, 100), at("20.0", 200, 100)
	d := build(a, b)
	d.Items[0].Balloon = layout.Circle{C: layout.Point{X: 400, Y: 400}, R: 10}
	position := d.Items[0].Balloon
	if err := d.Delete(d.Items[0].ID); err != nil {
		t.Fatal(err)
	}
	added := at("5.0", 50, 50)
	Build(d, []TextItem{added, a, b})
	if _, err := d.UndoDelete(); err != nil {
		t.Fatal(err)
	}
	assertSequence(t, d, added, a, b)
	if d.Items[1].Balloon != position {
		t.Error("undo lost deleted balloon's saved position")
	}
}

func TestBatchDeletionIsOneUndoAction(t *testing.T) {
	a, b, c, d := at("10.0", 100, 100), at("20.0", 200, 100), at("30.0", 300, 100), at("40.0", 400, 100)
	drawing := build(a, b, c, d)
	original := append([]Item(nil), drawing.Items...)
	// Selection order and duplicate IDs must not affect drawing or undo order.
	if err := drawing.DeleteMany([]string{original[2].ID, original[0].ID, original[2].ID}); err != nil {
		t.Fatal(err)
	}
	assertSequence(t, drawing, b, d)
	if len(drawing.Deleted) != 1 {
		t.Fatal("batch must create exactly one history entry")
	}
	if err := drawing.Delete(drawing.Items[0].ID); err != nil {
		t.Fatal(err)
	}
	assertSequence(t, drawing, d)
	restored, err := drawing.UndoDelete()
	if err != nil {
		t.Fatal(err)
	}
	if len(restored) != 1 {
		t.Fatal("first undo restored the wrong action")
	}
	assertSequence(t, drawing, b, d)
	restored, err = drawing.UndoDelete()
	if err != nil {
		t.Fatal(err)
	}
	if len(restored) != 2 {
		t.Fatal("batch undo must restore both balloons")
	}
	if !reflect.DeepEqual(drawing.Items, original) {
		t.Error("batch undo did not restore all original items and positions")
	}
}

func TestBatchDeletionRejectsInvalidSelectionAtomically(t *testing.T) {
	for _, ids := range [][]string{nil, {}, {"missing"}} {
		drawing := build(at("10.0", 100, 100), at("20.0", 200, 100))
		if len(ids) > 0 {
			ids = append([]string{drawing.Items[0].ID}, ids...)
		}
		before, _ := json.Marshal(drawing)
		if err := drawing.DeleteMany(ids); err == nil {
			t.Error("invalid batch should fail")
		}
		after, _ := json.Marshal(drawing)
		if string(before) != string(after) {
			t.Error("invalid selection partially deleted balloons")
		}
	}
}

func TestBatchUndoSurvivesJSONAndRebuildAcrossSheets(t *testing.T) {
	a, b, c := at("10.0", 100, 100), at("20.0", 200, 100), at("30.0", 100, 100)
	c.Page = 1
	drawing := &Drawing{Pages: []Page{page(), {Index: 1, Width: 842, Height: 595}}}
	Build(drawing, []TextItem{a, b, c})
	if err := drawing.DeleteMany([]string{drawing.Items[0].ID, drawing.Items[2].ID}); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(drawing)
	if err != nil {
		t.Fatal(err)
	}
	var back Drawing
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	back.Options = dimension.DefaultOptions()
	back.Options.DefaultTolerances = map[int]float64{1: 0.4}
	Build(&back, []TextItem{a, b, c})
	assertSequence(t, &back, b)
	if _, err := back.UndoDelete(); err != nil {
		t.Fatal(err)
	}
	assertSequence(t, &back, a, b, c)
	if back.Items[0].Char.Upper != 0.4 || back.Items[2].Char.Upper != 0.4 {
		t.Error("batch undo restored stale tolerance defaults")
	}
}
