//go:build cgo && ee

// EE DeepDoc runtime activation.
//
// This file is compiled in BOTH the OSS and EE cgo builds (only gated by
// `cgo`, never by a feature tag): it only defines InitCPUEE, which is never
// called in the OSS build, so the OSS default seam values are preserved and
// OSS behaviour is unchanged. The EE entry point (cmd/deepdoc_server_ee.go's
// runDeepDoc, reached via ragflow_server --deepdoc) calls InitCPUEE once at
// startup, before any inference, to switch the in-process DeepDoc backend to
// the EE weights and post-processors.
//
// Every field assigned here is an override seam declared in the shared OSS
// files (det_core.go / dla.go / ocr_rec.go / native_analyzer.go) as a
// package-level variable. EE does NOT fork those files; it only reassigns
// their seams here.
package native

import "ragflow/internal/common"

// InitCPUEE points the in-process DeepDoc backend at the EE deployment's own
// weights and behaviour. It must be called exactly once, at startup, before any
// inference runs (the server composition root does this). After it returns the
// seams are serving-immutable: the backend is read concurrently, so mutating
// these after first use would be a data race.
func InitCPUEE() {
	modelFileDet = "det.ee.cpu.ort"
	modelFileLayout = "layout.ee.cpu.ort"
	modelFileRec = "rec.ee.cpu.ort"
	detBoxThresh = 0.6
	dlaPostprocessFn = dlaPostprocessEE
	// EE ships its own weights (different from the OSS *.ort by design); the
	// TSR model is served under the EE 2-class name (RunTSREE loads it
	// directly, so modelFileTsr is left at the OSS default).
	common.DeepDocModelFiles = []string{
		"det.ee.cpu.ort",
		"layout.ee.cpu.ort",
		"tsr.ee.cpu.ort",
		"rec.ee.cpu.ort",
		"ocr.res",
	}
}
