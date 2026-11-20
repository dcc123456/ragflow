//
//  Copyright (c) 2026 InfiniFlow. All rights reserved.
//
//  RAGFlow Enterprise Edition — Proprietary and Confidential.
//
//  This file is part of the RAGFlow Enterprise Edition and is licensed,
//  not sold, under the RAGFlow Enterprise Commercial License Agreement.
//  Unauthorized copying, modification, distribution, or disclosure of this
//  file, via any medium, is strictly prohibited.
//
//  Portions of this file may incorporate code from RAGFlow Community Edition,
//  which is licensed under the Apache License, Version 2.0.
//
//  For commercial licensing inquiries: sales@infiniflow.ai
//

//go:build ee

package component

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/kaptinlin/jsonrepair"

	"ragflow/internal/common"
	"ragflow/internal/tokenizer"
)

const (
	// resumeSummaryMaxTokens bounds the structured header prepended to
	// embedding_text so short-window embedding models (e.g. 512 tokens) retain
	// high-signal role/project/skill context without losing everything to verbose prose.
	resumeSummaryMaxTokens = 250
	// resumeMaxImportantKeywords caps the high-weight important_kwd entries
	// so important_kwd^30 boosts precision without drowning full-text content_ltks^2.
	resumeMaxImportantKeywords = 32
	// resumeOverlapJaccardThreshold marks a project description as overlapping
	// with a work description so keywords/summary do not repeat the same prose.
	resumeOverlapJaccardThreshold = 0.65
)

var (
	resumeLongNoiseRE = regexp.MustCompile(`^[A-Za-z0-9\-~_]{40,}$`)
	resumeDateYMRE    = regexp.MustCompile(`(?i)^(\d{4})\s*[.\-/年]\s*(\d{1,2})`)
	resumeDateYearRE  = regexp.MustCompile(`^(\d{4})(?:年)?$`)
	resumeDateMonYRRE = regexp.MustCompile(`(?i)^(jan|january|feb|february|mar|march|apr|april|may|jun|june|jul|july|aug|august|sep|sept|september|oct|october|nov|november|dec|december)\.?\s+(\d{4})$`)
	resumeDateMDYRE   = regexp.MustCompile(`^(\d{1,2})\s*[./\-]\s*(\d{4})$`)
	resumeSplitItemRE = regexp.MustCompile(`[、,，;；|]+`)
)

// flexStringSlice accepts a JSON array of strings, a single string (split on
// common list separators), or null.
type flexStringSlice []string

func (f *flexStringSlice) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		*f = nil
		return nil
	}
	var arr []string
	if err := json.Unmarshal(data, &arr); err == nil {
		*f = arr
		return nil
	}
	var rawArr []any
	if err := json.Unmarshal(data, &rawArr); err == nil {
		out := make([]string, 0, len(rawArr))
		for _, item := range rawArr {
			if s := strings.TrimSpace(fmt.Sprint(item)); s != "" && item != nil {
				out = append(out, s)
			}
		}
		*f = out
		return nil
	}
	var single string
	if err := json.Unmarshal(data, &single); err == nil {
		single = strings.TrimSpace(single)
		if single == "" {
			*f = nil
			return nil
		}
		parts := resumeSplitItemRE.Split(single, -1)
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
		*f = out
		return nil
	}
	return nil
}

// flexBoolInt accepts JSON bool, number (1/0), or string ("1"/"true").
type flexBoolInt bool

func (b *flexBoolInt) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		*b = false
		return nil
	}
	var bv bool
	if err := json.Unmarshal(data, &bv); err == nil {
		*b = flexBoolInt(bv)
		return nil
	}
	var fv float64
	if err := json.Unmarshal(data, &fv); err == nil {
		*b = flexBoolInt(fv != 0)
		return nil
	}
	var sv string
	if err := json.Unmarshal(data, &sv); err == nil {
		s := strings.ToLower(strings.TrimSpace(sv))
		*b = flexBoolInt(s == "1" || s == "true" || s == "yes" || s == "是")
		return nil
	}
	return nil
}

// flexIntSlice accepts JSON arrays of numbers or numeric strings (e.g. [23, 24]).
type flexIntSlice []int

func (f *flexIntSlice) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		*f = nil
		return nil
	}
	var arr []int
	if err := json.Unmarshal(data, &arr); err == nil {
		*f = arr
		return nil
	}
	var raw []any
	if err := json.Unmarshal(data, &raw); err == nil {
		out := make([]int, 0, len(raw))
		for _, item := range raw {
			switch v := item.(type) {
			case float64:
				out = append(out, int(v))
			case string:
				if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
					out = append(out, n)
				}
			}
		}
		*f = out
		return nil
	}
	return nil
}

type resumeBasicRaw struct {
	Name            string          `json:"name_kwd"`
	CurrentLocation string          `json:"current_location"`
	ExpectCities    flexStringSlice `json:"expect_city_names_tks"`
	ExpectPositions flexStringSlice `json:"expect_position_name_tks"`
	Skills          flexStringSlice `json:"skill_tks"`
	Languages       flexStringSlice `json:"language_tks"`
	Certificates    flexStringSlice `json:"certificate_tks"`
}

