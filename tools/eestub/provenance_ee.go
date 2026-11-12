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

package main

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type reportOptions struct {
	baseRef, repository, upstream, summaryPath, apiURL, token string
}

type declarationIssue struct {
	kind, symbol, file, generated, reason string
	source                                *sourceFile
	fn                                    *ast.FuncDecl
	ee                                    *declaration
}

func (issue declarationIssue) isWarning() bool {
	return issue.kind == "OSS_IMPLEMENTED" || issue.kind == "REVIEW"
}

type sourcePR struct {
	Number      int    `json:"number"`
	URL         string `json:"html_url"`
	Title       string `json:"title"`
	MergeCommit string `json:"merge_commit_sha"`
	MergedAt    string `json:"merged_at"`
	User        struct {
		Login string `json:"login"`
	} `json:"user"`
	Head struct {
		SHA string `json:"sha"`
	} `json:"head"`
	repository string
}

type provenance struct {
	origin, change, commit, previous string
	pr                               sourcePR
}

type packageSnapshot struct {
	issues []declarationIssue
	files  []*sourceFile
	err    error
}

type sourceResolver struct {
	root      string
	opts      reportOptions
	head      string
	baseValid bool
	shallow   bool
	snapshots map[string]packageSnapshot
	prs       map[string]sourcePR
	client    *http.Client
}

func (r *sourceResolver) git(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = r.root
	return cmd.Output()
}

func newSourceResolver(root string, opts reportOptions) *sourceResolver {
	r := &sourceResolver{root: root, opts: opts, snapshots: map[string]packageSnapshot{}, prs: map[string]sourcePR{}, client: &http.Client{Timeout: 8 * time.Second}}
	if opts.baseRef == "" && opts.repository == "" && opts.upstream == "" {
		return r
	}
	if data, err := r.git("rev-parse", "HEAD"); err == nil {
		r.head = strings.TrimSpace(string(data))
	}
	if opts.baseRef != "" {
		_, err := r.git("cat-file", "-e", opts.baseRef+"^{commit}")
		r.baseValid = err == nil
	}
	data, _ := r.git("rev-parse", "--is-shallow-repository")
	r.shallow = strings.TrimSpace(string(data)) == "true"
	if r.opts.repository == "" {
		data, _ := r.git("remote", "get-url", "origin")
		remote := strings.TrimSuffix(strings.TrimSpace(string(data)), ".git")
		if i := strings.Index(remote, "github.com/"); i >= 0 {
			r.opts.repository = remote[i+len("github.com/"):]
		} else if i := strings.Index(remote, "github.com:"); i >= 0 {
			r.opts.repository = remote[i+len("github.com:"):]
		}
	}
	if r.opts.apiURL == "" {
		r.opts.apiURL = "https://api.github.com"
	}
	return r
}

// Reuse the actual scanner on the base package, rather than treating an old
// declaration as proof of an old gap: the EE implementation may have been
// removed by this merge. Only direct Go files are read from Git, never models.
func (r *sourceResolver) snapshot(dir string) packageSnapshot {
	if snapshot, ok := r.snapshots[dir]; ok {
		return snapshot
	}
	snapshot := packageSnapshot{}
	defer func() { r.snapshots[dir] = snapshot }()
	if dir != "." {
		if _, err := r.git("cat-file", "-e", r.opts.baseRef+":"+dir); err != nil {
			return snapshot // The package did not exist in the valid base tree.
		}
	}
	pattern := ":(glob)" + filepath.ToSlash(filepath.Join(dir, "*.go"))
	data, err := r.git("archive", r.opts.baseRef, "--", pattern)
	if err != nil {
		snapshot.err = err
		return snapshot
	}
	tmp, err := os.MkdirTemp("", "eestub-base-")
	if err != nil {
		snapshot.err = err
		return snapshot
	}
	defer os.RemoveAll(tmp)
	reader := tar.NewReader(bytes.NewReader(data))
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			snapshot.err = err
			return snapshot
		}
		name := filepath.Base(header.Name)
		if header.Typeflag != tar.TypeReg || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || strings.HasPrefix(name, generatedPrefix) {
			continue
		}
		contents, err := io.ReadAll(reader)
		if err == nil {
			err = os.WriteFile(filepath.Join(tmp, name), contents, 0644)
		}
		if err != nil {
			snapshot.err = err
			return snapshot
		}
	}
	snapshot.err = generatePackage(tmp, tmp, false, io.Discard, &snapshot.issues)
	if snapshot.err == nil {
		snapshot.files, _, snapshot.err = readPackage(tmp)
	}
	return snapshot
}

