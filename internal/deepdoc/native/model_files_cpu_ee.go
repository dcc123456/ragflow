//go:build cgo && ee && !cuda

package native

// model_files_cpu_ee.go — EE CPU build: suffix "cpu". Mirrored by
// model_files_gpu_ee.go (cuda) which uses "gpu". Keeping the name in a
// build-tagged file makes the artifact identity a compile-time fact.
const eeModelSuffix = "cpu"