type resumeWorkItemRaw struct {
	Company    string       `json:"company"`
	Position   string       `json:"position"`
	Internship flexBoolInt  `json:"internship"`
	StartDate  string       `json:"start_date"`
	EndDate    string       `json:"end_date"`
	DescLines  flexIntSlice `json:"desc_lines"`
}

type resumeWorkRaw struct {
	WorkExperience []resumeWorkItemRaw `json:"workExperience"`
}

type resumeEducationItemRaw struct {
	School     string       `json:"school"`
	Major      string       `json:"major"`
	Degree     string       `json:"degree"`
	Department string       `json:"department"`
	StartDate  string       `json:"start_date"`
	EndDate    string       `json:"end_date"`
	DescLines  flexIntSlice `json:"desc_lines"`
}

type resumeEducationRaw struct {
	Education []resumeEducationItemRaw `json:"education"`
}

type resumeProjectItemRaw struct {
	ProjectName string       `json:"project_name"`
	Role        string       `json:"role"`
	StartDate   string       `json:"start_date"`
	EndDate     string       `json:"end_date"`
	DescLines   flexIntSlice `json:"desc_lines"`
}

type resumeProjectRaw struct {
	ProjectExperience []resumeProjectItemRaw `json:"projectExperience"`
}

// resumeWorkEntry is a verified work experience record after source-text grounding.
type resumeWorkEntry struct {
	Company     string
	Position    string
	Internship  bool
	StartDate   string
	EndDate     string
	Description string
}

// resumeEducationEntry is a verified education record after source-text grounding.
type resumeEducationEntry struct {
	School      string
	Major       string
	Degree      string
	DegreeLevel int
	Department  string
	StartDate   string
	EndDate     string
	Description string
}

// resumeProjectEntry is a verified project record after source-text grounding.
type resumeProjectEntry struct {
	ProjectName string
	Role        string
	StartDate   string
	EndDate     string
	Description string
}

// resumeLaneStatus captures per-lane extraction diagnostics for logging and progress reporting.
type resumeLaneStatus struct {
	Lane        resumeLane
	Outcome     string // "ok", "empty", "parse_error", "call_error", "out_of_bounds"
	OutOfBounds int
	Err         string
}

// resumeProfile is the deterministic Go-merged structured view of a single resume.
// Decoupling resumeProfile from chunk rendering lets V1 emit one enriched chunk
// while keeping the door open for V2 parent-child semantic chunk rendering.
type resumeProfile struct {
	IsEnglish           bool
	Name                string
	CurrentLocation     string
	ExpectedCities      []string
	ExpectedPositions   []string
	Skills              []string
	Languages           []string
	Certificates        []string
	HighestDegree       string
	HighestDegreeLevel  int
	WorkExperienceYears *float64
	Companies           []string
	Positions           []string
	Schools             []string
	Majors              []string
	WorkEntries         []resumeWorkEntry
	EducationEntries    []resumeEducationEntry
	ProjectEntries      []resumeProjectEntry
	LaneStatuses        []resumeLaneStatus
}

// buildResumeIndexedText normalizes line breaks, drops empty/random-hash noise
// lines, and returns both the line-indexed prompt text (`[0]: ...`) and the
// backing line slice used to recover verified source snippets.
func buildResumeIndexedText(raw string) (string, []string) {
	normalized := strings.ReplaceAll(raw, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	rawLines := strings.Split(normalized, "\n")

	lines := make([]string, 0, len(rawLines))
	var b strings.Builder
	for _, line := range rawLines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if isResumeNoiseLine(line) {
			continue
		}
		idx := len(lines)
		lines = append(lines, line)
		if idx > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "[%d]: %s", idx, line)
	}
	return b.String(), lines
}

func isResumeNoiseLine(line string) bool {
	if strings.ContainsAny(line, " \t@:/") {
		return false
	}
	return resumeLongNoiseRE.MatchString(line)
}

// decodeResumeLaneJSON cleans thinking tags and markdown fences, attempts strict
// JSON decode, and falls back to jsonrepair for malformed model output.
func decodeResumeLaneJSON[T any](raw string) (*T, bool) {
	s := cleanLLMText(raw)
	s = common.StripThinkTrailing(s)
	s = stripResumeCodeFence(s)
	if s == "" {
		return nil, false
	}
	var out T
	if err := json.Unmarshal([]byte(s), &out); err == nil {
		return &out, true
	}
	repaired, err := jsonrepair.Repair(s)
	if err != nil {
		return nil, false
	}
	repaired = strings.TrimSpace(repaired)
	if repaired == "" {
		return nil, false
	}
	var outRepaired T
	if err := json.Unmarshal([]byte(repaired), &outRepaired); err != nil {
		return nil, false
	}
	return &outRepaired, true
}

// decodeResumeLanePresence returns the set of top-level keys a lane reply
// explicitly carries. An explicit empty value is the model confirming
// absence; a missing key — or a JSON null, which the contract never asks
// for — is the model saying nothing.
func decodeResumeLanePresence(raw string) map[string]struct{} {
	m, ok := decodeResumeLaneJSON[map[string]json.RawMessage](raw)
	if !ok || m == nil || *m == nil {
		return nil
	}
	presence := make(map[string]struct{}, len(*m))
	for k, v := range *m {
		if strings.EqualFold(strings.TrimSpace(string(v)), "null") {
			continue
		}
		presence[k] = struct{}{}
	}
	return presence
}

