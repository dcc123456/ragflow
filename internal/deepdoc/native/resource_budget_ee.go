//go:build cgo && ee

package native

// resource_budget_ee.go — GPU (VRAM) ResourceBudget implementation and the
// GPU activation helpers. The shared machinery lives in resource_budget.go.

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"go.uber.org/zap"
	"ragflow/internal/common"
)

type gpuResource struct{}

func (gpuResource) Name() string { return "gpu-vram" }

func (gpuResource) TotalMB() int { return nvmlQuery("memory.total") }
func (gpuResource) UsedMB() int  { return nvmlQuery("memory.used") }

// nvmlQuery runs a nvidia-smi query for a single numeric value (MiB).
func nvmlQuery(field string) int {
	out, err := exec.Command("nvidia-smi", "--query-gpu="+field,
		"--format=csv,noheader,nounits").Output()
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return 0
	}
	mb, err := strconv.Atoi(strings.TrimSpace(fields[0]))
	if err != nil || mb <= 0 {
		return 0
	}
	return mb
}

// InitGPUVramLedger installs the adaptive budget for the GPU. GPU defaults ON;
// RAGFLOW_DEEPDOC_RESOURCE_BUDGET=0 opts out. Retained name for call-site
// compatibility; it delegates to the shared InitResourceBudget.
func InitGPUVramLedger(ctx context.Context) {
	InitResourceBudget(ctx, gpuResource{}, true)
}

const (
	envAdaptiveKMode      = "RAGFLOW_DEEPDOC_K_MODE"
	envAdaptiveKPerRunMB  = "RAGFLOW_DEEPDOC_K_VRAM_PER_RUN_MB"
	envAdaptiveKWeightsMB = "RAGFLOW_DEEPDOC_K_WEIGHTS_MB"
	envAdaptiveKMax       = "RAGFLOW_DEEPDOC_K_MAX"

	defPerRunMB = 560
	defKMax     = 64
	defKMin     = 2
)

// AdaptiveInferenceConcurrency derives GPU K from detected VRAM and model
// weights. Returns 0 to keep the configured K when detection fails or an
// explicit non-auto mode is set.
func AdaptiveInferenceConcurrency(budgetRatio float64) int {
	mode := strings.TrimSpace(os.Getenv(envAdaptiveKMode))
	if mode != "" && mode != "auto" {
		return 0
	}
	g := gpuResource{}
	totalMB := g.TotalMB()
	if totalMB <= 0 {
		return 0
	}
	weightsMB := detectedWeightsMB([]string{layoutModel(), tsrModel(), detModel(), recModel()})
	if w := envInt(envAdaptiveKWeightsMB, 0); w > 0 {
		weightsMB = w
	}
	perRunMB := envInt(envAdaptiveKPerRunMB, defPerRunMB)
	if perRunMB < 64 {
		perRunMB = defPerRunMB
	}
	kMax := envInt(envAdaptiveKMax, defKMax)
	if kMax < defKMin {
		kMax = defKMin
	}
	k := computeAdaptiveK(totalMB, weightsMB, budgetRatio, perRunMB, defKMin, kMax)
	common.Info("GPU adaptive inference concurrency derived",
		zap.Int("total_mb", totalMB), zap.Int("weights_mb", weightsMB),
		zap.Int("per_run_mb", perRunMB), zap.Int("k", k))
	return k
}
