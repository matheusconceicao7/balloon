//go:build browser

package api

import (
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

// These tests exercise actual browser events. They are opt-in because they
// require Chrome/Chromium; ordinary model and API tests need only Go.
func editorBrowser(t *testing.T) context.Context {
	t.Helper()
	server := httptest.NewServer(srv())
	t.Cleanup(server.Close)
	opts := append([]chromedp.ExecAllocatorOption(nil), chromedp.DefaultExecAllocatorOptions[:]...)
	opts = append(opts, chromedp.WindowSize(1440, 1000))
	if path := os.Getenv("BALLOON_TEST_BROWSER"); path != "" {
		opts = append(opts, chromedp.ExecPath(path))
	}
	alloc, cancel := chromedp.NewExecAllocator(context.Background(), opts...)
	t.Cleanup(cancel)
	ctx, cancel := chromedp.NewContext(alloc)
	t.Cleanup(cancel)
	ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
	t.Cleanup(cancel)
	browserDo(t, ctx, chromedp.Navigate(server.URL), chromedp.Evaluate[chromedp.Void](`window.__keys=[]; document.addEventListener("keydown",e=>window.__keys.push({key:e.key,target:e.target.id,editable:e.target.isContentEditable})); window.__errors=[]; window.addEventListener("error",e=>window.__errors.push(e.message));`))
	return ctx
}

func browserDo(t *testing.T, ctx context.Context, actions ...chromedp.Action[chromedp.Void]) {
	t.Helper()
	if err := chromedp.Do(ctx, actions...); err != nil {
		t.Fatal(err)
	}
}

func browserValue[T any](t *testing.T, ctx context.Context, expression string) T {
	t.Helper()
	result, err := chromedp.Run(ctx, chromedp.Evaluate[T](expression))
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func waitEditor(t *testing.T, ctx context.Context, expression string) {
	t.Helper()
	if err := chromedp.Do(ctx, chromedp.Poll[chromedp.Void](expression, chromedp.WithPollingTimeout(5*time.Second))); err != nil {
		t.Log(browserValue[map[string]any](t, ctx, `({keys:window.__keys,errors:window.__errors,toast:document.querySelector("#toast").textContent,selected:document.querySelector("#tbody .selected")?.dataset.id,button:document.querySelector("#deleteBalloon").disabled,count:document.querySelectorAll(".balloon").length})`))
		t.Fatal(err)
	}
}

func loadEditorDemo(t *testing.T, ctx context.Context) int {
	t.Helper()
	browserDo(t, ctx, chromedp.Click(chromedp.CSS("#loadDemo")))
	waitEditor(t, ctx, `document.querySelectorAll('#tbody tr[data-id]').length>0`)
	return browserValue[int](t, ctx, `document.querySelectorAll('#tbody tr[data-id]').length`)
}

func waitItemCount(t *testing.T, ctx context.Context, n int) {
	t.Helper()
	waitEditor(t, ctx, fmt.Sprintf(`document.querySelectorAll('.balloon').length===%d && !document.querySelector('#tidy').disabled`, n))
}

func TestBrowserDeleteRenumberUndoAndReRead(t *testing.T) {
	ctx := editorBrowser(t)
	count := loadEditorDemo(t, ctx)
	original := browserValue[[]string](t, ctx, `Array.from(document.querySelectorAll('#tbody .req'),el=>el.textContent)`)
	for _, key := range []string{kb.Delete, kb.Backspace} {
		browserDo(t, ctx, chromedp.ScrollIntoView(chromedp.CSS("#tbody .num")), chromedp.Click(chromedp.CSS("#tbody .num")), chromedp.KeyEvent(key))
		count--
		waitItemCount(t, ctx, count)
	}
	numbers := browserValue[[]int](t, ctx, `Array.from(document.querySelectorAll('#tbody .num'),el=>Number(el.textContent))`)
	for i, n := range numbers {
		if n != i+1 {
			t.Errorf("number %d = %d", i, n)
		}
	}
	browserDo(t, ctx, chromedp.Click(chromedp.CSS("#tidy")))
	waitEditor(t, ctx, `document.querySelector('#toast').textContent==='Balloons re-placed'`)
	browserDo(t, ctx, chromedp.Click(chromedp.CSS("summary")), chromedp.Click(chromedp.CSS("#reparse")))
	waitEditor(t, ctx, `document.querySelector('#toast').textContent==='Drawing re-read with the new defaults'`)
	waitItemCount(t, ctx, count)
	browserDo(t, ctx, chromedp.Click(chromedp.CSS("#undoDelete")))
	count++
	waitItemCount(t, ctx, count)
	// Verify both Windows/Linux and macOS shortcuts are accepted.
	browserDo(t, ctx, chromedp.KeyEvent("z", chromedp.KeyModifiers(chromedp.ModifierMeta)))
	count++
	waitItemCount(t, ctx, count)
	actual := browserValue[[]string](t, ctx, `Array.from(document.querySelectorAll('#tbody .req'),el=>el.textContent)`)
	if !reflect.DeepEqual(actual, original) {
		t.Fatalf("undo restored wrong order: %v", actual)
	}
	if !browserValue[bool](t, ctx, `document.querySelector('#undoDelete').disabled`) {
		t.Error("undo button enabled with empty history")
	}
}

func TestBrowserShortcutsProtectTextEditing(t *testing.T) {
	ctx := editorBrowser(t)
	count := loadEditorDemo(t, ctx)
	browserDo(t, ctx, chromedp.ScrollIntoView(chromedp.CSS("#tbody .num")), chromedp.Click(chromedp.CSS("#tbody .num")), chromedp.KeyEvent(kb.Delete))
	waitItemCount(t, ctx, count-1)
	for _, selector := range []string{"#partNumber", "#tbody .req"} {
		browserDo(t, ctx, chromedp.Evaluate[chromedp.Void](fmt.Sprintf(`document.querySelector(%q).focus()`, selector)), chromedp.KeyEvent(kb.Backspace), chromedp.KeyEvent(kb.Delete), chromedp.KeyEvent("z", chromedp.KeyModifiers(chromedp.ModifierCtrl)))
		if got := browserValue[int](t, ctx, `document.querySelectorAll('.balloon').length`); got != count-1 {
			t.Errorf("editing %s changed balloon count to %d", selector, got)
		}
	}
	browserDo(t, ctx, chromedp.Evaluate[chromedp.Void](`document.activeElement.blur()`))
	waitItemCount(t, ctx, count-1)
	browserDo(t, ctx, chromedp.KeyEvent("z", chromedp.KeyModifiers(chromedp.ModifierCtrl)))
	waitItemCount(t, ctx, count)
}

func TestBrowserDeleteAllAndUndo(t *testing.T) {
	ctx := editorBrowser(t)
	count := loadEditorDemo(t, ctx)
	for n := count; n > 0; n-- {
		browserDo(t, ctx, chromedp.ScrollIntoView(chromedp.CSS("#tbody .num")), chromedp.Click(chromedp.CSS("#tbody .num")), chromedp.Click(chromedp.CSS("#deleteBalloon")))
		waitItemCount(t, ctx, n-1)
	}
	if !browserValue[bool](t, ctx, `document.querySelector('#deleteBalloon').disabled`) {
		t.Error("delete enabled on empty drawing")
	}
	browserDo(t, ctx, chromedp.Click(chromedp.CSS("#undoDelete")))
	waitItemCount(t, ctx, 1)
	if got := browserValue[string](t, ctx, `document.querySelector('#tbody .num').textContent`); got != "1" {
		t.Errorf("restored balloon number=%s", got)
	}
}

func TestBrowserMultiPagePDF(t *testing.T) {
	path := os.Getenv("BALLOON_TEST_PDF")
	if path == "" {
		t.Skip("set BALLOON_TEST_PDF to a two-page drawing")
	}
	ctx := editorBrowser(t)
	browserDo(t, ctx, chromedp.SetUploadFiles(chromedp.CSS("#file"), []string{path}))
	waitEditor(t, ctx, `document.querySelector('#pageLabel').textContent==='1 / 2' && document.querySelectorAll('.balloon').length>0`)
	firstCount := browserValue[int](t, ctx, `document.querySelectorAll('.balloon').length`)
	canvas := browserValue[string](t, ctx, `document.querySelector('#canvas').toDataURL()`)
	browserDo(t, ctx, chromedp.ScrollIntoView(chromedp.CSS("#tbody .num")), chromedp.Click(chromedp.CSS("#tbody .num")), chromedp.KeyEvent(kb.Delete))
	waitItemCount(t, ctx, firstCount-1)
	if got := browserValue[string](t, ctx, `document.querySelector('#canvas').toDataURL()`); got != canvas {
		t.Error("deletion changed original PDF rendering")
	}
	browserDo(t, ctx, chromedp.Click(chromedp.CSS("#next")))
	waitEditor(t, ctx, `document.querySelector('#pageLabel').textContent==='2 / 2'`)
	label := browserValue[int](t, ctx, `Number(document.querySelector('#tbody .num').textContent)`)
	if label != firstCount {
		t.Errorf("second sheet starts at %d, want %d", label, firstCount)
	}
	browserDo(t, ctx, chromedp.Click(chromedp.CSS("#undoDelete")))
	waitEditor(t, ctx, `document.querySelector('#pageLabel').textContent==='1 / 2'`)
	waitItemCount(t, ctx, firstCount)
}
