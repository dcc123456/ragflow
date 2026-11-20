//go:build cgo && ee

package native

// tsr_ee.go — EE Table Structure Recognition (TSR) recognizer.
//
// EE replaces the open-source 6-class PP-Det TableStructureRecognizer with a
// 2-class YOLOv8 separator model (best.20250908.pt, exported to tsr.onnx →
// tsr.ort). The open-source 6-class TSR (table / column / row / …) lives in
// tsr.go and is retired dead code in EE; RunTSREE in this file is the EE
// replacement and is what the EE deepdoc service's /predict/tsr endpoint calls.
//
// The two classes are separator lines, matching the EE Python inference service
// (deepdoc/servers/tsr/tsr_svr.py), which emits:
//
//	{"bboxes": [[x0, y0, x1, y1, score, cls], ...]}   cls ∈ {0, 1}
//
//   - class 0 = "v" (vertical separator)
//   - class 1 = "h" (horizontal separator)
//
// The decoding was validated box-for-box against the live EE service on
// table.jpg: 382/382 golden boxes matched within ~1.3px / 0.05 score, class
// distribution v≈359 / h≈23, identical score-descending order (per-class
// max_det cap, see tsrEEMaxDetPerClass). See the parity fixture testdata/tsr_ee_table.*.

import (
	"context"
	"encoding/json"
	"math"
	"path/filepath"
	"sort"
)

const (
	tsrEEInputSize  = 640
	tsrEECandidates = 8400
	// tsrEEMaxDetPerClass caps the number of kept boxes PER CLASS (v and h
	// independently) AFTER per-class NMS in tsrEEPostprocess. The EE 2-class
	// TSR model emits far more vertical (v) separators than horizontal (h)
	// ones; a single GLOBAL cap (ultralytics' default behaviour) lets the
	// high-scoring, numerous v-lines crowd the lower-scoring h-lines out of
	// the top-K — the "horizontal-line starvation" defect. Capping per class
	// keeps the minority h-class from being starved by a shared quota.
	//
	// NOTE: ultralytics applies max_det GLOBALLY (offset-trick per-class NMS
	// then a single i[:max_det]); Go intentionally diverges here to protect h.
	// 600 is comfortably above the observed per-class counts (v≈359, h≈23 on
	// the parity fixture) so no real detection is truncated. Lowering this
	// below the actual per-class count WILL change Go output and requires
	// re-capturing tsr_ee_table.golden.json (from Go, since the Python
	// reference is being removed) and updating the integration-test bounds.
	tsrEEMaxDetPerClass = 600  // per-class cap; ultralytics default is global 300
	tsrEEConfThr        = 0.25 // ultralytics default conf_thres
	tsrEEIouThr         = 0.7  // ultralytics default iou_thres (agnostic=False)
)

// tsrEELabels maps class id → separator kind. Index == cls.
var tsrEELabels = []string{"v", "h"}

// tsrEEClassMap maps the separator kind back to the wire class id (mirrors the
// EE Python service output).
var tsrEEClassMap = map[string]int{"v": 0, "h": 1}

// TSREEBox is one detected table separator line in original pixel coordinates.
type TSREEBox struct {
	Label               string
	Score               float32
	X0, X1, Top, Bottom float32
}

// TSREEResult is the set of detected separator lines for one table region.
// W/H are the source image dimensions, used to clamp boxes into bounds.
type TSREEResult struct {
	Boxes []TSREEBox
	W, H  int
}

// RunTSREE runs EE table-structure recognition (2-class YOLOv8 separator model)
// on a cropped table image. It loads tsr.ee.cpu.ort (EE 2-class) and returns boxes in
// the EE wire format {"bboxes": [[x0, y0, x1, y1, score, cls], ...]}.
func RunTSREE(ctx context.Context, modelDir string, img *Image) (TSREEResult, error) {
	blob, lb := tsrEEPreprocess(img)
	// EE ships its 2-class YOLOv8 separator model. In EE the OSS 6-class
	// TableStructureRecognizer (RunTSR) is retired dead code, so the EE 2-class
	// weights are served under the canonical name tsr.ee.cpu.ort (the EE
	// deployment's own 2-class weights, distinct from the OSS 6-class model). Go
	// loads the ORT export
	// directly; the minimal static ORT build consumes .ort only (same contract as
	// OSS), so the runtime model matches the golden reference source
	// (tsr_op17.onnx, converted to .ort).
	sess, release, err := getModelSession(ctx, filepath.Join(modelDir, "tsr.ee.cpu.ort"), "images",
		[]int64{1, 3, tsrEEInputSize, tsrEEInputSize}, "output0")
	if err != nil {
		return TSREEResult{}, err
	}
	defer release()

	out, err := sess.Run(ctx, blob)
	if err != nil {
		return TSREEResult{}, err
	}
	if err := checkOutputLength("tsr", len(out), 6*tsrEECandidates); err != nil {
		return TSREEResult{}, err
	}
	res := tsrEEPostprocess(out, lb)
	res.W, res.H = img.W, img.H
	return res, nil
}