func declarationText(fn *ast.FuncDecl) string {
	copy := *fn
	copy.Doc, copy.Body = nil, nil
	return strings.ReplaceAll(printNode(&copy), "\n", " ")
}

func issueKind(kind string) string {
	if kind == "GENERATED" {
		return "MISSING"
	}
	return kind
}

func (r *sourceResolver) resolve(issue declarationIssue) provenance {
	p := provenance{origin: "Unknown", change: "Unknown", commit: "Unknown"}
	if r.baseValid {
		base := r.snapshot(filepath.ToSlash(filepath.Dir(issue.file)))
		if base.err == nil {
			p.origin = "Detected in this merge"
			p.change = "Added declaration to !ee stub"
			for _, old := range base.issues {
				if functionKey(old.fn) != functionKey(issue.fn) {
					continue
				}
				sameReason := issueKind(issue.kind) != "UNSUPPORTED" || old.reason == issue.reason
				if issueKind(old.kind) == issueKind(issue.kind) && signature(old.fn, old.source) == signature(issue.fn, issue.source) && sameReason {
					if old.ee == nil && issue.ee == nil || old.ee != nil && issue.ee != nil && signature(old.ee.fn, old.ee.file) == signature(issue.ee.fn, issue.ee.file) {
						p.origin = "Pre-existing"
					}
				}
			}
			for _, file := range base.files {
				if !file.stub && !file.shared {
					continue
				}
				for _, decl := range file.file.Decls {
					fn, ok := decl.(*ast.FuncDecl)
					if !ok || functionKey(fn) != functionKey(issue.fn) || file.active && !file.shared {
						continue
					}
					p.previous = declarationText(fn)
					if file.shared && filepath.Base(issue.file) != file.name {
						p.change = "Moved from shared " + file.name + " into a !ee stub"
					} else if signature(fn, file) != signature(issue.fn, issue.source) {
						p.change = "Changed declaration signature"
					} else if p.origin == "Pre-existing" {
						p.change = "Declaration and EE gap unchanged from base"
					} else {
						p.change = "EE declaration or dependencies changed from base"
					}
				}
			}
		}
	}
	if r.head == "" {
		return p
	}
	if _, err := r.git("diff", "--quiet", r.head, "--", issue.file); err != nil {
		return p // An uncommitted signature has no reliable source commit yet.
	}
	start := issue.source.fset.Position(issue.fn.Pos()).Line
	end := issue.source.fset.Position(issue.fn.Type.End()).Line
	data, err := r.git("blame", "--line-porcelain", "-w", "-L", fmt.Sprintf("%d,%d", start, end), r.head, "--", issue.file)
	if err != nil {
		return p
	}
	commits := map[string]bool{}
	current := ""
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && len(fields[0]) == 40 {
			if _, err := strconv.ParseInt(fields[1], 10, 64); err == nil {
				current = fields[0]
				commits[current] = true
			}
		}
		if line == "boundary" && r.shallow {
			delete(commits, current)
		}
	}
	latest := int64(-1)
	subject := ""
	for commit := range commits {
		data, err := r.git("show", "-s", "--format=%ct%x00%s", commit)
		if err != nil {
			continue
		}
		fields := strings.SplitN(strings.TrimSpace(string(data)), "\x00", 2)
		if len(fields) != 2 {
			continue
		}
		stamp, err := strconv.ParseInt(fields[0], 10, 64)
		newer := stamp > latest
		if err == nil && stamp == latest {
			_, ancestorErr := r.git("merge-base", "--is-ancestor", p.commit, commit)
			newer = ancestorErr == nil
		}
		if err == nil && newer {
			latest, p.commit, subject = stamp, commit, fields[1]
		}
	}
	if p.commit != "Unknown" {
		p.pr = r.lookupPR(p.commit, subject)
	}
	return p
}