// laneContractPresent reports whether a lane reply explicitly carries at
// least one of the lane's contract keys.
func laneContractPresent(lane resumeLane, presence map[string]struct{}) bool {
	for _, k := range resumeLaneContractKeys[lane] {
		if _, ok := presence[k]; ok {
			return true
		}
	}
	return false
}

func stripResumeCodeFence(s string) string {
	trimmed := strings.TrimSpace(s)
	if strings.HasPrefix(trimmed, "```") {
		if idx := strings.IndexByte(trimmed, '\n'); idx >= 0 {
			trimmed = trimmed[idx+1:]
		} else {
			trimmed = trimmed[3:]
		}
		if firstNL := strings.IndexByte(trimmed, '\n'); firstNL >= 0 {
			if firstLine := strings.TrimSpace(trimmed[:firstNL]); firstLine == "json" || firstLine == "JSON" {
				trimmed = trimmed[firstNL+1:]
			}
		}
		if i := strings.LastIndex(trimmed, "```"); i >= 0 {
			trimmed = trimmed[:i]
		}
		return strings.TrimSpace(trimmed)
	}
	// If the model prefixed prose before the opening brace, slice from first '{' to last '}'.
	start := strings.IndexByte(trimmed, '{')
	end := strings.LastIndexByte(trimmed, '}')
	if start >= 0 && end > start {
		return strings.TrimSpace(trimmed[start : end+1])
	}
	return trimmed
}

// extractResumeLines recovers source text from validated 0-based line indices.
// Out-of-bounds or inverted ranges are rejected and reported instead of clamped
// or fabricated.
func extractResumeLines(descLines []int, lines []string) (string, bool) {
	if len(descLines) == 0 {
		return "", false
	}
	n := len(lines)
	if n == 0 {
		return "", true
	}
	switch len(descLines) {
	case 1:
		idx := descLines[0]
		if idx < 0 || idx >= n {
			return "", true
		}
		return lines[idx], false
	case 2:
		start, end := descLines[0], descLines[1]
		if start < 0 || end < 0 || start >= n || end >= n || start > end {
			return "", true
		}
		return strings.Join(lines[start:end+1], "\n"), false
	default:
		seen := make(map[int]struct{}, len(descLines))
		parts := make([]string, 0, len(descLines))
		hadOOB := false
		for _, idx := range descLines {
			if idx < 0 || idx >= n {
				hadOOB = true
				continue
			}
			if _, ok := seen[idx]; ok {
				continue
			}
			seen[idx] = struct{}{}
			parts = append(parts, lines[idx])
		}
		return strings.Join(parts, "\n"), hadOOB
	}
}

