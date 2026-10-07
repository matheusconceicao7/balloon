// Browser-only document exports. The source PDF stays on the user's machine.
import {
  PDFDocument, StandardFonts, rgb,
  pushGraphicsState, popGraphicsState, concatTransformationMatrix,
} from './vendor/pdf-lib.mjs';

const palette = { red: '#c62828', black: '#1a1a1a', green: '#16803c' };
const color = (hex) => rgb(...hex.match(/[a-f\d]{2}/gi).map(v => parseInt(v, 16) / 255));

// Copy the original document, retaining its vectors, text, page boxes and
// rotation. Only the annotation layer is added; nothing is reconstructed from
// extracted measurements or from the screen's zoom-dependent canvas.
export async function exportPDF(pdf, drawing) {
  const doc = await PDFDocument.load(await pdf.getData());
  const font = await doc.embedFont(StandardFonts.Helvetica);
  const ink = color(palette[drawing.balloon_color] || palette.black);
  for (const sheet of drawing.pages) {
    const items = drawing.items.filter(it => it.page === sheet.index);
    if (!items.length) continue;
    const source = await pdf.getPage(sheet.index + 1);
    const vp = source.getViewport({ scale: 1 });
    const page = doc.getPage(sheet.index);
    // Invert PDF.js's viewport matrix, then flip our bottom-left overlay to
    // viewport coordinates. Handles CropBox offsets, rotation and UserUnit.
    const [a, b, c, d, e, f] = vp.transform;
    const det = a * d - b * c;
    const ia = d / det, ib = -b / det, ic = -c / det, id = a / det;
    const ie = (c * f - d * e) / det, iff = (b * e - a * f) / det;
    page.pushOperators(pushGraphicsState(), concatTransformationMatrix(
      ia, ib, -ic, -id, ic * vp.height + ie, id * vp.height + iff,
    ));
    const point = p => ({ x: p.x, y: vp.height - p.y });
    for (const it of items) {
      const { a: start, b: end } = it.leader;
      if (start.x === end.x && start.y === end.y) continue;
      page.drawLine({ start: point(start), end: point(end), thickness: 0.8, color: ink });
      page.drawCircle({ ...point(start), size: 1.6, color: ink });
    }
    for (const it of items) {
      const { c: center, r } = it.balloon;
      const p = point(center);
      page.drawCircle({ ...p, size: r, borderWidth: 1.1, borderColor: ink,
        color: color(it.clean ? '#ffffff' : '#fff8e1') });
      const label = String(it.number), size = r * 1.05;
      page.drawText(label, { x: p.x - font.widthOfTextAtSize(label, size) / 2,
        y: p.y - r * 0.36, size, font, color: ink });
    }
    page.pushOperators(popGraphicsState());
  }
  return new Blob([await doc.save()], { type: 'application/pdf' });
}

export async function includePDFBackground(svg, pdf, pageIndex) {
  const page = await pdf.getPage(pageIndex + 1);
  const natural = page.getViewport({ scale: 1 });
  // 144 dpi normally; cap large sheets to avoid exceeding canvas limits.
  const scale = Math.min(2, 8192 / Math.max(natural.width, natural.height),
    Math.sqrt(16_000_000 / (natural.width * natural.height)));
  const vp = page.getViewport({ scale });
  const canvas = document.createElement('canvas');
  canvas.width = Math.ceil(vp.width);
  canvas.height = Math.ceil(vp.height);
  try {
    await page.render({ canvasContext: canvas.getContext('2d'), viewport: vp }).promise;
    const xml = new DOMParser().parseFromString(svg, 'image/svg+xml');
    if (xml.querySelector('parsererror')) throw new Error('Invalid SVG export');
    const image = xml.createElementNS('http://www.w3.org/2000/svg', 'image');
    image.setAttribute('width', natural.width);
    image.setAttribute('height', natural.height);
    image.setAttribute('preserveAspectRatio', 'none');
    image.setAttribute('href', canvas.toDataURL('image/png'));
    xml.documentElement.prepend(image);
    return new Blob([new XMLSerializer().serializeToString(xml)], { type: 'image/svg+xml' });
  } finally {
    canvas.width = canvas.height = 0;
  }
}
