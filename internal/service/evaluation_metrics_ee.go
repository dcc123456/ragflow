//go:build ee

package service

import (
	"context"
	"embed"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"ragflow/internal/entity"
	kccommon "ragflow/internal/ingestion/component/knowledge_compiler/common"
)

//go:embed prompts/evaluation/*.md
var evaluationPrompts embed.FS

var evaluationMetricNames = []string{"faithfulness", "context_relevance", "semantic_similarity"}

// EvaluationMetricInput is the immutable answer and retrieval snapshot to score.
type EvaluationMetricInput struct {
	Question        string
	Answer          string
	ReferenceAnswer string
	References      entity.JSONMap
	RelevantDocIDs  []string
}

// ComputeEvaluationMetrics computes the source feature's non-model metrics.
func ComputeEvaluationMetrics(input EvaluationMetricInput) entity.JSONMap {
	metrics := entity.JSONMap{}
	if len(input.RelevantDocIDs) > 0 {
		var retrieved []string
		for _, chunk := range evaluationChunks(input.References) {
			if id, ok := getChunkValue(chunk, "document_id", "doc_id").(string); ok && id != "" {
				retrieved = append(retrieved, id)
			}
		}
		for key, value := range computeEvaluationRetrievalMetrics(retrieved, input.RelevantDocIDs) {
			metrics[key] = value
		}
	}
	if input.Answer != "" {
		metrics["answer_length"] = utf8.RuneCountInString(input.Answer)
		metrics["has_answer"] = 0.0
		if strings.TrimSpace(input.Answer) != "" {
			metrics["has_answer"] = 1.0
		}
		if input.ReferenceAnswer != "" {
			metrics["blue_score"] = computeEvaluationBlueScore(input.Answer, input.ReferenceAnswer)
		}
	}
	return metrics
}

func computeEvaluationBlueScore(candidate, reference string) float64 {
	tokens := func(text string) []string {
		return strings.Fields(strings.Map(func(r rune) rune {
			if unicode.IsLetter(r) || unicode.IsNumber(r) {
				return unicode.ToLower(r)
			}
			return ' '
		}, text))
	}
	candidateTokens, referenceTokens := tokens(candidate), tokens(reference)
	if len(candidateTokens) == 0 || len(referenceTokens) == 0 {
		return 0
	}
	counts := make(map[string]int)
	for _, token := range referenceTokens {
		counts[token]++
	}
	overlap := 0
	for _, token := range candidateTokens {
		if counts[token] > 0 {
			overlap++
			counts[token]--
		}
	}
	penalty := 1.0
	if len(candidateTokens) <= len(referenceTokens) {
		penalty = math.Exp(1 - float64(len(referenceTokens))/float64(len(candidateTokens)))
	}
	return penalty * float64(overlap) / float64(len(candidateTokens))
}

func computeEvaluationRetrievalMetrics(retrieved, relevant []string) entity.JSONMap {
	retrievedSet, relevantSet := make(map[string]bool), make(map[string]bool)
	for _, id := range retrieved {
		retrievedSet[id] = true
	}
	for _, id := range relevant {
		relevantSet[id] = true
	}
	hits := 0
	for id := range retrievedSet {
		if relevantSet[id] {
			hits++
		}
	}
	precision, recall, f1, hit, mrr := 0.0, 0.0, 0.0, 0.0, 0.0
	if len(retrievedSet) > 0 {
		precision = float64(hits) / float64(len(retrievedSet))
	}
	if len(relevantSet) > 0 {
		recall = float64(hits) / float64(len(relevantSet))
	}
	if precision+recall > 0 {
		f1 = 2 * precision * recall / (precision + recall)
		hit = 1
	}
	for i, id := range retrieved {
		if relevantSet[id] {
			mrr = 1 / float64(i+1)
			break
		}
	}
	return entity.JSONMap{"precision": precision, "recall": recall, "f1_score": f1, "hit_rate": hit, "mrr": mrr}
}

func evaluationChunks(references entity.JSONMap) []map[string]any {
	var chunks []map[string]any
	switch values := references["chunks"].(type) {
	case []any:
		for _, value := range values {
			switch chunk := value.(type) {
			case map[string]any:
				chunks = append(chunks, chunk)
			case entity.JSONMap:
				chunks = append(chunks, chunk)
			}
		}
	case []map[string]any:
		return values
	}
	return chunks
}

func truncateEvaluationText(text string, limit int) string {
	runes := []rune(strings.TrimSpace(text))
	return string(runes[:min(len(runes), limit)])
}

var (
	evaluationLeftSpaces  = regexp.MustCompile(`(?i)([^a-z0-9.,)>]) +([^ ])`)
	evaluationRightSpaces = regexp.MustCompile(`(?i)([^ ]) +([^a-z0-9.,(<])`)
)

func buildEvaluationContext(references entity.JSONMap) string {
	var parts []string
	chunks := evaluationChunks(references)
	for _, chunk := range chunks[:min(len(chunks), 6)] {
		content, _ := chunk["content"].(string)
		if content == "" {
			content, _ = chunk["content_with_weight"].(string)
		}
		if content == "" {
			content, _ = chunk["content_ltks"].(string)
			content = evaluationLeftSpaces.ReplaceAllString(content, "${1}${2}")
			content = evaluationRightSpaces.ReplaceAllString(content, "${1}${2}")
		}
		if content = truncateEvaluationText(content, 1024); content != "" {
			parts = append(parts, content)
		}
	}
	return strings.Join(parts, "\n\n")
}

