//go:build browser

package api

import (
	"encoding/base64"
	"os"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

func TestBrowserPDFExportPreservesPages(t *testing.T) {
	path := os.Getenv("BALLOON_TEST_PDF")
	if path == "" {
		t.Skip("set BALLOON_TEST_PDF to a drawing")
	}
	ctx := editorBrowser(t)
	browserDo(t, ctx, chromedp.SetUploadFiles(chromedp.CSS("#file"), []string{path}))
	waitEditor(t, ctx, `document.querySelectorAll('.balloon').length>0 && !document.querySelector('#exportSvg').disabled`)
	// Keep the actual downloaded blob and the original input for round-trip checks.
	browserDo(t, ctx, chromedp.Evaluate[chromedp.Void](`window.__pdfBlob=null; const create=URL.createObjectURL; URL.createObjectURL=function(blob){if(blob.type==='application/pdf')window.__pdfBlob=blob;return create.call(this,blob)};`), chromedp.Click(chromedp.CSS("#exportPdf")))
	waitEditor(t, ctx, `window.__pdfBlob!==null`)
	result, err := chromedp.Run(ctx, chromedp.Evaluate[string](`(async()=>{
      const lib=await import('/vendor/pdf.mjs');
      const original=await lib.getDocument({data:await document.querySelector('#file').files[0].arrayBuffer()}).promise;
      const output=await lib.getDocument({data:await window.__pdfBlob.arrayBuffer()}).promise;
      if(output.numPages!==original.numPages) return 'lost pages';
      let added=0;
      for(let n=1;n<=original.numPages;n++) {
        const a=await original.getPage(n), b=await output.getPage(n);
        if(JSON.stringify(a.view)!==JSON.stringify(b.view)||a.rotate!==b.rotate) return 'changed page geometry';
        const ta=(await a.getTextContent()).items.map(i=>i.str).filter(Boolean);
        const tb=(await b.getTextContent()).items.map(i=>i.str).filter(Boolean);
        let j=0; for(const s of tb) if(s===ta[j])j++;
        if(j!==ta.length) return 'lost original drawing text on page '+n;
        added+=tb.length-ta.length;
      }
      return added>0?'ok':'no balloon labels in PDF';
    })()`, chromedp.EvalAwaitPromise))
	if err != nil || result != "ok" {
		t.Fatalf("round-trip PDF: %s, %v", result, err)
	}
	if path := os.Getenv("BALLOON_TEST_EXPORT"); path != "" {
		data, err := chromedp.Run(ctx, chromedp.Evaluate[string](`new Promise(resolve=>{const r=new FileReader();r.onload=()=>resolve(r.result.split(',')[1]);r.readAsDataURL(window.__pdfBlob)})`, chromedp.EvalAwaitPromise))
		if err != nil {
			t.Fatal(err)
		}
		bytes, err := base64.StdEncoding.DecodeString(data)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, bytes, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestBrowserPDFExportRotatedCroppedPages(t *testing.T) {
	ctx := editorBrowser(t)
	result, err := chromedp.Run(ctx, chromedp.Evaluate[string](`(async()=>{
      const {PDFDocument,PDFName,PDFNumber,degrees,rgb}=await import('/vendor/pdf-lib.mjs');
      const js=await import('/vendor/pdf.mjs');
      const {exportPDF}=await import('/export.js');
      const source=await PDFDocument.create();
      for(const angle of [0,90,180,270]) {
        const p=source.addPage([320,240]);p.setCropBox(20,30,240,160);p.setRotation(degrees(angle));
        p.node.set(PDFName.of('UserUnit'),PDFNumber.of(2));
        p.drawRectangle({x:30,y:40,width:100,height:90,color:rgb(0.2,0.5,0.7)});
        p.drawText('Original geometry',{x:40,y:160,size:12});
      }
      const original=await js.getDocument({data:await source.save()}).promise;
      const drawing={pages:[],items:[],balloon_color:'red'};
      for(let i=0;i<4;i++) {
        const p=await original.getPage(i+1),v=p.getViewport({scale:1});
        drawing.pages.push({index:i,width:v.width,height:v.height});
        drawing.items.push({page:i,number:i+1,clean:true,balloon:{c:{x:70,y:60},r:12},leader:{a:{x:45,y:40},b:{x:62,y:51}}});
      }
      const blob=await exportPDF(original,drawing);
      const output=await js.getDocument({data:await blob.arrayBuffer()}).promise;
      const pixels=async p=>{const v=p.getViewport({scale:1}),c=document.createElement('canvas');c.width=v.width;c.height=v.height;const ctx=c.getContext('2d');await p.render({canvasContext:ctx,viewport:v}).promise;return {w:c.width,h:c.height,data:ctx.getImageData(0,0,c.width,c.height).data}};
      for(let i=1;i<=4;i++) {
        const a=await pixels(await original.getPage(i)),b=await pixels(await output.getPage(i));
        if(a.w!==b.w||a.h!==b.h) return 'wrong dimensions on '+i;
        let changed=0;
        for(let y=0;y<a.h;y++)for(let x=0;x<a.w;x++) {
          const k=(y*a.w+x)*4;
          if(Math.abs(a.data[k]-b.data[k])+Math.abs(a.data[k+1]-b.data[k+1])+Math.abs(a.data[k+2]-b.data[k+2])<12)continue;
          if(x<42||x>84||y<37||y>74)return 'drawing changed outside balloon at '+x+','+y+' page '+i;
          changed++;
        }
        if(changed<50)return 'missing balloon on '+i;
      }
      // Export twice from the same original, proving balloons are not accumulated.
      const again=await js.getDocument({data:await(await exportPDF(original,drawing)).arrayBuffer()}).promise;
      if((await(await again.getPage(1)).getTextContent()).items.length!==(await(await output.getPage(1)).getTextContent()).items.length)return 'duplicate balloons';
      return 'ok';
    })()`, chromedp.EvalAwaitPromise))
	if err != nil || result != "ok" {
		t.Fatalf("PDF render comparison: %s, %v", result, err)
	}
}

func TestBrowserSVGIncludesOriginalPDF(t *testing.T) {
	path := os.Getenv("BALLOON_TEST_PDF")
	if path == "" {
		t.Skip("set BALLOON_TEST_PDF to a drawing")
	}
	ctx := editorBrowser(t)
	browserDo(t, ctx, chromedp.SetUploadFiles(chromedp.CSS("#file"), []string{path}))
	waitEditor(t, ctx, `document.querySelectorAll('.balloon').length>0 && !document.querySelector('#exportSvg').disabled`)
	browserDo(t, ctx, chromedp.Evaluate[chromedp.Void](`window.__svg=null; const create=URL.createObjectURL; URL.createObjectURL=function(blob){blob.text().then(s=>window.__svg=s);return create.call(this,blob)};`), chromedp.Click(chromedp.CSS("#exportSvg")))
	waitEditor(t, ctx, `window.__svg!==null`)
	svg := browserValue[string](t, ctx, `window.__svg`)
	if !strings.Contains(svg, "data:image/png;base64,") {
		t.Fatal("SVG is missing the original PDF drawing background")
	}
}

func TestBrowserPDFExportAvailable(t *testing.T) {
	ctx := editorBrowser(t)
	loadEditorDemo(t, ctx)
	if !browserValue[bool](t, ctx, `!!document.querySelector('#exportPdf')`) {
		t.Fatal("missing Export PDF action")
	}
}
