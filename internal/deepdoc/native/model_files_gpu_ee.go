//go:build cgo && ee && cuda

package native

// model_files_gpu_ee.go — EE GPU build: the deployment model-file suffix is
// "gpu". The CUDA EP requires de-fused weights (Conv+HardSigmoid separate);
// naming them *.ee.gpu.ort keeps them distinct from the CPU *.ee.cpu.ort set
// in the shared staging directory (deepdoc_ee_models/).
const eeModelSuffix = "gpu"
