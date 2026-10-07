package model

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
)

// sourceID stays stable when visible numbers change or re-reading recognises
// additional callouts. A number is a label, not an item's identity.
func sourceID(source TextItem) string {
	data, _ := json.Marshal(source)
	sum := sha256.Sum256(data)
	return fmt.Sprintf("p%d-%x", source.Page, sum[:12])
}

// Delete saves the complete item for undo and removes it from both render and
// export input. The deletion history is carried by the browser with the drawing.
func (d *Drawing) Delete(id string) error {
	for i, it := range d.Items {
		if it.ID != id {
			continue
		}
		d.Deleted = append(d.Deleted, it)
		d.Items = append(d.Items[:i:i], d.Items[i+1:]...)
		d.renumber()
		return nil
	}
	return fmt.Errorf("no balloon with id %q", id)
}

// UndoDelete restores the most recently deleted item in drawing reading order.
// Saving only an array offset would restore it in the wrong place if re-reading
// recognised another callout earlier in the drawing.
func (d *Drawing) UndoDelete() (Item, error) {
	if len(d.Deleted) == 0 {
		return Item{}, fmt.Errorf("no deletion to undo")
	}
	restored := d.Deleted[len(d.Deleted)-1]
	for _, it := range d.Items {
		if it.Source == restored.Source || it.ID == restored.ID {
			return Item{}, fmt.Errorf("balloon is already present")
		}
	}
	d.Deleted = d.Deleted[:len(d.Deleted)-1]
	d.Items = append(d.Items, restored)
	pages := map[int]int{}
	for i, p := range d.Pages {
		pages[p.Index] = i
	}
	sort.SliceStable(d.Items, func(a, b int) bool {
		x, y := d.Items[a].Source, d.Items[b].Source
		if x.Page != y.Page {
			return pages[x.Page] < pages[y.Page]
		}
		rowX, rowY := int(x.Box.Y/rowBand), int(y.Box.Y/rowBand)
		if rowX != rowY {
			return rowX < rowY
		}
		return x.Box.X < y.Box.X
	})
	d.renumber()
	for _, it := range d.Items {
		if it.ID == restored.ID {
			return it, nil
		}
	}
	return restored, nil
}

func (d *Drawing) renumber() {
	for i := range d.Items {
		d.Items[i].Number = i + 1
	}
}
