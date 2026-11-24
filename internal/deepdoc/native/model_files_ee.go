//go:build cgo && ee

package native

// model_files_ee.go — centralized EE DeepDoc weight filename construction.
// All EE weights live in one directory (deepdoc_ee_models/) and differ only by
// suffix: *.ee.cpu.ort (EE CPU) vs *.ee.gpu.ort (EE GPU). The suffix is a
// build-time constant supplied by model_files_cpu_ee.go / model_files_gpu_ee.go,
// so a binary can never load the wrong device's weights.

// eeModelFile builds "<base>.ee.<suffix>.ort", e.g. det.ee.gpu.ort.
func eeModelFile(base string) string { return base + ".ee." + eeModelSuffix + ".ort" }

func detModel() string    { return eeModelFile("det") }
func layoutModel() string { return eeModelFile("layout") }
func tsrModel() string    { return eeModelFile("tsr") }
func recModel() string    { return eeModelFile("rec") }