// JudgeEvaluationMetrics resolves the tenant model through the shared model factory.
// An empty metric list selects the three source defaults.
func JudgeEvaluationMetrics(ctx context.Context, factory *ModelFactory, tenantID, modelRef string, input EvaluationMetricInput, names []string) (entity.JSONMap, error) {
	if factory == nil {
		return nil, fmt.Errorf("evaluation: model factory not initialized")
	}
	return judgeEvaluationMetrics(ctx, navChatInvoker{modelFactory: factory, tenantID: tenantID, modelRef: modelRef}, input, names)
}

// EvaluateEvaluationMetrics scores the selected enabled metrics in a saved run.
// Existing unselected scores remain the runner's responsibility.
func EvaluateEvaluationMetrics(ctx context.Context, tenantID string, config entity.JSONMap, c *entity.EvaluationCase, result *entity.EvaluationResult, names []string) (entity.JSONMap, error) {
	input := EvaluationMetricInput{Answer: result.GeneratedAnswer, References: result.RetrievedChunks}
	input.Question, _ = c.Variable["question"].(string)
	input.ReferenceAnswer, _ = c.Variable["reference_answer"].(string)
	for _, value := range c.RelevantDocIDs {
		if id, ok := value.(string); ok {
			input.RelevantDocIDs = append(input.RelevantDocIDs, id)
		}
	}
	if len(names) == 0 {
		names = evaluationMetricNames
	}
	metrics := entity.JSONMap{}
	seen := make(map[string]bool)
	for _, name := range names {
		switch name {
		case "faithfulness", "context_relevance", "semantic_similarity":
		default:
			return nil, fmt.Errorf("evaluation: unknown metric %q", name)
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		var setting map[string]any
		switch value := config[name].(type) {
		case map[string]any:
			setting = value
		case entity.JSONMap:
			setting = value
		}
		if setting == nil || setting["enable"] == false {
			continue
		}
		for key, value := range ComputeEvaluationMetrics(input) {
			metrics[key] = value
		}
		modelRef, _ := setting["llm_id"].(string)
		scores, err := JudgeEvaluationMetrics(ctx, NewModelFactory(), tenantID, modelRef, input, []string{name})
		if err != nil {
			return metrics, err
		}
		for key, value := range scores {
			metrics[key] = value
		}
	}
	return metrics, nil
}

func judgeEvaluationMetrics(ctx context.Context, chat kccommon.ChatInvoker, input EvaluationMetricInput, names []string) (entity.JSONMap, error) {
	if len(names) == 0 {
		names = evaluationMetricNames
	}
	selected := make(map[string]bool)
	for _, name := range names {
		switch name {
		case "faithfulness", "context_relevance", "semantic_similarity":
			selected[name] = true
		default:
			return nil, fmt.Errorf("evaluation: unknown metric %q", name)
		}
	}
	metrics := entity.JSONMap{}
	if input.Question == "" || input.Answer == "" {
		return metrics, nil
	}
	contextText, reference := buildEvaluationContext(input.References), truncateEvaluationText(input.ReferenceAnswer, 512)
	if contextText == "" {
		contextText = "None"
	}
	if reference == "" {
		reference = "None"
	}
	replacer := strings.NewReplacer(
		"{{ question }}", truncateEvaluationText(input.Question, 256),
		"{{ answer }}", truncateEvaluationText(input.Answer, 512),
		"{{ context }}", contextText, "{{ reference_answer }}", reference,
	)
	for _, name := range evaluationMetricNames {
		if !selected[name] {
			continue
		}
		system, err := evaluationPrompts.ReadFile("prompts/evaluation/" + name + "_system.md")
		if err != nil {
			return metrics, err
		}
		user, err := evaluationPrompts.ReadFile("prompts/evaluation/" + name + "_user.md")
		if err != nil {
			return metrics, err
		}
		reply, err := kccommon.GenJSON(ctx, chat, kccommon.ChatRequest{
			SystemPrompt: string(system), UserPrompt: replacer.Replace(string(user)),
		}, 1)
		if err != nil {
			return metrics, fmt.Errorf("evaluation %s judge: %w", name, err)
		}
		score, ok := evaluationNumber(reply["score"])
		if !ok {
			if text, isString := reply["score"].(string); isString {
				score, err = strconv.ParseFloat(text, 64)
				ok = err == nil && !math.IsNaN(score) && !math.IsInf(score, 0)
			}
		}
		if !ok {
			return metrics, fmt.Errorf("evaluation %s judge: invalid score", name)
		}
		reason, ok := reply["reason"].(string)
		if !ok && reply["reason"] != nil {
			return metrics, fmt.Errorf("evaluation %s judge: invalid reason", name)
		}
		metrics[name] = min(1.0, max(0.0, score))
		metrics[name+"_reason"] = reason
	}
	return metrics, nil
}

func evaluationNumber(value any) (float64, bool) {
	if value == nil {
		return 0, false
	}
	v := reflect.ValueOf(value)
	var number float64
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		number = float64(v.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		number = float64(v.Uint())
	case reflect.Float32, reflect.Float64:
		number = v.Float()
	default:
		return 0, false
	}
	return number, !math.IsNaN(number) && !math.IsInf(number, 0)
}

// ComputeEvaluationSummary averages each available numeric metric independently.
func ComputeEvaluationSummary(results []entity.EvaluationResult) entity.JSONMap {
	sums, counts := make(map[string]float64), make(map[string]int)
	add := func(key string, value any) {
		if number, ok := evaluationNumber(value); ok {
			sums[key] += number
			counts[key]++
		}
	}
	for _, result := range results {
		for key, value := range result.Metrics {
			add(key, value)
		}
		if result.ExecutionTime >= 0 {
			add("execution_time", result.ExecutionTime)
		}
	}
	summary := entity.JSONMap{}
	for key, sum := range sums {
		summary[key] = entity.JSONMap{"type": "", "config": "", "summary": sum / float64(counts[key])}
	}
	return summary
}
