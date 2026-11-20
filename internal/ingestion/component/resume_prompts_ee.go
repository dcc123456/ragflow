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
	"strings"
	"unicode"
)

type resumeLane string

const (
	resumeLaneBasic     resumeLane = "basic_info"
	resumeLaneWork      resumeLane = "work_exp"
	resumeLaneEducation resumeLane = "education"
	resumeLaneProject   resumeLane = "project_exp"
)

var resumeLanes = []resumeLane{
	resumeLaneBasic,
	resumeLaneWork,
	resumeLaneEducation,
	resumeLaneProject,
}

// resumeLaneContractKeys lists the top-level JSON keys a compliant lane reply
// carries at least one of. A bare {} — or an object holding none of the
// lane's keys — is a noncompliant reply: it confirms nothing about the
// resume, is never cached, and clears no metadata on re-parse. The basic
// lane's keys derive from resumeBasicManagedKeySources, so the reply
// contract and the managed-key mapping cannot drift apart.
var resumeLaneContractKeys = map[resumeLane][]string{
	resumeLaneBasic:     basicResumeContractKeys(),
	resumeLaneWork:      {"workExperience"},
	resumeLaneEducation: {"education"},
	resumeLaneProject:   {"projectExperience"},
}

const (
	resumeSystemPromptZH = `你是一个专业的简历分析助手。你的任务是将给定的简历文本转换为 JSON 输出。
严格按照 JSON 格式返回结果，不要有任何其他文字。`

	resumeSystemPromptEN = `You are a professional resume analysis assistant. Your task is to convert the given resume text into JSON output.
Return strictly valid JSON only, with no additional commentary or markdown.`

	resumeBasicInfoPromptZH = `请从用户消息中带行号索引的简历文本中提取基本信息。

提取如下信息到 JSON，若某些字段不存在则输出 "" 或 []:
{
  "name_kwd": "",
  "current_location": "",
  "expect_city_names_tks": [],
  "expect_position_name_tks": [],
  "skill_tks": [],
  "language_tks": [],
  "certificate_tks": []
}

字段说明:
- name_kwd: 姓名，如"张三"，遵循原文不要猜测
- current_location: 现居地/当前城市，不要从工作经历中推测，仅在原文写明时填写
- expect_city_names_tks: 期望工作城市列表，简历中需要明确说明是期望城市
- expect_position_name_tks: 期望职位/求职意向列表
- skill_tks: 技能/技术栈列表，保留原文写法
- language_tks: 语言能力列表
- certificate_tks: 证书/资质列表，保留原文写法

只返回 JSON。`

	resumeWorkExpPromptZH = `请从用户消息中带行号索引的简历文本中提取工作经历。

提取为 JSON，每段工作经历包含:
{
  "workExperience": [
    {
      "company": "",
      "position": "",
      "internship": 0,
      "start_date": "",
      "end_date": "",
      "desc_lines": [start_index, end_index]
    }
  ]
}

字段说明:
- company: 公司全称（含括号内地区信息），遵循原文不要编造
- position: 职位名称，遵循原文不要编造或推测
- internship: 该段经历是否是实习，是实习为1，不是为0
- start_date: 入职时间，格式为 YYYY.MM 或 YYYY，如 "2024.1"
- end_date: 离职时间，仅当原文明确写"至今/目前/在职/Present"时填写"至今"，若缺失结束日期则填写""
- desc_lines: [起始行号, 结束行号]，工作描述对应的闭区间行号范围（整数数组）
  - 指工作经历描述的原文引用行号范围，包括工作成果、业绩、主要工作、技术栈等
  - 不包括 company、position、start_date、end_date 所在行
  - 遇到下一段工作经历或其他段落标题（自我评价、专业技能、教育背景、项目经历、证书资质、求职意向等）时必须截止
  - 如果不存在就写 []

示例:
[22]: 阿里巴巴 2021.11-2022.11 高级工程师
[23]: 工作描述: 负责支付核心链路重构
[24]: 保障迁移期间双写校验与差异补偿
则 desc_lines 应为 [23, 24]

只返回 JSON。`

	resumeEducationPromptZH = `请从用户消息中带行号索引的简历文本中提取教育背景。

提取为 JSON:
{
  "education": [
    {
      "school": "",
      "major": "",
      "degree": "",
      "department": "",
      "start_date": "",
      "end_date": "",
      "desc_lines": [start_index, end_index]
    }
  ]
}

字段说明:
- school: 学校全称，如"厦门大学"，中英文都可以，遵循原文
- major: 专业，如"计算机科学与技术"
- degree: 学位，如 博士/硕士/本科/专科/高中/初中，若不存在则填""
- department: 系/学院，如"信息工程系"
- start_date: 开始时间，格式为 YYYY.MM 或 YYYY
- end_date: 结束时间，仅当原文明确写至今时填写"至今"，若不存在填写""
- desc_lines: [起始行号, 结束行号]，教育描述对应的行号范围（可选），不存在填 []

只返回 JSON。`

	resumeProjectExpPromptZH = `请从用户消息中带行号索引的简历文本中提取项目经验。

提取为 JSON，每段项目经验包含:
{
  "projectExperience": [
    {
      "project_name": "",
      "role": "",
      "start_date": "",
      "end_date": "",
      "desc_lines": [start_index, end_index]
    }
  ]
}

字段说明:
- project_name: 项目名称，遵循原文
- role: 担任角色/职责，如"项目负责人"、"后端开发"
- start_date: 开始时间，格式为 YYYY.MM 或 YYYY
- end_date: 结束时间，仅当原文明确写至今时填写"至今"，若不存在填写""
- desc_lines: [起始行号, 结束行号]，项目描述对应的闭区间行号范围（整数数组）
  - 指项目描述的原文引用行号范围，包括项目背景、方案、技术栈、成果等
  - 不包括 project_name、role、start_date、end_date 所在行
  - 遇到下一个项目或其他段落标题（自我评价、专业技能、教育背景、工作经历、证书资质、求职意向等）时必须截止
  - 如果不存在就写 []

只返回 JSON。`

	resumeBasicInfoPromptEN = `Please extract basic information from the line-indexed resume text provided in the user message.

Extract into JSON. Leave empty string "" or empty list [] if a field is not present:
{
  "name_kwd": "",
  "current_location": "",
  "expect_city_names_tks": [],
  "expect_position_name_tks": [],
  "skill_tks": [],
  "language_tks": [],
  "certificate_tks": []
}

Field descriptions:
- name_kwd: Full name verbatim from the resume, do not fabricate
- current_location: Current city/location, only if explicitly stated
- expect_city_names_tks: Preferred work cities if explicitly stated
- expect_position_name_tks: Desired positions / career objective titles
- skill_tks: Skills/tech stack list verbatim from the resume
- language_tks: Language proficiencies
- certificate_tks: Certificates/qualifications verbatim from the resume

Return JSON only.`

	resumeWorkExpPromptEN = `Please extract work experience from the line-indexed resume text provided in the user message.

Extract into JSON, each work experience entry contains:
{
  "workExperience": [
    {
      "company": "",
      "position": "",
      "internship": 0,
      "start_date": "",
      "end_date": "",
      "desc_lines": [start_index, end_index]
    }
  ]
}

Field descriptions:
- company: Full company name verbatim from the resume
- position: Job title verbatim from the resume, do not fabricate
- internship: 1 if this entry is an internship, 0 otherwise
- start_date: Start date in YYYY.MM or YYYY format, e.g. "2024.1"
- end_date: End date; write "Present" ONLY if the resume explicitly says Present/Current/Now, otherwise "" if missing
- desc_lines: [start_index, end_index], inclusive line number range for job description
  - Covers achievements, responsibilities, tech stack, etc.
  - Excludes lines containing company, position, start_date, end_date
  - Stop before the next work entry or another section heading (Summary, Skills, Education, Projects, Certificates, Languages, Objective)
  - Use [] if not available

Example:
[22]: Google Inc. 2021.11-2022.11 Senior Engineer
[23]: Led backend migration of payment ledger
[24]: Implemented dual-write verification and reconciliation
Then desc_lines should be [23, 24]

Return JSON only.`

	resumeEducationPromptEN = `Please extract education background from the line-indexed resume text provided in the user message.

Extract into JSON:
{
  "education": [
    {
      "school": "",
      "major": "",
      "degree": "",
      "department": "",
      "start_date": "",
      "end_date": "",
      "desc_lines": [start_index, end_index]
    }
  ]
}

Field descriptions:
- school: Full school name verbatim from the resume
- major: Major/field of study
- degree: Degree level (PhD/Master/Bachelor/Associate/High School/Middle School), leave "" if not present
- department: Department/College
- start_date: Start date in YYYY.MM or YYYY format
- end_date: End date; write "Present" only if explicitly stated, otherwise ""
- desc_lines: [start_index, end_index], optional line number range for coursework/honors/GPA; use [] if not available

Return JSON only.`

	resumeProjectExpPromptEN = `Please extract project experience from the line-indexed resume text provided in the user message.

Extract into JSON, each project experience entry contains:
{
  "projectExperience": [
    {
      "project_name": "",
      "role": "",
      "start_date": "",
      "end_date": "",
      "desc_lines": [start_index, end_index]
    }
  ]
}

Field descriptions:
- project_name: Project name verbatim from the resume
- role: Role/responsibility, e.g. "Project Lead", "Backend Developer"
- start_date: Start date in YYYY.MM or YYYY format
- end_date: End date; write "Present" only if explicitly stated, otherwise ""
- desc_lines: [start_index, end_index], inclusive line number range for project description
  - Excludes lines containing project_name, role, start_date, end_date
  - Stop before the next project entry or another section heading (Summary, Skills, Education, Work Experience, Certificates, Languages, Objective)
  - Use [] if not available

Return JSON only.`
)

