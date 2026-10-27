//go:build cgo

// EE DLA post-processor override.
//
// Compiled in BOTH the OSS and EE cgo builds (only gated by `cgo`, never by a
// feature tag): it only defines dlaPostprocessEE, which is idle until
// InitCPUEE (see inference_ee.go) assigns it to the shared dlaPostprocessFn
// seam declared in dla.go. The OSS build never calls InitCPUEE, so the OSS
// post-processor is preserved.
//
// Why EE differs from OSS here (by design, not a bug):
//   - The OSS Python layout recognizer (deepdoc/vision/layout_recognizer.py
//     LayoutRecognizer4YOLOv10) runs per-class NMS @0.45 and gates only
//     *garbage* layout classes at 0.4. See dla.go's dlaPostprocess for that path.
//   - The EE Python DLA service (deepdoc/servers/dla/dla_svr.py, ONNX/CPU path)
//     runs NO NMS (the layout.ort graph has no NMS operator and the serving
//     path applies none) and applies a flat 0.4 confidence gate on EVERY class
//     on top of the engine's internal 0.25 filter. Re-running our own NMS here
//     would double-suppress overlapping boxes the EE service keeps, diverging
//     the box count and the chosen candidate from the EE wire output.

package native

import (
	"sort"
	"strings"
)

// dlaPostprocessEE is the EE post-processor: it takes the raw model output
// as-is (NO NMS — the layout.ort graph and the EE serving path apply none) and
// applies a flat 0.4 confidence gate on EVERY class on top of the engine's
// internal 0.25 filter, mirroring deepdoc/servers/dla/dla_svr.py.
func dlaPostprocessEE(out []float32, sf [4]float32) DLAResult {
	const scoreThr = 0.08
	type cand struct {
		nmsBox
		cls int
	}
	cands := make([]cand, 0, dlaMaxBoxes)
	for i := 0; i < dlaMaxBoxes; i++ {
		base := i * 6
		score := out[base+4]
		if score <= scoreThr {
			continue
		}
		// Truncate toward zero, matching deepdoc LayoutRecognizer4YOLOv10.postprocess
		// (boxes[:, -1].astype(int)). The prior int(x+0.5) rounding shifted
		// class-channel values in [2.5, 2.999] from class 2 to class 3.
		cls := int(out[base+5])
		cands = append(cands, cand{
			nmsBox: nmsBox{
				X0:    (out[base+0] - sf[2]) * sf[0],
				Y0:    (out[base+1] - sf[3]) * sf[1],
				X1:    (out[base+2] - sf[2]) * sf[0],
				Y1:    (out[base+3] - sf[3]) * sf[1],
				Score: score,
			},
			cls: cls,
		})
	}

	// No NMS: the EE Python DLA service (dla_svr.py, ONNX/CPU path) applies
	// NONE — it takes the raw model output and only filters by score. The
	// layout.ort graph has no NMS operator, so there is nothing to "undo" here.
	res := DLAResult{}
	for _, c := range cands {
		res.Boxes = append(res.Boxes, DLABox{
			X0: round2(c.nmsBox.X0), Y0: round2(c.nmsBox.Y0),
			X1: round2(c.nmsBox.X1), Y1: round2(c.nmsBox.Y1),
			Score: round4(c.nmsBox.Score), Class: c.cls,
		})
	}
	// Re-map class ids through the OSS label->Go index map (identical class
	// vocabulary in EE).
	mapped := res.Boxes[:0]
	for _, b := range res.Boxes {
		// Guard the raw YOLO class index before the slice lookup: the model
		// output column is the integer class id, but an out-of-range or
		// negative value would panic on yoloDlaLabels[b.Class] and take the
		// server process down. Drop the box instead, mirroring the bestCls
		// bounds guard in tsr.go.
		if b.Class < 0 || b.Class >= len(yoloDlaLabels) {
			continue
		}
		label := yoloDlaLabels[b.Class]
		goCls, ok := dlaClassMap[strings.ToLower(label)]
		if !ok {
			continue
		}
		b.Class = goCls
		mapped = append(mapped, b)
	}
	res.Boxes = mapped

	// Flat 0.4 confidence gate on EVERY class, mirroring dla_svr.py
	// encode_response (filtered = [d for d in dets if len(d) > 4 and d[4] > 0.4]).
	// Without it the Go server would emit boxes all the way down to the 0.08
	// candidate threshold and diverge from the EE wire output (extra low-confidence
	// layout boxes).
	kept := res.Boxes[:0]
	for _, b := range res.Boxes {
		if b.Score > 0.4 {
			kept = append(kept, b)
		}
	}
	res.Boxes = kept

	// Deterministic ordering: dlaPostprocess iterates a class->index map, whose
	// iteration order is unspecified in Go. Sort so identical detections always
	// serialize identically (e.g. for stable Wire() across runs / session reuse).
	sort.Slice(res.Boxes, func(i, j int) bool {
		a, b := res.Boxes[i], res.Boxes[j]
		if a.Class != b.Class {
			return a.Class < b.Class
		}
		if a.X0 != b.X0 {
			return a.X0 < b.X0
		}
		if a.Y0 != b.Y0 {
			return a.Y0 < b.Y0
		}
		if a.X1 != b.X1 {
			return a.X1 < b.X1
		}
		if a.Y1 != b.Y1 {
			return a.Y1 < b.Y1
		}
		return a.Score < b.Score
	})
	return res
}