// tsrEELetterbox holds the letterbox parameters needed to map model (640-space)
// coordinates back to source pixels: original = (coord - halfpad) / ratio.
type tsrEELetterbox struct {
	ratio  float32
	dw, dh float32
}

// tsrEEPreprocess letterboxes the image (aspect-preserving, 114-gray padding, BGR
// channel order) into the 640×640 input the EE YOLOv8 TSR model expects, and
// returns the CHW float blob (/255) plus the letterbox parameters for the inverse
// transform.
//
// Channel order MUST match the Python EE docker (deepdoc/servers/tsr/tsr_svr.py):
// that service does cv2.cvtColor(BGR, BGR2RGB) on the decoded image and then
// feeds the RGB array to ultralytics, which applies BGR2RGB once more internally
// — so the model actually receives BGR-order pixels. Go's image decoder yields
// RGB (image.go), so we call img.ToBGR() to land on the same BGR order the EE
// docker's model sees. (This is also the convention used by the OSS TSR path in
// tsr_decode.go.) Feeding RGB instead was a parity divergence: Go then detected
// horizontal separators that the docker suppressed. Coordinates are restored with
// original = (640coord - halfpad) / ratio.
func tsrEEPreprocess(img *Image) (blob []float32, lb tsrEELetterbox) {
	r := float32(math.Min(float64(tsrEEInputSize)/float64(img.W), float64(tsrEEInputSize)/float64(img.H)))
	newW := int(math.Round(float64(img.W) * float64(r)))
	newH := int(math.Round(float64(img.H) * float64(r)))
	dw := (float64(tsrEEInputSize) - float64(newW)) / 2.0
	dh := (float64(tsrEEInputSize) - float64(newH)) / 2.0
	lb = tsrEELetterbox{ratio: r, dw: float32(dw), dh: float32(dh)}

	resized := bilinearResize(img.ToBGR(), img.W, img.H, newW, newH)
	top := int(math.Round(dh - 0.1))
	left := int(math.Round(dw - 0.1))
	blob = tsrEELetterboxBlob(resized, newW, newH, top, left)
	return blob, lb
}

// tsrEELetterboxBlob places the already-resized BGR raster (newH*newW*3,
// row-major, as produced by img.ToBGR()) into the 640×640 canvas with 114-filled
// borders and returns the CHW float blob (/255). Channel order is B,G,R to match
// the EE docker's effective color order (see tsrEEPreprocess).
func tsrEELetterboxBlob(resized []byte, newW, newH, top, left int) []float32 {
	const S = tsrEEInputSize
	blob := make([]float32, 3*S*S)
	for y := 0; y < S; y++ {
		for x := 0; x < S; x++ {
			var b0, b1, b2 float32 = 114, 114, 114
			inY, inX := y-top, x-left
			if inY >= 0 && inY < newH && inX >= 0 && inX < newW {
				o := (inY*newW + inX) * 3
				b0 = float32(resized[o])   // B (channel 0)
				b1 = float32(resized[o+1]) // G (channel 1)
				b2 = float32(resized[o+2]) // R (channel 2)
			}
			i := y*S + x
			blob[0*S*S+i] = b0 / 255.0
			blob[1*S*S+i] = b1 / 255.0
			blob[2*S*S+i] = b2 / 255.0
		}
	}
	return blob
}