func normalizeForGrounding(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func groundedInSource(normSource, value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	normVal := normalizeForGrounding(value)
	if normVal == "" {
		return false
	}
	return strings.Contains(normSource, normVal)
}

func dedupePreserveVerbatim(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		key := strings.ToLower(item)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

type resumeLaneOutputs struct {
	basic   *resumeBasicRaw
	work    *resumeWorkRaw
	edu     *resumeEducationRaw
	project *resumeProjectRaw
	status  []resumeLaneStatus
	// present carries, per lane, the top-level keys its reply explicitly
	// held; only a lane that ran on the untruncated resume gets an entry.
	present map[resumeLane]map[string]struct{}
}

// resumeLaneMetadataKeys maps the aggregate-producing lanes to the document
// metadata keys only that lane can produce (the basic lane's per-field keys
// live in resumeBasicManagedKeySources; the project lane produces none). A
// key is cleared on re-parse only when the lane's reply explicitly carried
// its contract and ran on the untruncated resume — see
// confirmedManagedMetadataKeys — so a timed-out, unparsable, or trimmed lane
// never erases the value a previous run stored.
var resumeLaneMetadataKeys = map[string][]string{
	"work_exp":  {"work_experience_years", "companies", "positions"},
	"education": {"highest_degree", "highest_degree_level", "schools", "majors"},
}

// resumeBasicManagedKeySources maps the basic lane's contract fields to the
// document metadata keys they confirm, in a fixed order. A field the reply
// carries — including an explicit empty list — confirms its key; a reply
// that omits the field says nothing about it and the stored value is left
// alone.
var resumeBasicManagedKeySources = []struct{ src, meta string }{
	{"name_kwd", "name"},
	{"current_location", "current_location"},
	{"expect_city_names_tks", "expected_cities"},
	{"expect_position_name_tks", "expected_positions"},
	{"skill_tks", "skills"},
	{"language_tks", "languages"},
	{"certificate_tks", "certificates"},
}

// basicResumeContractKeys derives the basic lane's reply contract from
// resumeBasicManagedKeySources so the contract and the managed-key mapping
// stay in one place: a field joins or leaves both together.
func basicResumeContractKeys() []string {
	keys := make([]string, 0, len(resumeBasicManagedKeySources))
	for _, src := range resumeBasicManagedKeySources {
		keys = append(keys, src.src)
	}
	return keys
}

// mergeResumeLanes deterministically merges and validates the four decoded lane
// payloads against the original resume lines.
func mergeResumeLanes(rawText string, lines []string, isEnglish bool, now time.Time, lanes resumeLaneOutputs) resumeProfile {
	profile := resumeProfile{
		IsEnglish:    isEnglish,
		LaneStatuses: append([]resumeLaneStatus(nil), lanes.status...),
	}
	normSource := normalizeForGrounding(rawText)

	// 1. Basic info
	if lanes.basic != nil {
		b := lanes.basic
		if name := strings.TrimSpace(b.Name); name != "" &&
			!strings.EqualFold(name, "unknown") && name != "未知" &&
			groundedInSource(normSource, name) {
			profile.Name = name
		}
		if loc := strings.TrimSpace(b.CurrentLocation); loc != "" && groundedInSource(normSource, loc) {
			profile.CurrentLocation = loc
		}
		profile.ExpectedCities = dedupePreserveVerbatim(b.ExpectCities)
		profile.ExpectedPositions = dedupePreserveVerbatim(b.ExpectPositions)
		profile.Skills = dedupePreserveVerbatim(b.Skills)
		profile.Languages = dedupePreserveVerbatim(b.Languages)
		profile.Certificates = dedupePreserveVerbatim(b.Certificates)
	}

	// 2. Work experience
	if lanes.work != nil {
		var companies, positions []string
		var verifiedForTenure []resumeWorkItemRaw
		tenureUncertain := false
		oobCount := 0
		for _, item := range lanes.work.WorkExperience {
			company := strings.TrimSpace(item.Company)
			if company != "" && !groundedInSource(normSource, company) {
				company = ""
			}
			position := strings.TrimSpace(item.Position)
			if position != "" && !groundedInSource(normSource, position) {
				position = ""
			}
			desc, oob := extractResumeLines(item.DescLines, lines)
			if oob {
				oobCount++
			}
			if company == "" && position == "" && desc == "" {
				continue
			}
			if company != "" {
				companies = append(companies, company)
			}
			if position != "" {
				positions = append(positions, position)
			}
			// Tenure is a hard-filter fact: a date the model returned is accepted only
			// when the resume text itself carries it, otherwise the whole value is
			// unknown rather than computed from a possibly misread date.
			if (company != "" || position != "") && !bool(item.Internship) && strings.TrimSpace(item.StartDate) != "" {
				endDate := strings.TrimSpace(item.EndDate)
				if !resumeDateGrounded(rawText, item.StartDate) || (endDate != "" && !resumeDateGrounded(rawText, endDate)) {
					tenureUncertain = true
				} else {
					verifiedForTenure = append(verifiedForTenure, item)
				}
			}
			profile.WorkEntries = append(profile.WorkEntries, resumeWorkEntry{
				Company:     company,
				Position:    position,
				Internship:  bool(item.Internship),
				StartDate:   strings.TrimSpace(item.StartDate),
				EndDate:     strings.TrimSpace(item.EndDate),
				Description: strings.TrimSpace(desc),
			})
		}
		profile.Companies = dedupePreserveVerbatim(companies)
		profile.Positions = dedupePreserveVerbatim(positions)
		if !tenureUncertain {
			profile.WorkExperienceYears = calculateWorkExperienceYears(verifiedForTenure, now)
		}
		if oobCount > 0 {
			recordLaneOOB(&profile.LaneStatuses, resumeLaneWork, oobCount)
		}
	}

	// 3. Education
	if lanes.edu != nil {
		var schools, majors []string
		oobCount := 0
		bestLevel := 0
		bestLabel := ""
		for _, item := range lanes.edu.Education {
			school := strings.TrimSpace(item.School)
			if school != "" && !groundedInSource(normSource, school) {
				school = ""
			}
			major := strings.TrimSpace(item.Major)
			if major != "" && !groundedInSource(normSource, major) {
				major = ""
			}
			degreeLabel, degreeLevel := normalizeResumeDegree(item.Degree, isEnglish)
			if degreeLevel > 0 && !resumeDegreeGrounded(rawText, degreeLevel) {
				degreeLabel, degreeLevel = "", 0
			}
			desc, oob := extractResumeLines(item.DescLines, lines)
			if oob {
				oobCount++
			}
			if school == "" && major == "" && degreeLevel == 0 && desc == "" {
				continue
			}
			if school != "" {
				schools = append(schools, school)
			}
			if major != "" {
				majors = append(majors, major)
			}
			if degreeLevel > bestLevel {
				bestLevel = degreeLevel
				bestLabel = degreeLabel
			}
			profile.EducationEntries = append(profile.EducationEntries, resumeEducationEntry{
				School:      school,
				Major:       major,
				Degree:      degreeLabel,
				DegreeLevel: degreeLevel,
				Department:  strings.TrimSpace(item.Department),
				StartDate:   strings.TrimSpace(item.StartDate),
				EndDate:     strings.TrimSpace(item.EndDate),
				Description: strings.TrimSpace(desc),
			})
		}
		profile.Schools = dedupePreserveVerbatim(schools)
		profile.Majors = dedupePreserveVerbatim(majors)
		if bestLevel > 0 {
			profile.HighestDegree = bestLabel
			profile.HighestDegreeLevel = bestLevel
		}
		if oobCount > 0 {
			recordLaneOOB(&profile.LaneStatuses, resumeLaneEducation, oobCount)
		}
	}

	// 4. Project experience
	if lanes.project != nil {
		oobCount := 0
		for _, item := range lanes.project.ProjectExperience {
			projectName := strings.TrimSpace(item.ProjectName)
			if projectName != "" && !groundedInSource(normSource, projectName) {
				projectName = ""
			}
			role := strings.TrimSpace(item.Role)
			if role != "" && !groundedInSource(normSource, role) {
				role = ""
			}
			desc, oob := extractResumeLines(item.DescLines, lines)
			if oob {
				oobCount++
			}
			if projectName == "" && role == "" && desc == "" {
				continue
			}
			profile.ProjectEntries = append(profile.ProjectEntries, resumeProjectEntry{
				ProjectName: projectName,
				Role:        role,
				StartDate:   strings.TrimSpace(item.StartDate),
				EndDate:     strings.TrimSpace(item.EndDate),
				Description: strings.TrimSpace(desc),
			})
		}
		if oobCount > 0 {
			recordLaneOOB(&profile.LaneStatuses, resumeLaneProject, oobCount)
		}
	}

	return profile
}

func recordLaneOOB(statuses *[]resumeLaneStatus, lane resumeLane, count int) {
	for i := range *statuses {
		if (*statuses)[i].Lane == lane {
			(*statuses)[i].OutOfBounds += count
			if (*statuses)[i].Outcome == "ok" {
				(*statuses)[i].Outcome = "out_of_bounds"
			}
			return
		}
	}
	*statuses = append(*statuses, resumeLaneStatus{
		Lane:        lane,
		Outcome:     "out_of_bounds",
		OutOfBounds: count,
	})
}

type parsedYearMonth struct {
	year     int
	month    int // 1..12
	yearOnly bool
}

var englishMonthMap = map[string]int{
	"jan": 1, "january": 1,
	"feb": 2, "february": 2,
	"mar": 3, "march": 3,
	"apr": 4, "april": 4,
	"may": 5,
	"jun": 6, "june": 6,
	"jul": 7, "july": 7,
	"aug": 8, "august": 8,
	"sep": 9, "sept": 9, "september": 9,
	"oct": 10, "october": 10,
	"nov": 11, "november": 11,
	"dec": 12, "december": 12,
}

func isExplicitOngoingDate(s string) bool {
	low := strings.ToLower(strings.TrimSpace(s))
	switch low {
	case "至今", "目前", "现在", "在职", "今", "现今",
		"present", "current", "now", "ongoing", "to date", "till now", "until now":
		return true
	default:
		return false
	}
}

func parseResumeYearMonth(raw string) (parsedYearMonth, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return parsedYearMonth{}, false
	}
	if m := resumeDateYMRE.FindStringSubmatch(s); len(m) == 3 {
		year, err1 := strconv.Atoi(m[1])
		month, err2 := strconv.Atoi(m[2])
		if err1 == nil && err2 == nil && year >= 1950 && month >= 1 && month <= 12 {
			return parsedYearMonth{year: year, month: month}, true
		}
	}
	if m := resumeDateYearRE.FindStringSubmatch(s); len(m) == 2 {
		year, err := strconv.Atoi(m[1])
		if err == nil && year >= 1950 {
			return parsedYearMonth{year: year, month: 1, yearOnly: true}, true
		}
	}
	if m := resumeDateMonYRRE.FindStringSubmatch(s); len(m) == 3 {
		month := englishMonthMap[strings.ToLower(m[1])]
		year, err := strconv.Atoi(m[2])
		if err == nil && year >= 1950 && month >= 1 {
			return parsedYearMonth{year: year, month: month}, true
		}
	}
	if m := resumeDateMDYRE.FindStringSubmatch(s); len(m) == 3 {
		month, err1 := strconv.Atoi(m[1])
		year, err2 := strconv.Atoi(m[2])
		if err1 == nil && err2 == nil && year >= 1950 && month >= 1 && month <= 12 {
			return parsedYearMonth{year: year, month: month}, true
		}
	}
	return parsedYearMonth{}, false
}

// calculateWorkExperienceYears computes deduplicated non-internship work tenure
// in years (rounded to 1 decimal place).
//
// Rules:
//   - Internships (internship == true) are excluded.
//   - Missing end_date is skipped unless explicitly marked as ongoing ("至今"/"Present").
//   - Overlapping months across concurrent jobs are counted once via a month set
//     using half-open [startYM, endYM) intervals (or 1 month when startYM == endYM).
//   - Returns nil when no valid tenure interval exists (unknown, never 0).
func calculateWorkExperienceYears(items []resumeWorkItemRaw, now time.Time) *float64 {
	if len(items) == 0 {
		return nil
	}
	maxYear := now.Year() + 1
	nowYM := now.Year()*12 + int(now.Month()) - 1
	months := make(map[int]struct{})

	for _, item := range items {
		if bool(item.Internship) {
			continue
		}
		start, ok := parseResumeYearMonth(item.StartDate)
		if !ok || start.year > maxYear {
			continue
		}
		endStr := strings.TrimSpace(item.EndDate)
		if endStr == "" {
			// Missing end date without an explicit ongoing marker is not extrapolated.
			continue
		}
		var endYM int
		if isExplicitOngoingDate(endStr) {
			endYM = nowYM
		} else {
			end, ok := parseResumeYearMonth(endStr)
			if !ok || end.year > maxYear {
				continue
			}
			endYM = end.year*12 + (end.month - 1)
			if start.yearOnly && end.yearOnly && end.year == start.year {
				// Single calendar year such as "2022 - 2022": count 12 months max.
				endYM = start.year*12 + 12
			}
		}
		startYM := start.year*12 + (start.month - 1)
		if endYM < startYM {
			continue
		}
		if endYM == startYM {
			months[startYM] = struct{}{}
			continue
		}
		for ym := startYM; ym < endYM; ym++ {
			months[ym] = struct{}{}
		}
	}

	if len(months) == 0 {
		return nil
	}
	years := math.Round((float64(len(months))/12.0)*10) / 10
	return &years
}

type resumeDegreeAlias struct {
	level   int
	aliases []string
	exact   []string
}

// resumeDegreeAliases is ordered from the highest level to the lowest, so a text
// naming several degrees resolves to the highest. The same table decides whether
// a model-reported degree is written in the resume at all.
var resumeDegreeAliases = []resumeDegreeAlias{
	{level: 6, aliases: []string{"博士", "phd", "ph.d", "doctor"}},
	{level: 5, aliases: []string{"硕士", "研究生", "master", "mba", "emba", "mpa", "m.s", "msc", "m.eng", "meng"}, exact: []string{"m.a.", "ma"}},
	{level: 4, aliases: []string{"本科", "学士", "bachelor", "undergraduate", "b.s", "bsc", "b.eng", "beng"}, exact: []string{"b.a.", "ba"}},
	{level: 3, aliases: []string{"专科", "大专", "高职", "associate", "diploma", "junior college"}},
	{level: 2, aliases: []string{"高中", "中专", "职高", "技校", "high school", "secondary"}},
	{level: 1, aliases: []string{"初中", "middle school"}},
}

// normalizeResumeDegree maps Chinese/English degree text to a canonical label
// and ordinal level (1..6). Returns ("", 0) when unrecognized.
func normalizeResumeDegree(raw string, isEnglish bool) (string, int) {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" {
		return "", 0
	}
	for _, d := range resumeDegreeAliases {
		for _, e := range d.exact {
			if s == e {
				return degreeLabelForLevel(d.level, isEnglish), d.level
			}
		}
		for _, a := range d.aliases {
			if strings.Contains(s, a) {
				return degreeLabelForLevel(d.level, isEnglish), d.level
			}
		}
	}
	return "", 0
}

