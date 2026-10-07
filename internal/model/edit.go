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

// Deletion records a whole user action so one undo restores its entire batch.
type Deletion struct {
	Items []Item `json:"items"`
}

func (d *Drawing) Delete(id string) error {
	return d.DeleteMany([]string{id})
}

// DeleteMany validates the entire selection before changing the drawing. The
// saved items follow drawing order, regardless of selection order or duplicate IDs.
func (d *Drawing) DeleteMany(ids []string) error {
	if len(ids) == 0 {
		return fmt.Errorf("no balloons selected")
	}
	selected := make(map[string]bool, len(ids))
	for _, id := range ids {
		selected[id] = true
	}
	removed := make([]Item, 0, len(selected))
	remaining := make([]Item, 0, len(d.Items))
	for _, item := range d.Items {
		if selected[item.ID] {
			removed = append(removed, item)
		} else {
			remaining = append(remaining, item)
		}
	}
	if len(removed) != len(selected) {
		return fmt.Errorf("selection contains an unknown balloon")
	}
	d.Deleted = append(d.Deleted, Deletion{Items: removed})
	d.Items = remaining
	d.renumber()
	return nil
}

// UndoDelete restores the most recent deletion action in drawing reading order.
// Saving only an array offset would restore it in the wrong place if re-reading
// recognised another callout earlier in the drawing.
func (d *Drawing) UndoDelete() ([]Item, error) {
	if len(d.Deleted) == 0 {
		return nil, fmt.Errorf("no deletion to undo")
	}
	restored := d.Deleted[len(d.Deleted)-1].Items
	if len(restored) == 0 {
		return nil, fmt.Errorf("empty deletion action")
	}
	for _, removed := range restored {
		for _, it := range d.Items {
			if it.Source == removed.Source || it.ID == removed.ID {
				return nil, fmt.Errorf("balloon is already present")
			}
		}
	}

	d.Deleted = d.Deleted[:len(d.Deleted)-1]
	d.Items = append(d.Items, restored...)
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
	ids := map[string]bool{}
	for _, it := range restored {
		ids[it.ID] = true
	}
	result := make([]Item, 0, len(restored))
	for _, it := range d.Items {
		if ids[it.ID] {
			result = append(result, it)
		}
	}
	return result, nil
}

func (d *Drawing) renumber() {
	for i := range d.Items {
		d.Items[i].Number = i + 1
	}
}
