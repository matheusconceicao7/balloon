//go:build browser

package api

import (
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
	"testing"
)

func TestBrowserToolbarLayout(t *testing.T) {
	ctx := editorBrowser(t)
	if !browserValue[bool](t, ctx, `document.querySelector('#exportXlsx').disabled && document.querySelector('#zoomIn').disabled && document.querySelector('#openDrawing').classList.contains('primary')`) {
		t.Fatal("empty editor should emphasize opening a drawing and disable drawing actions")
	}
	loadEditorDemo(t, ctx)
	if !browserValue[bool](t, ctx, `document.querySelector('#exportXlsx').classList.contains('primary') && !document.querySelector('#openDrawing').classList.contains('primary') && document.querySelector('#documentName').textContent==='Demo part'`) {
		t.Fatal("loaded drawing should show its name and emphasize report export")
	}
	browserDo(t, ctx, chromedp.Click(chromedp.CSS("#zoomIn")))
	waitEditor(t, ctx, `document.querySelector('#zoomLabel').textContent==='163%'`)
	browserDo(t, ctx, chromedp.Click(chromedp.CSS("#addBalloon")))
	if !browserValue[bool](t, ctx, `!document.querySelector('#addHint').hidden`) {
		t.Fatal("add mode should show a persistent instruction")
	}
	browserDo(t, ctx, chromedp.KeyEvent(kb.Escape))
	if !browserValue[bool](t, ctx, `document.querySelector('#addHint').hidden`) {
		t.Fatal("Escape should dismiss add mode")
	}
	browserDo(t, ctx, chromedp.Click(chromedp.CSS("#exportOptions")))
	if !browserValue[bool](t, ctx, `document.querySelector('#exportMenu').matches(':popover-open') && document.querySelector('#exportPdf').disabled && !document.querySelector('#exportSvg').disabled`) {
		t.Fatal("export options should reflect available formats")
	}
	browserDo(t, ctx, chromedp.KeyEvent(kb.Escape))
	if !browserValue[bool](t, ctx, `!document.querySelector('#exportMenu').matches(':popover-open') && document.activeElement.id==='exportOptions'`) {
		t.Fatal("Escape should close export options and restore focus")
	}
	browserDo(t, ctx, chromedp.Click(chromedp.CSS("#exportOptions")), chromedp.Click(chromedp.CSS("#exportSvg")))
	waitEditor(t, ctx, `!document.querySelector('#exportSvg').disabled`)
	if browserValue[bool](t, ctx, `document.querySelector('#exportMenu').matches(':popover-open')`) {
		t.Fatal("choosing an export must close the dropdown")
	}
	for _, width := range []int64{1440, 1024, 768, 390} {
		browserDo(t, ctx, chromedp.EmulateViewport(width, 900))
		waitEditor(t, ctx, `document.documentElement.scrollWidth <= window.innerWidth && document.querySelector('#stage').getBoundingClientRect().height > 100`)
		if width == 768 {
			browserDo(t, ctx, chromedp.Click(chromedp.CSS(".report-details summary")), chromedp.Click(chromedp.CSS(".tolerances summary")), chromedp.ScrollIntoView(chromedp.CSS("#reparse")))
			if !browserValue[bool](t, ctx, `document.querySelector('#reparse').getBoundingClientRect().bottom <= innerHeight`) {
				t.Fatal("expanded sidebar controls must remain scrollable into view")
			}
			browserDo(t, ctx, chromedp.Click(chromedp.CSS(".report-details summary")), chromedp.Click(chromedp.CSS(".tolerances summary")))
		}
	}
	if !browserValue[bool](t, ctx, `getComputedStyle(document.querySelector('#appearanceTools')).display==='none'`) {
		t.Fatal("closed appearance popover must be hidden")
	}
	browserDo(t, ctx, chromedp.Click(chromedp.CSS("#appearanceOptions")))
	if !browserValue[bool](t, ctx, `document.querySelector('#appearanceTools').matches(':popover-open')`) {
		t.Fatal("narrow layout should expose appearance tools")
	}
	browserDo(t, ctx, chromedp.SetValue(chromedp.CSS("#balloonColor"), "green"), chromedp.Evaluate[chromedp.Void](`document.querySelector('#balloonColor').dispatchEvent(new Event('change',{bubbles:true}))`))
	waitEditor(t, ctx, `document.querySelector('#overlay').style.getPropertyValue('--balloon-ink') !== ''`)
	browserDo(t, ctx, chromedp.KeyEvent(kb.Escape), chromedp.EmulateViewport(1440, 900))
	waitEditor(t, ctx, `!document.querySelector('#appearanceTools').hasAttribute('popover') && document.querySelector('#balloonColor').value==='green'`)
	if got := browserValue[[]string](t, ctx, `window.__errors`); len(got) > 0 {
		t.Fatalf("browser errors: %v", got)
	}
}