// resumeDegreeExactREs precompiles word-boundary patterns for the exact-match
// degree aliases ("MA", "M.A.", "BA", "B.A."): every dot becomes an optional
// literal dot, so every spelling of the letters grounds while "Manager" and
// "Baltimore" do not. Without this, a degree normalizeResumeDegree recognizes
// via an exact match could never pass grounding and was always dropped.
var resumeDegreeExactREs = buildResumeDegreeExactREs()

func buildResumeDegreeExactREs() map[int][]*regexp.Regexp {
	out := make(map[int][]*regexp.Regexp)
	for _, d := range resumeDegreeAliases {
		for _, e := range d.exact {
			letters := strings.Map(func(r rune) rune {
				if r == '.' {
					return -1
				}
				return r
			}, e)
			if letters == "" {
				continue
			}
			var b strings.Builder
			b.WriteString(`(?i)\b`)
			for i, r := range letters {
				if i > 0 {
					b.WriteString(`\.?`)
				}
				b.WriteString(regexp.QuoteMeta(string(r)))
			}
			b.WriteString(`\.?\b`)
			out[d.level] = append(out[d.level], regexp.MustCompile(b.String()))
		}
	}
	return out
}

// resumeDegreeGrounded reports whether the resume text names a degree of the
// given level, in any of its Chinese or English spellings.
func resumeDegreeGrounded(rawText string, level int) bool {
	src := strings.ToLower(rawText)
	for _, d := range resumeDegreeAliases {
		if d.level != level {
			continue
		}
		for _, a := range d.aliases {
			if strings.Contains(src, a) {
				return true
			}
		}
	}
	for _, re := range resumeDegreeExactREs[level] {
		if re.MatchString(rawText) {
			return true
		}
	}
	return false
}

