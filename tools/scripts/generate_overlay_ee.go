//go:build cgo && ee

// Command generate_overlay_ee runs the EE 2-class TSR Go port (RunTSREE) on the
// parity fixture, matches its output against the EE Python golden using the SAME
// tolerance logic as the parity test (MatchBoxesRelaxed: coord tol 3.5px, score
// tol 0.05), and renders a PNG that highlights the divergent separators so a
// human reviewer can eyeball them.
//
// Output:
//   - <out>            : full-table overlay (all boxes faint, 2 divergent bold)
//   - <out>_zoom1.png  : tight crop around divergent #1
//   - <out>_zoom2.png  : tight crop around divergent #2
//
// Build/run (CGO + static ORT, same env as the parity tests):
//
//	V=~/ragflow-native-libs/onnxruntime/static_lib/v1.29.0-2da6c6ac8165a16e
//	DL="$(pwd)/.cache/ort_dynamic_list.txt"; printf '{\n  OrtGetApiBase;\n};\n' > "$DL"
//	export MODEL_DIR=/tmp/eemodel_ee CGO_ENABLED=1
//	export CGO_LDFLAGS="-Wl,--undefined=OrtGetApiBase -Wl,--dynamic-list=$DL $V/libonnxruntime.a -lstdc++ -lm -lpthread -ldl -lrt -lgcc_s -lutil -lc"
//	go run -tags cgo ./tools/scripts/generate_overlay_ee.go -out docs/develop/ee_tsr_ee_overlay.png
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"path/filepath"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"

	"ragflow/internal/deepdoc/native"
)

type box = []float64 // [x0,y0,x1,y1,score,cls]

type divInfo struct {
	label  string
	golden box
	goBox  box // nearest same-class Go box (may be the ~40px-off one)
	gIdx   int
	goIdx  int
}