// detectResumeEnglish decides whether a resume should use English prompts and
// labels. Resume content is authoritative because datasets often keep the
// default "English" language even when ingesting Chinese resumes.
func detectResumeEnglish(text, lang string) bool {
	var hanCount, latinCount int
	for _, r := range text {
		switch {
		case unicode.Is(unicode.Han, r):
			hanCount++
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'):
			latinCount++
		}
	}
	if hanCount >= 8 || (hanCount > 0 && hanCount*10 >= hanCount+latinCount) {
		return false
	}
	if latinCount > 0 {
		return true
	}
	l := strings.ToLower(strings.TrimSpace(lang))
	return l == "english" || l == "en"
}

func resumeSystemPrompt(isEnglish bool) string {
	if isEnglish {
		return resumeSystemPromptEN
	}
	return resumeSystemPromptZH
}

// resumeLaneInstructions returns the lane's output contract (JSON schema, field
// rules, line-number rules) WITHOUT the resume text. The contract travels in the
// system message and the indexed resume travels in the user message: the shared
// message fitter keeps system messages intact and trims the long user message,
// so an oversized resume loses its tail instead of the output schema.
func resumeLaneInstructions(lane resumeLane, isEnglish bool) string {
	var tmpl string
	if isEnglish {
		switch lane {
		case resumeLaneBasic:
			tmpl = resumeBasicInfoPromptEN
		case resumeLaneWork:
			tmpl = resumeWorkExpPromptEN
		case resumeLaneEducation:
			tmpl = resumeEducationPromptEN
		case resumeLaneProject:
			tmpl = resumeProjectExpPromptEN
		}
	} else {
		switch lane {
		case resumeLaneBasic:
			tmpl = resumeBasicInfoPromptZH
		case resumeLaneWork:
			tmpl = resumeWorkExpPromptZH
		case resumeLaneEducation:
			tmpl = resumeEducationPromptZH
		case resumeLaneProject:
			tmpl = resumeProjectExpPromptZH
		}
	}
	return resumeSystemPrompt(isEnglish) + "\n\n" + strings.TrimSpace(tmpl)
}