var (
	resumeOngoingZHAliases = []string{"至今", "目前", "现在", "在职", "至现在"}
	resumeOngoingENRE      = regexp.MustCompile(`(?i)\b(present|current|currently|now|ongoing|to date)\b`)
)

// resumeDateGrounded reports whether a model-reported date is written in the
// resume text: an ongoing marker needs an ongoing word, a year-month needs that
// year next to that month in a recognised layout, a bare year needs the year.
func resumeDateGrounded(rawText, date string) bool {
	date = strings.TrimSpace(date)
	if date == "" {
		return false
	}
	if isExplicitOngoingDate(date) {
		for _, a := range resumeOngoingZHAliases {
			if strings.Contains(rawText, a) {
				return true
			}
		}
		return resumeOngoingENRE.MatchString(rawText)
	}
	ym, ok := parseResumeYearMonth(date)
	if !ok {
		return false
	}
	year := strconv.Itoa(ym.year)
	if ym.yearOnly {
		return regexp.MustCompile(`(^|\D)` + year + `(\D|$)`).MatchString(rawText)
	}
	month := strconv.Itoa(ym.month)
	names := []string{}
	for name, m := range englishMonthMap {
		if m == ym.month {
			names = append(names, regexp.QuoteMeta(name))
		}
	}
	pattern := `(?i)` +
		`(^|\D)` + year + `\s*[./\-年]\s*0?` + month + `(\D|$)` +
		`|(^|\D)0?` + month + `\s*[./\-]\s*` + year + `(\D|$)` +
		`|(` + strings.Join(names, "|") + `)\.?\s+` + year
	return regexp.MustCompile(pattern).MatchString(rawText)
}

