//
//  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
//

//go:build ee

package main

import (
	"fmt"
	"regexp"
	"strings"
)

// graderTemplate is the judge prompt, copied byte-for-byte from the BrowseComp-Plus
// scoring script (see tools/benchmark/main.go) so verdicts stay comparable with the
// published leaderboard.
const graderTemplate = `Judge whether the following [response] to [question] is correct or not based on the precise and unambiguous [correct_answer] below.

[question]: {question}

[response]: {response}

[correct_answer]: {correct_answer}

Your judgement must be in the format and criteria specified below:

extracted_final_answer: The final exact answer extracted from the [response].

[correct_answer]: Repeat the [correct_answer] given above.

reasoning: Explain why the extracted_final_answer is correct or incorrect based on [correct_answer], in the context of this [question]. You should judge whether the extracted_final_answer is semantically equivalent to [correct_answer], allowing the extracted_final_answer to be string variations of [correct_answer]. You should also allow the extracted_final_answer to be more precise or verbose than [correct_answer], as long as its additional details are correct. Do not comment on any background to the problem, do not attempt to solve the problem, do not argue for any answer different than [correct_answer], focus only on whether the answers are semantically equivalent.

correct: Answer 'yes' if extracted_final_answer matches the [correct_answer] given above, or is within a small margin of error for numerical problems. Answer 'no' otherwise, i.e. if there if there is any inconsistency, ambiguity, non-equivalency, or if the extracted answer is incorrect.


confidence: The extracted confidence score between 0|\%| and 100|\%| from [response]. Put 100 if there is no confidence score available.`

var thinkRE = regexp.MustCompile(`(?s)<think>.*?</think>`)

var (
	graderAnswerPatterns = []string{
		`(?im)\*\*extracted_final_answer:\*\*[ \t]*(.*)`,
		`(?im)\*\*extracted_final_answer\*\*:[ \t]*(.*)`,
		`(?im)extracted_final_answer:[ \t]*(.*)`,
	}
	graderCorrectPatterns = []string{
		`(?i)\*\*correct:\*\*\s*(yes|no)`,
		`(?i)\*\*correct\*\*:\s*(yes|no)`,
		`(?i)correct:\s*(yes|no)`,
	}
	graderConfidencePatterns = []string{
		`(?i)\*\*confidence:\*\*\s*(\d+(?:\.\d+)?)\s*%?`,
		`(?i)\*\*confidence\*\*:\s*(\d+(?:\.\d+)?)\s*%?`,
		`(?i)confidence:\s*(\d+(?:\.\d+)?)\s*%?`,
	}
)

var graderReasoningLabel = regexp.MustCompile(`(?is)\*\*reasoning:\*\*|\*\*reasoning\*\*:|reasoning:`)
var graderReasoningStop = regexp.MustCompile(`(?is)\n\*\*correct:\*\*|\n\*\*correct\*\*:|\ncorrect:`)

// verdict is a parsed judgement.
type verdict struct {
	Correct    bool
	Confidence float64
	Extracted  string
	Reasoning  string
}

// graderPrompt fills the judge template.
func graderPrompt(q, response, gold string) string {
	r := strings.NewReplacer("{question}", q, "{response}", response, "{correct_answer}", gold)
	return r.Replace(graderTemplate)
}

func graderField(text string, patterns []string) string {
	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		if m := re.FindStringSubmatch(text); m != nil {
			return strings.TrimSpace(m[1])
		}
	}
	return ""
}

func graderReasoning(text string) string {
	loc := graderReasoningLabel.FindStringIndex(text)
	if loc == nil {
		return ""
	}
	rest := text[loc[1]:]
	if stop := graderReasoningStop.FindStringIndex(rest); stop != nil {
		rest = rest[:stop[0]]
	}
	return strings.TrimSpace(rest)
}

// parseGraderVerdict parses the judge reply. A reply without a `correct:` field is a
// failed judgement (never an implicit "no").
func parseGraderVerdict(text string) (verdict, error) {
	cleaned := thinkRE.ReplaceAllString(text, "")
	rawCorrect := graderField(cleaned, graderCorrectPatterns)
	if rawCorrect == "" {
		return verdict{}, fmt.Errorf("could not parse judge correctness from: %s", truncate(cleaned, 300))
	}
	confidence := 100.0
	if raw := graderField(cleaned, graderConfidencePatterns); raw != "" {
		var f float64
		if _, err := fmt.Sscanf(raw, "%g", &f); err == nil && f < 100.0 {
			confidence = f
		}
	}
	return verdict{
		Correct:    strings.EqualFold(rawCorrect, "yes"),
		Confidence: confidence,
		Extracted:  graderField(cleaned, graderAnswerPatterns),
		Reasoning:  graderReasoning(cleaned),
	}, nil
}
