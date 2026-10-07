package model

import (
	"crypto/rand"
	"fmt"
	"math"
	"strings"

	"github.com/dhwanikher/balloon/internal/dimension"
	"github.com/dhwanikher/balloon/internal/layout"
)

// AddManual adds a human-confirmed callout. Its parsed values are retained on
// rebuild because the PDF text layer cannot reliably recover this characteristic.
func (d *Drawing) AddManual(source TextItem) error {
	var page *Page
	for i := range d.Pages {
		if d.Pages[i].Index == source.Page {
			page = &d.Pages[i]
			break
		}
	}
	if page == nil {
		return fmt.Errorf("unknown drawing page")
	}
	b := source.Box
	for _, v := range []float64{b.X, b.Y, b.W, b.H} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return fmt.Errorf("invalid measure area")
		}
	}
	if b.X < 0 || b.Y < 0 || b.W <= 0 || b.H <= 0 || b.X+b.W > page.Width || b.Y+b.H > page.Height {
		return fmt.Errorf("mark a measure area inside the drawing")
	}
	source.Text = strings.TrimSpace(source.Text)
	opt := d.Options
	if opt.DefaultTolerances == nil {
		opt = dimension.DefaultOptions()
	}
	c := dimension.Parse(source.Text, opt)
	if !isCharacteristic(c) {
		return fmt.Errorf("enter a valid measure and tolerance, such as 145 ±2")
	}
	token := make([]byte, 16)
	if _, err := rand.Read(token); err != nil {
		return err
	}
	item := Item{ID: fmt.Sprintf("manual-%x", token), Manual: true, Page: source.Page, Source: source, Char: c, Include: c.Inspectable(), Requirement: c.Requirement(), LimitsText: c.Limits(), Designator: c.Designator()}
	// Existing balloon footprints also constrain the initial placement.
	obstacles := append([]layout.Rect(nil), page.Obstacles...)
	obstacles = append(obstacles, b)
	for _, it := range d.Items {
		if it.Page == page.Index {
			circle := it.Balloon
			obstacles = append(obstacles, layout.Rect{X: circle.C.X - circle.R, Y: circle.C.Y - circle.R, W: 2 * circle.R, H: 2 * circle.R})
		}
	}
	placement := layout.Solve([]layout.Anchor{{ID: item.ID, Number: len(d.Items) + 1, At: b.Center(), Avoid: b}}, obstacles, layout.DefaultConfig(layout.Rect{W: page.Width, H: page.Height}))[0]
	item.Balloon, item.Leader, item.Clean, item.Issues = placement.Balloon, placement.Leader, placement.Clean, placement.Issues
	d.Items = append(d.Items, item)
	d.sortReadingOrder()
	d.renumber()
	return nil
}