func degreeLabelForLevel(level int, isEnglish bool) string {
	if isEnglish {
		switch level {
		case 6:
			return "PhD"
		case 5:
			return "Master"
		case 4:
			return "Bachelor"
		case 3:
			return "Associate"
		case 2:
			return "High School"
		case 1:
			return "Middle School"
		}
		return ""
	}
	switch level {
	case 6:
		return "博士"
	case 5:
		return "硕士"
	case 4:
		return "本科"
	case 3:
		return "专科"
	case 2:
		return "高中"
	case 1:
		return "初中"
	}
	return ""
}

// Metadata returns the document-level metadata map for indexing and filtering.
// Unknown/missing fields (including unknown work_experience_years) are omitted.
func (p resumeProfile) Metadata() map[string]any {
	meta := make(map[string]any)
	if p.Name != "" {
		meta["name"] = p.Name
	}
	if p.HighestDegree != "" && p.HighestDegreeLevel > 0 {
		meta["highest_degree"] = p.HighestDegree
		meta["highest_degree_level"] = p.HighestDegreeLevel
	}
	if p.WorkExperienceYears != nil {
		meta["work_experience_years"] = *p.WorkExperienceYears
	}
	if len(p.Skills) > 0 {
		meta["skills"] = p.Skills
	}
	if len(p.Certificates) > 0 {
		meta["certificates"] = p.Certificates
	}
	if len(p.ExpectedPositions) > 0 {
		meta["expected_positions"] = p.ExpectedPositions
	}
	if len(p.ExpectedCities) > 0 {
		meta["expected_cities"] = p.ExpectedCities
	}
	if p.CurrentLocation != "" {
		meta["current_location"] = p.CurrentLocation
	}
	if len(p.Companies) > 0 {
		meta["companies"] = p.Companies
	}
	if len(p.Positions) > 0 {
		meta["positions"] = p.Positions
	}
	if len(p.Schools) > 0 {
		meta["schools"] = p.Schools
	}
	if len(p.Majors) > 0 {
		meta["majors"] = p.Majors
	}
	if len(p.Languages) > 0 {
		meta["languages"] = p.Languages
	}
	return meta
}