func main() {
	modelDir := flag.String("modeldir", os.Getenv("MODEL_DIR"), "DEEPDOC_MODEL_DIR with tsr.ee.cpu.onnx")
	fixture := flag.String("fixture", "internal/deepdoc/native/testdata/tsr_ee_parity/tsr_ee_table.png", "fixture image")
	golden := flag.String("golden", "internal/deepdoc/native/testdata/tsr_ee_parity/tsr_ee_table.golden.json", "golden json")
	out := flag.String("out", "docs/develop/ee_tsr_ee_overlay.png", "output PNG (full overlay)")
	coordTol := flag.Float64("coordtol", 3.5, "coordinate tolerance (px), mirrors parity CmpTolCoord")
	scoreTol := flag.Float64("scoretol", 0.05, "score tolerance, mirrors parity CmpTolScore")
	flag.Parse()

	if *modelDir == "" {
		fmt.Fprintln(os.Stderr, "MODEL_DIR empty: set -modeldir or env MODEL_DIR")
		os.Exit(2)
	}

	// ONNX Runtime is statically linked; the process-global env must be
	// initialized once before any session Run (same as the parity tests).
	if err := native.InitORT(); err != nil {
		fmt.Fprintf(os.Stderr, "InitORT: %v\n", err)
		os.Exit(1)
	}

	// --- run the real Go port ---
	img, err := native.Decode(*fixture)
	if err != nil {
		fmt.Fprintf(os.Stderr, "decode fixture: %v\n", err)
		os.Exit(1)
	}
	res, err := native.RunTSREE(context.Background(), *modelDir, img)
	if err != nil {
		fmt.Fprintf(os.Stderr, "RunTSREE: %v\n", err)
		os.Exit(1)
	}
	var goWire struct {
		Bboxes [][]float64 `json:"bboxes"`
	}
	if err := json.Unmarshal([]byte(res.Wire()), &goWire); err != nil {
		fmt.Fprintf(os.Stderr, "parse Go wire: %v\n", err)
		os.Exit(1)
	}
	goBoxes := goWire.Bboxes

	// --- load golden ---
	goldBoxes := loadGolden(*golden)
	if len(goldBoxes) == 0 || len(goBoxes) == 0 {
		fmt.Fprintln(os.Stderr, "empty boxes")
		os.Exit(1)
	}

	// --- find divergent (same logic as MatchBoxesRelaxed) ---
	divergent := findDivergent(goldBoxes, goBoxes, *coordTol, *scoreTol)

	fmt.Printf("golden=%d  go=%d  divergent=%d\n", len(goldBoxes), len(goBoxes), len(divergent))
	for _, d := range divergent {
		gc := center(d.golden)
		if d.goBox == nil {
			fmt.Printf("  %s golden=(%d,%d) size=(%.0fx%.0f) cls=%.0f  [NO matching Go box of same class]\n",
				d.label, gc.X, gc.Y, d.golden[2]-d.golden[0], d.golden[3]-d.golden[1], d.golden[5])
			continue
		}
		vc := center(d.goBox)
		dx, dy := gc.X-vc.X, gc.Y-vc.Y
		fmt.Printf("  %s golden=(%d,%d) go=(%d,%d) offset=(%d,%d) dist=%.1fpx cls=%.0f\n",
			d.label, gc.X, gc.Y, vc.X, vc.Y, dx, dy, math.Hypot(float64(dx), float64(dy)), d.golden[5])
	}

	// --- background image (original coords, same space as boxes) ---
	bg, err := decodePNG(*fixture)
	if err != nil {
		fmt.Fprintf(os.Stderr, "decode png for drawing: %v\n", err)
		os.Exit(1)
	}

	// full overlay
	if err := render(*out, bg, image.Point{}, bg.Bounds().Size(), goldBoxes, goBoxes, divergent, true); err != nil {
		fmt.Fprintf(os.Stderr, "render full: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s\n", *out)

	// zoomed crops around each divergent point
	for i, d := range divergent {
		if i >= 2 {
			break
		}
		c := center(d.golden)
		half := 140 // ~280px crop
		origin := image.Point{X: int(c.X) - half, Y: int(c.Y) - half}
		size := image.Point{X: 2 * half, Y: 2 * half}
		// clamp to image
		if origin.X < 0 {
			origin.X = 0
		}
		if origin.Y < 0 {
			origin.Y = 0
		}
		if origin.X+size.X > bg.Bounds().Dx() {
			size.X = bg.Bounds().Dx() - origin.X
		}
		if origin.Y+size.Y > bg.Bounds().Dy() {
			size.Y = bg.Bounds().Dy() - origin.Y
		}
		zpath := fmt.Sprintf("%s_zoom%d.png", trimExt(*out), i+1)
		if err := render(zpath, bg, origin, size, goldBoxes, goBoxes, []divInfo{d}, false); err != nil {
			fmt.Fprintf(os.Stderr, "render zoom%d: %v\n", i+1, err)
			os.Exit(1)
		}
		fmt.Printf("wrote %s\n", zpath)
	}
}

// ---------- matching (mirrors MatchBoxesRelaxed) ----------

func findDivergent(goldens, gos []box, coordTol, scoreTol float64) []divInfo {
	used := make([]bool, len(gos))
	var out []divInfo
	for gi, g := range goldens {
		cls := int(g[5])
		gcx, gcy := (g[0]+g[2])/2, (g[1]+g[3])/2
		best, bd := -1, math.MaxFloat64
		for i, v := range gos {
			if used[i] || int(v[5]) != cls {
				continue
			}
			vcx, vcy := (v[0]+v[2])/2, (v[1]+v[3])/2
			d := (gcx-vcx)*(gcx-vcx) + (gcy-vcy)*(gcy-vcy)
			if d < bd {
				bd, best = d, i
			}
		}
		if best < 0 {
			out = append(out, divInfo{label: fmt.Sprintf("#%d", gi), golden: g, goBox: nil, gIdx: gi, goIdx: -1})
			continue
		}
		coordDiff := 0.0
		for j := 0; j < 4; j++ {
			coordDiff = math.Max(coordDiff, math.Abs(g[j]-gos[best][j]))
		}
		scoreDiff := math.Abs(g[4] - gos[best][4])
		if coordDiff > coordTol || scoreDiff > scoreTol {
			out = append(out, divInfo{label: fmt.Sprintf("#%d", gi), golden: g, goBox: gos[best], gIdx: gi, goIdx: best})
		}
	}
	return out
}

// ---------- drawing ----------

var (
	faintBlue  = color.NRGBA{80, 140, 255, 70}
	faintGreen = color.NRGBA{80, 200, 120, 55}
	red        = color.NRGBA{255, 40, 40, 255}
	orange     = color.NRGBA{255, 165, 0, 255}
	yellow     = color.NRGBA{255, 230, 0, 255}
)

func render(path string, bg image.Image, origin, size image.Point, goldens, gos []box, div []divInfo, legend bool) error {
	canvas := image.NewRGBA(image.Rect(0, 0, size.X, size.Y))
	draw.Draw(canvas, canvas.Bounds(), bg, origin, draw.Src)

	// faint context: all boxes
	for _, g := range goldens {
		if visible(g, origin, size) {
			outline(canvas, rectOf(g, origin), faintBlue, 1)
		}
	}
	for _, v := range gos {
		if visible(v, origin, size) {
			outline(canvas, rectOf(v, origin), faintGreen, 1)
		}
	}

	// bold divergent
	for _, d := range div {
		gr := rectOf(d.golden, origin)
		outline(canvas, gr, red, 3)
		if d.goBox != nil {
			vr := rectOf(d.goBox, origin)
			outline(canvas, vr, orange, 3)
			drawLine(canvas, center(d.golden).Sub(origin), center(d.goBox).Sub(origin), yellow, 2)
		}
		labelPt := center(d.golden).Sub(origin)
		drawLabel(canvas, labelPt.X, labelPt.Y-8, d.label, red)
	}

	if legend {
		drawLegend(canvas)
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, canvas)
}

func drawLegend(c *image.RGBA) {
	lines := []string{
		"EE TSR parity overlay",
		"faint blue = golden (300)",
		"faint green = Go RunTSREE (300)",
		"bold red = divergent golden",
		"bold orange = nearest Go box (~40px off)",
		"yellow line = golden->Go offset",
	}
	x, y := 12, 18
	for _, l := range lines {
		drawLabel(c, x, y, l, color.NRGBA{255, 255, 255, 255})
		y += 16
	}
}

func visible(b box, origin image.Point, size image.Point) bool {
	cx, cy := int((b[0]+b[2])/2)-origin.X, int((b[1]+b[3])/2)-origin.Y
	return cx >= 0 && cx < size.X && cy >= 0 && cy < size.Y
}

func rectOf(b box, origin image.Point) image.Rectangle {
	x0 := int(b[0]) - origin.X
	y0 := int(b[1]) - origin.Y
	x1 := int(b[2]) - origin.X
	y1 := int(b[3]) - origin.Y
	return image.Rect(x0, y0, x1, y1)
}

func center(b box) image.Point {
	return image.Point{X: int((b[0] + b[2]) / 2), Y: int((b[1] + b[3]) / 2)}
}

func outline(dst *image.RGBA, r image.Rectangle, c color.Color, t int) {
	if t < 1 {
		t = 1
	}
	fill(dst, image.Rect(r.Min.X, r.Min.Y, r.Max.X, r.Min.Y+t), c) // top
	fill(dst, image.Rect(r.Min.X, r.Max.Y-t, r.Max.X, r.Max.Y), c) // bottom
	fill(dst, image.Rect(r.Min.X, r.Min.Y, r.Min.X+t, r.Max.Y), c) // left
	fill(dst, image.Rect(r.Max.X-t, r.Min.Y, r.Max.X, r.Max.Y), c) // right
}

func fill(dst *image.RGBA, r image.Rectangle, c color.Color) {
	draw.Draw(dst, r, &image.Uniform{c}, image.Point{}, draw.Src)
}

func drawLine(dst *image.RGBA, a, b image.Point, c color.Color, t int) {
	// Bresenham with thickness
	x0, y0, x1, y1 := a.X, a.Y, b.X, b.Y
	dx := abs(x1 - x0)
	dy := -abs(y1 - y0)
	sx, sy := 1, 1
	if x0 >= x1 {
		sx = -1
	}
	if y0 >= y1 {
		sy = -1
	}
	err := dx + dy
	for {
		fill(dst, image.Rect(x0-t/2, y0-t/2, x0-t/2+t, y0-t/2+t), c)
		if x0 == x1 && y0 == y1 {
			break
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x0 += sx
		}
		if e2 <= dx {
			err += dx
			y0 += sy
		}
	}
}

func drawLabel(dst *image.RGBA, x, y int, text string, c color.Color) {
	d := &font.Drawer{
		Dst:  dst,
		Src:  image.NewUniform(c),
		Face: basicfont.Face7x13,
		Dot:  fixed.P(x, y),
	}
	// background strip for readability
	bg := image.NewRGBA(image.Rect(x-2, y-13, x+len(text)*7+2, y+2))
	fill(bg, bg.Bounds(), color.NRGBA{0, 0, 0, 170})
	draw.Draw(dst, bg.Bounds(), bg, image.Point{}, draw.Over)
	d.DrawString(text)
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// ---------- io helpers ----------

func decodePNG(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return png.Decode(f)
}

func loadGolden(path string) []box {
	raw, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read golden: %v\n", err)
		os.Exit(1)
	}
	var wrap struct {
		Bboxes [][]float64 `json:"bboxes"`
	}
	if err := json.Unmarshal(raw, &wrap); err != nil {
		fmt.Fprintf(os.Stderr, "parse golden: %v\n", err)
		os.Exit(1)
	}
	return wrap.Bboxes
}

func trimExt(p string) string {
	ext := filepath.Ext(p)
	return p[:len(p)-len(ext)]
}