// tsrEEPostprocess decodes the EE YOLOv8 TSR output [1, 6, 8400] (channels-first:
// out[c*8400 + a]) into separator boxes.
//
//	out[0:4] = (cx, cy, w, h) in 640-space PIXELS (the export already emits
//	          pixel coordinates — no scale_factor multiply, unlike PP-Det).
//	out[4:6] = class probabilities, already sigmoided by the ONNX export.
//
// Decoding mirrors ultralytics YOLO predict: per-class NMS (agnostic=False,
// iou=0.7 — equivalent to its offset-trick single NMS call), then concat
// survivors, then keep up to tsrEEMaxDetPerClass boxes per class. Boxes are
// un-letterboxed back to source pixels and rounded (matching tsr_svr.py's
// bboxes.round().int()).
func tsrEEPostprocess(out []float32, lb tsrEELetterbox) TSREEResult {
	type cand struct {
		nmsBox
		cls int
	}
	cands := make([]cand, 0, tsrEECandidates)
	for a := 0; a < tsrEECandidates; a++ {
		p0 := out[4*tsrEECandidates+a]
		p1 := out[5*tsrEECandidates+a]
		score, cls := p0, 0
		if p1 > p0 {
			score, cls = p1, 1
		}
		if score <= tsrEEConfThr {
			continue
		}
		cx := out[0*tsrEECandidates+a]
		cy := out[1*tsrEECandidates+a]
		w := out[2*tsrEECandidates+a]
		h := out[3*tsrEECandidates+a]
		x0 := cx - w/2
		y0 := cy - h/2
		x1 := cx + w/2
		y1 := cy + h/2
		// un-letterbox: original = (640coord - halfpad) / ratio
		x0 = (x0 - lb.dw) / lb.ratio
		y0 = (y0 - lb.dh) / lb.ratio
		x1 = (x1 - lb.dw) / lb.ratio
		y1 = (y1 - lb.dh) / lb.ratio
		cands = append(cands, cand{
			nmsBox: nmsBox{X0: x0, Y0: y0, X1: x1, Y1: y1, Score: score},
			cls:    cls,
		})
	}

	// Per-class NMS (agnostic=False), iou=0.7 — equivalent to ultralytics' single
	// offset-trick NMS call (cross-class boxes never suppress each other).
	byClass := map[int][]int{}
	for i, c := range cands {
		byClass[c.cls] = append(byClass[c.cls], i)
	}
	// Per-class cap: after per-class NMS, keep at most tsrEEMaxDetPerClass
	// boxes for EACH class independently. This stops the numerous,
	// high-scoring v-lines from starving the h-lines under one shared cap.
	keep := make([]cand, 0, len(cands))
	for cls, idxs := range byClass {
		sub := make([]nmsBox, len(idxs))
		for k, i := range idxs {
			sub[k] = cands[i].nmsBox
		}
		survived := nms(sub, tsrEEIouThr, false)
		clsKept := make([]cand, 0, len(survived))
		for _, k := range survived {
			clsKept = append(clsKept, cand{nmsBox: sub[k], cls: cls})
		}
		// highest-scoring first, then per-class truncation
		sort.Slice(clsKept, func(i, j int) bool { return clsKept[i].Score > clsKept[j].Score })
		if len(clsKept) > tsrEEMaxDetPerClass {
			clsKept = clsKept[:tsrEEMaxDetPerClass]
		}
		keep = append(keep, clsKept...)
	}
	// stable global ordering for deterministic output (no truncation here)
	sort.Slice(keep, func(i, j int) bool { return keep[i].Score > keep[j].Score })

	boxes := make([]TSREEBox, 0, len(keep))
	for _, c := range keep {
		boxes = append(boxes, TSREEBox{
			Label:  tsrEELabels[c.cls],
			Score:  round4(c.nmsBox.Score),
			X0:     round2(c.nmsBox.X0),
			X1:     round2(c.nmsBox.X1),
			Top:    round2(c.nmsBox.Y0),
			Bottom: round2(c.nmsBox.Y1),
		})
	}
	return TSREEResult{Boxes: boxes}
}

// Wire emits the EE TSR wire format:
// {"bboxes": [[x0, y0, x1, y1, score, cls], ...]} with cls ∈ {0,1}.
func (r TSREEResult) Wire() string {
	rows := make([][]float32, 0, len(r.Boxes))
	w, h := float32(r.W), float32(r.H)
	for _, b := range r.Boxes {
		cls, ok := tsrEEClassMap[b.Label]
		if !ok {
			continue
		}
		// Clamp into image bounds (mirrors tsr_svr.py downstream clamping).
		x0 := minf(maxf(b.X0, 0), w)
		x1 := minf(maxf(b.X1, 0), w)
		top := minf(maxf(b.Top, 0), h)
		bot := minf(maxf(b.Bottom, 0), h)
		rows = append(rows, []float32{x0, top, x1, bot, b.Score, float32(cls)})
	}
	out, _ := json.Marshal(map[string]any{"bboxes": rows})
	return string(out)
}

func maxf(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

func minf(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}
