//go:build cgo && ee && cuda

package native

// shared_weights_gpu_ee.go — EE GPU implementation of the shared-weights
// extraction seam. On GPU we must extract initializers from a CUDA session so
// that GetInitializerOnDevice can hand back CUDA-memory OrtValues; injecting
// those into every CUDA session via AddInitializer makes ORT's device check
// accept them and share ONE GPU weight buffer across all sessions (zero-copy),
// instead of each session re-uploading its own copy (which exhausted 12GB VRAM).

import (
	"fmt"
	"sync"

	ort "github.com/infiniflow/onnxruntime_go"
)

var (
	gpuWeightMu    sync.Mutex
	gpuWeightCache = map[string]*weightSet{} // key: deviceID + "\x00" + modelPath
	// The extracted CUDA OrtValues point into buffers owned by the source
	// session's device allocator. The source session MUST stay alive for the
	// lifetime of those buffers (process-wide), otherwise its destruction
	// invalidates the GPU memory and inference hits illegal memory access.
	gpuSourceSessions = map[string]*ort.AdvancedSession{}
)

// sharedGPUWeights builds (or returns cached) CUDA-memory shared initializers
// for modelPath. It is installed as the sharedWeightsSeam hook by init() below.
// Returns (nil, nil) when the process is in CPU mode, so the default CPU path
// in sharedWeights() is unchanged.
func sharedGPUWeights(modelPath, inName string, inShape []int64, outName string) (*weightSet, error) {
	dev := getDevice()
	if !dev.UseGPU {
		return nil, nil // CPU mode: fall through to the default extraction path
	}

	cacheKey := fmt.Sprintf("%d\x00%s", dev.DeviceID, modelPath)
	gpuWeightMu.Lock()
	defer gpuWeightMu.Unlock()
	if ws, ok := gpuWeightCache[cacheKey]; ok {
		return ws, nil
	}

	// CUDA source session: same EP options a running session gets, so the
	// initializers are planned on the CUDA device and the new API can extract
	// device-resident OrtValues.
	opts, err := ort.NewSessionOptions()
	if err != nil {
		return nil, fmt.Errorf("gpu shared weights: session options: %w", err)
	}
	defer opts.Destroy()
	if err := applyDeviceHook(opts); err != nil {
		return nil, fmt.Errorf("gpu shared weights: cuda options: %w", err)
	}
	inT, err := ort.NewTensor(ort.NewShape(inShape...), make([]float32, prod(inShape)))
	if err != nil {
		return nil, fmt.Errorf("gpu shared weights: extraction input: %w", err)
	}
	defer inT.Destroy()
	outT, err := ort.NewTensor(ort.NewShape(1), []float32{0})
	if err != nil {
		return nil, fmt.Errorf("gpu shared weights: extraction output: %w", err)
	}
	defer outT.Destroy()
	ext, err := ort.NewAdvancedSession(modelPath,
		[]string{inName}, []string{outName},
		[]ort.Value{inT}, []ort.Value{outT}, opts)
	if err != nil {
		return nil, fmt.Errorf("gpu shared weights: open CUDA extraction session: %w", err)
	}
	// Do NOT Destroy ext: it owns the CUDA allocator that backs the extracted
	// device buffers. Keep it alive process-wide alongside the weight set.
	gpuSourceSessions[cacheKey] = ext

	count, err := ext.GetInitializerCount()
	if err != nil {
		return nil, fmt.Errorf("gpu shared weights: initializer count: %w", err)
	}
	ws := &weightSet{
		names: make([]string, 0, count),
		vals:  make([]*ort.SharedInitializer, 0, count),
	}
	for i := 0; i < count; i++ {
		name, err := ext.GetInitializerName(i)
		if err != nil {
			return nil, fmt.Errorf("gpu shared weights: initializer name %d: %w", i, err)
		}
		val, err := ext.GetInitializerOnDevice(name, dev.DeviceID)
		if err != nil {
			// Weight planned on CPU (e.g. small constants) — leave it to ORT's
			// normal per-session handling, as the C POC showed is correct.
			continue
		}
		ws.names = append(ws.names, name)
		ws.vals = append(ws.vals, val)
	}
	gpuWeightCache[cacheKey] = ws
	return ws, nil
}

func init() {
	sharedWeightsSeam = sharedGPUWeights
}