// ImportantKeywords builds a bounded, deduplicated list of high-signal technical
// terms, roles, and project phrases for important_kwd / important_tks boosting.
func (p resumeProfile) ImportantKeywords() []string {
	candidates := make([]string, 0, len(p.Skills)+len(p.Positions)+len(p.ExpectedPositions)+len(p.ProjectEntries)+len(p.Certificates))
	candidates = append(candidates, p.Skills...)
	candidates = append(candidates, p.Positions...)
	candidates = append(candidates, p.ExpectedPositions...)
	for _, proj := range p.ProjectEntries {
		if proj.ProjectName != "" {
			candidates = append(candidates, proj.ProjectName)
		}
		if proj.Role != "" {
			candidates = append(candidates, proj.Role)
		}
	}
	candidates = append(candidates, p.Certificates...)

	deduped := dedupePreserveVerbatim(candidates)
	out := make([]string, 0, min(len(deduped), resumeMaxImportantKeywords))
	for _, kw := range deduped {
		kw = sanitizeKeywordTerm(kw)
		if kw == "" {
			continue
		}
		out = append(out, kw)
		if len(out) >= resumeMaxImportantKeywords {
			break
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// EmbeddingSummary builds a concise, high-signal structured summary ordered by
// semantic retrieval priority (roles/skills -> work highlights -> deduplicated
// project highlights -> education/tenure), capped to resumeSummaryMaxTokens.
func (p resumeProfile) EmbeddingSummary() string {
	var parts []string

	roles := dedupePreserveVerbatim(append(append([]string(nil), p.Positions...), p.ExpectedPositions...))
	if len(roles) > 0 {
		parts = append(parts, formatLabeledLine(p.IsEnglish, "Roles", "职位方向", strings.Join(roles, " / ")))
	}
	if len(p.Skills) > 0 {
		parts = append(parts, formatLabeledLine(p.IsEnglish, "Skills", "核心技能", strings.Join(p.Skills, ", ")))
	}

	workDescs := make([]string, 0, len(p.WorkEntries))
	var workHighlights []string
	for _, w := range p.WorkEntries {
		if w.Description != "" {
			workDescs = append(workDescs, w.Description)
		}
		var header []string
		if w.Company != "" {
			header = append(header, w.Company)
		}
		if w.Position != "" {
			header = append(header, w.Position)
		}
		snippet := compactResumeSnippet(w.Description, 140)
		entry := strings.Join(header, " - ")
		if snippet != "" {
			if entry != "" {
				entry += ": " + snippet
			} else {
				entry = snippet
			}
		}
		if entry != "" {
			workHighlights = append(workHighlights, entry)
		}
	}
	if len(workHighlights) > 0 {
		parts = append(parts, formatLabeledLine(p.IsEnglish, "Work Experience", "工作经历", strings.Join(workHighlights, "; ")))
	}

	var projectHighlights []string
	for _, proj := range p.ProjectEntries {
		desc := proj.Description
		if desc != "" && overlapsAnyText(desc, workDescs) {
			desc = ""
		}
		var header []string
		if proj.ProjectName != "" {
			header = append(header, proj.ProjectName)
		}
		if proj.Role != "" {
			header = append(header, proj.Role)
		}
		snippet := compactResumeSnippet(desc, 140)
		entry := strings.Join(header, " - ")
		if snippet != "" {
			if entry != "" {
				entry += ": " + snippet
			} else {
				entry = snippet
			}
		}
		if entry != "" {
			projectHighlights = append(projectHighlights, entry)
		}
	}
	if len(projectHighlights) > 0 {
		parts = append(parts, formatLabeledLine(p.IsEnglish, "Projects", "项目经历", strings.Join(projectHighlights, "; ")))
	}

	var profileFacts []string
	if p.HighestDegree != "" {
		profileFacts = append(profileFacts, p.HighestDegree)
	}
	if p.WorkExperienceYears != nil {
		if p.IsEnglish {
			profileFacts = append(profileFacts, fmt.Sprintf("%gyrs", *p.WorkExperienceYears))
		} else {
			profileFacts = append(profileFacts, fmt.Sprintf("%g年经验", *p.WorkExperienceYears))
		}
	}
	if len(p.Schools) > 0 {
		profileFacts = append(profileFacts, strings.Join(p.Schools, "/"))
	}
	if len(p.Majors) > 0 {
		profileFacts = append(profileFacts, strings.Join(p.Majors, "/"))
	}
	if len(p.Certificates) > 0 {
		profileFacts = append(profileFacts, strings.Join(p.Certificates, ", "))
	}
	if len(profileFacts) > 0 {
		parts = append(parts, formatLabeledLine(p.IsEnglish, "Profile", "背景概况", strings.Join(profileFacts, " | ")))
	}

	summary := strings.TrimSpace(strings.Join(parts, "\n"))
	if summary == "" {
		return ""
	}
	return strings.TrimSpace(tokenizer.TrimContentToTokenLimit(summary, resumeSummaryMaxTokens))
}

// BuildEmbeddingText prepends the verified structured summary to the original
// resume text. When all four lanes produce nothing, it falls back verbatim to
// rawText so the chunk always receives an embedding vector.
func (p resumeProfile) BuildEmbeddingText(rawText string) string {
	summary := p.EmbeddingSummary()
	if summary == "" {
		return rawText
	}
	return summary + "\n\n" + rawText
}

func formatLabeledLine(isEnglish bool, enLabel, zhLabel, val string) string {
	if isEnglish {
		return enLabel + ": " + val
	}
	return zhLabel + ": " + val
}

func compactResumeSnippet(s string, maxRunes int) string {
	s = strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
	if s == "" {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes])
}

func overlapsAnyText(candidate string, refs []string) bool {
	normCand := normalizeForGrounding(candidate)
	if normCand == "" || len(refs) == 0 {
		return false
	}
	candShingles := runeShingles(normCand, 4)
	for _, ref := range refs {
		normRef := normalizeForGrounding(ref)
		if normRef == "" {
			continue
		}
		if strings.Contains(normRef, normCand) || strings.Contains(normCand, normRef) {
			return true
		}
		if shingleJaccard(candShingles, runeShingles(normRef, 4)) >= resumeOverlapJaccardThreshold {
			return true
		}
	}
	return false
}

func runeShingles(s string, k int) map[string]struct{} {
	runes := []rune(s)
	if len(runes) == 0 {
		return nil
	}
	if len(runes) <= k {
		return map[string]struct{}{s: {}}
	}
	out := make(map[string]struct{}, len(runes)-k+1)
	for i := 0; i <= len(runes)-k; i++ {
		out[string(runes[i:i+k])] = struct{}{}
	}
	return out
}

func shingleJaccard(a, b map[string]struct{}) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	inter := 0
	for k := range a {
		if _, ok := b[k]; ok {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}
