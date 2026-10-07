package api

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBalloonColorSurvivesEditsAndSVGExport(t *testing.T) {
	for _, test := range []struct{ name, hex string }{{"red", "#c62828"}, {"black", "#1a1a1a"}, {"green", "#16803c"}, {"invalid", "#1a1a1a"}} {
		t.Run(test.name, func(t *testing.T) {
			demo := do(t, "GET", "/api/demo", nil)
			var payload map[string]json.RawMessage
			if err := json.Unmarshal(demo.Body.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
			var drawing map[string]any
			if err := json.Unmarshal(payload["drawing"], &drawing); err != nil {
				t.Fatal(err)
			}
			drawing["balloon_color"] = test.name
			res := do(t, "POST", "/api/layout", map[string]any{"drawing": drawing, "page": 0})
			if res.Code != 200 {
				t.Fatalf("layout rejected drawing color: %s", res.Body.String())
			}
			if err := json.Unmarshal(res.Body.Bytes(), &drawing); err != nil {
				t.Fatal(err)
			}
			res = do(t, "POST", "/api/build", map[string]any{"drawing": drawing, "texts": payload["texts"]})
			if res.Code != 200 {
				t.Fatal(res.Body.String())
			}
			if err := json.Unmarshal(res.Body.Bytes(), &drawing); err != nil {
				t.Fatal(err)
			}
			if drawing["balloon_color"] != test.name {
				t.Fatal("color lost on re-read")
			}
			svg := do(t, "POST", "/api/export.svg", map[string]any{"drawing": drawing, "page": 0})
			if svg.Code != 200 {
				t.Fatal(svg.Body.String())
			}
			// Every balloon circle and leader line must use the drawing color,
			// even when a placement has a warning.
			for _, tag := range strings.Split(svg.Body.String(), "<") {
				if strings.HasPrefix(tag, "line ") || strings.HasPrefix(tag, "circle ") {
					if strings.Contains(tag, "stroke=") && !strings.Contains(tag, `stroke="`+test.hex+`"`) {
						t.Errorf("unexpected stroke: %s", tag)
					}
					if strings.Contains(tag, `r="1.6"`) && !strings.Contains(tag, `fill="`+test.hex+`"`) {
						t.Errorf("unexpected leader dot: %s", tag)
					}
				}
			}
		})
	}
}