var subjectPR = regexp.MustCompile(`\(#([0-9]+)\)\s*$`)

func (r *sourceResolver) lookupPR(commit, subject string) sourcePR {
	if pr, ok := r.prs[commit]; ok {
		return pr
	}
	var found sourcePR
	defer func() { r.prs[commit] = found }()
	seen := map[string]bool{}
	for _, repository := range []string{r.opts.upstream, r.opts.repository} {
		if repository == "" || seen[repository] {
			continue
		}
		seen[repository] = true
		endpoint := strings.TrimRight(r.opts.apiURL, "/") + "/repos/" + repository + "/commits/" + commit + "/pulls?per_page=100"
		request, err := http.NewRequest(http.MethodGet, endpoint, nil)
		if err != nil {
			continue
		}
		request.Header.Set("Accept", "application/vnd.github+json")
		request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		if r.opts.token != "" {
			request.Header.Set("Authorization", "Bearer "+r.opts.token)
		}
		response, err := r.client.Do(request)
		if err != nil {
			continue
		}
		var prs []sourcePR
		if response.StatusCode == http.StatusOK {
			err = json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&prs)
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusOK || err != nil {
			continue
		}
		match := subjectPR.FindStringSubmatch(subject)
		for _, pr := range prs {
			if pr.MergeCommit == commit || pr.Head.SHA == commit || len(match) > 1 && strconv.Itoa(pr.Number) == match[1] {
				pr.repository = repository
				found = pr
				return found
			}
		}
		// An association alone can be a later synchronization PR. Use an
		// unambiguous merged source PR; otherwise leave the attribution unknown.
		var merged []sourcePR
		for _, pr := range prs {
			if pr.MergedAt != "" {
				merged = append(merged, pr)
			}
		}
		if repository == r.opts.upstream && len(merged) == 1 {
			found = merged[0]
			found.repository = repository
			return found
		}
	}
	return found
}

