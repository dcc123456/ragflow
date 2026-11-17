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

package pipeline

import (
	"embed"
	"io/fs"
	"path"
	"sort"
	"strings"
)

const (
	// sharedTemplateDir is the embed directory owned by the community tree.
	sharedTemplateDir = "template"
	// editionTemplateDir holds built-in pipelines that only the enterprise
	// edition ships. Assets must live outside sharedTemplateDir: the shared
	// embed glob reaches every JSON under it, so a community build would
	// discover a pipeline whose components it never registers.
	editionTemplateDir = "template_ee"
)

//go:embed template_ee/*.json
var editionTemplateFS embed.FS

// editionTemplatesFS exposes sharedTemplateDir as the union of the shared
// templates and the enterprise ones. Keeping the merge at the fs.FS layer lets
// the shared registry own parsing, duplicate detection and ordering, so this
// file cannot drift from NewRegistryFromFS.
type editionTemplatesFS struct{}

var _ fs.ReadDirFS = editionTemplatesFS{}

func (editionTemplatesFS) Open(name string) (fs.File, error) {
	file, err := builtinTemplateFS.Open(name)
	if err == nil {
		return file, nil
	}
	if editionName, ok := editionTemplatePath(name); ok {
		if editionFile, editionErr := editionTemplateFS.Open(editionName); editionErr == nil {
			return editionFile, nil
		}
	}
	return nil, err
}

func (editionTemplatesFS) ReadDir(name string) ([]fs.DirEntry, error) {
	entries, err := fs.ReadDir(builtinTemplateFS, name)
	if err != nil {
		return nil, err
	}
	if name != sharedTemplateDir {
		return entries, nil
	}
	editionEntries, err := fs.ReadDir(editionTemplateFS, editionTemplateDir)
	if err != nil {
		return nil, err
	}
	entries = append(entries, editionEntries...)
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	return entries, nil
}

// editionTemplatePath maps a shared-directory path to its enterprise asset.
func editionTemplatePath(name string) (string, bool) {
	rest, ok := strings.CutPrefix(name, sharedTemplateDir+"/")
	if !ok {
		return "", false
	}
	return path.Join(editionTemplateDir, rest), true
}

// init owns DefaultRegistry's first build so the edition templates are part of
// it. This must run before any other init in this package that would call
// DefaultRegistry: a registry built from builtinTemplateFS alone silently drops
// every template_ee/ asset. TestDefaultRegistryIncludesEditionTemplates fails
// loudly if that ever happens. When the shared tree grows an explicit edition
// seam, delete this file and load template_ee/ through that seam instead.
func init() {
	defaultRegistryOnce.Do(func() {
		defaultRegistry, defaultRegistryErr = NewRegistryFromFS(editionTemplatesFS{}, sharedTemplateDir)
	})
}