func reportIssues(root string, issues []declarationIssue, opts reportOptions, out io.Writer) error {
	r := newSourceResolver(root, opts)
	provenances := make([]provenance, len(issues))
	newCount, oldCount := 0, 0
	order := make([]int, len(issues))
	for i, issue := range issues {
		p := r.resolve(issue)
		provenances[i] = p
		order[i] = i
		if p.origin == "Detected in this merge" {
			newCount++
		}
		if p.origin == "Pre-existing" {
			oldCount++
		}
	}
	sort.SliceStable(order, func(i, j int) bool {
		left, right := order[i], order[j]
		if provenances[left].pr.URL != provenances[right].pr.URL {
			return provenances[left].pr.URL < provenances[right].pr.URL
		}
		return issues[left].symbol < issues[right].symbol
	})
	for _, i := range order {
		issue, p := issues[i], provenances[i]
		pr, author := "Unknown", "Unknown"
		if p.pr.URL != "" {
			pr = p.pr.URL
			if p.pr.Title != "" {
				pr += " — " + p.pr.Title
			}
		}
		if p.pr.User.Login != "" {
			author = "@" + p.pr.User.Login
		}
		severity, color, command, title := "ERROR", "31", "error", "Missing EE implementation"
		if issue.isWarning() {
			severity, color, command, title = "WARNING", "33", "warning", "EE hook review"
		}
		fmt.Fprintf(out, "\n\x1b[%sm%s [%s] %s\x1b[0m\n", color, severity, issueKind(issue.kind), issue.symbol)
		fmt.Fprintf(out, "\x1b[31m  Source PR:      %s\x1b[0m\n", pr)
		fmt.Fprintf(out, "\x1b[31m  PR author:      %s\x1b[0m\n", author)
		fmt.Fprintf(out, "  Location:       %s:%d\n  Origin:         %s\n  Source commit:  %s\n  Change:         %s\n", issue.file, issue.source.fset.Position(issue.fn.Pos()).Line, p.origin, p.commit, p.change)
		if p.previous != "" && p.previous != declarationText(issue.fn) {
			fmt.Fprintf(out, "  Previous:       %s\n", p.previous)
		}
		fmt.Fprintf(out, "  Expected:       %s\n", declarationText(issue.fn))
		if issue.ee != nil {
			fmt.Fprintf(out, "  EE declaration: %s:%d\n  EE signature:   %s\n", filepath.ToSlash(filepath.Join(filepath.Dir(issue.file), issue.ee.file.name)), issue.ee.file.fset.Position(issue.ee.fn.Pos()).Line, declarationText(issue.ee.fn))
		}
		if issue.generated != "" {
			fmt.Fprintf(out, "  Generated file: %s\n\x1b[31m  - %s: panic fallback (%s)\x1b[0m\n", issue.generated, issue.symbol, filepath.Base(issue.generated))
		} else {
			fmt.Fprintf(out, "  Generated file: None\n  Reason:         %s\n", issue.reason)
		}
		if os.Getenv("GITHUB_ACTIONS") == "true" {
			instruction := "submit the required EE changes on this PR branch."
			if issue.isWarning() {
				instruction = "review the hook; this warning does not fail the check."
			}
			message := strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A").Replace(issue.symbol + "; Source PR: " + pr + "; PR author: " + author + "; " + instruction)
			fmt.Fprintf(out, "::%s title=%s::%s\n", command, title, message)
		}
	}
	fmt.Fprintf(out, "\nDetected in this merge: %d\nPre-existing: %d\nOrigin unknown: %d\n", newCount, oldCount, len(issues)-newCount-oldCount)
	if opts.summaryPath == "" {
		return nil
	}
	file, err := os.OpenFile(opts.summaryPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer file.Close()
	var summary strings.Builder
	fmt.Fprintf(&summary, "### Enterprise declaration issues\n\nDetected in this merge: **%d** · Pre-existing: **%d** · Origin unknown: **%d**\n\n", newCount, oldCount, len(issues)-newCount-oldCount)
	groups := map[string][]int{}
	for _, i := range order {
		groups[provenances[i].pr.URL] = append(groups[provenances[i].pr.URL], i)
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		p := provenances[groups[key][0]]
		pr, author := "Unknown", "Unknown"
		if p.pr.URL != "" {
			pr = fmt.Sprintf("[%s#%d](%s)", p.pr.repository, p.pr.Number, p.pr.URL)
		}
		if p.pr.User.Login != "" {
			author = fmt.Sprintf("[@%s](https://github.com/%s)", p.pr.User.Login, p.pr.User.Login)
		}
		level := "WARNING"
		for _, i := range groups[key] {
			if !issues[i].isWarning() {
				level = "CAUTION"
			}
		}
		fmt.Fprintf(&summary, "> [!%s]\n> Source PR: **%s**\n> PR author: **%s**\n\n", level, pr, author)
		summary.WriteString("| Declaration | Severity | Status | Origin | Location | Source commit |\n|---|---|---|---|---|---|\n")
		for _, i := range groups[key] {
			issue := issues[i]
			line := issue.source.fset.Position(issue.fn.Pos()).Line
			location := fmt.Sprintf("%s:%d", issue.file, line)
			if r.opts.repository != "" && r.head != "" {
				link := (&url.URL{Scheme: "https", Host: "github.com", Path: "/" + r.opts.repository + "/blob/" + r.head + "/" + issue.file, Fragment: fmt.Sprintf("L%d", line)}).String()
				location = fmt.Sprintf("[%s](%s)", location, link)
			}
			commit := provenances[i].commit
			if commit != "Unknown" && r.opts.repository != "" {
				repository := r.opts.repository
				if provenances[i].pr.repository != "" {
					repository = provenances[i].pr.repository
				}
				commit = fmt.Sprintf("[%s](https://github.com/%s/commit/%s)", commit[:8], repository, commit)
			}
			severity := "ERROR"
			if issue.isWarning() {
				severity = "WARNING"
			}
			fmt.Fprintf(&summary, "| `%s` | %s | %s | %s | %s | %s |\n", strings.ReplaceAll(issue.symbol, "|", "\\|"), severity, issueKind(issue.kind), provenances[i].origin, location, commit)
		}
		summary.WriteString("\n")
	}
	_, err = io.WriteString(file, summary.String())
	return err
}
