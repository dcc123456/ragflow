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
	// sharedTemplateDir is the embed directory every build reads.
	sharedTemplateDir = "template"
	// editionTemplateDir holds built-in pipelines reached only through
	// editionTemplateFS. Assets must live outside sharedTemplateDir: the
	// shared embed glob reaches every JSON under it, so a build that cannot
	// register their components would still discover them.
	editionTemplateDir = "template_ee"
)

//go:embed template_ee/*.json
var editionAssetsFS embed.FS

// editionTemplatesFS exposes sharedTemplateDir as the union of the
// always-embedded templates and the ones under editionTemplateDir. Keeping
// the merge at the fs.FS layer lets the registry own parsing, duplicate
// detection and ordering through one NewRegistryFromFS.
type editionTemplatesFS struct{}

var _ fs.ReadDirFS = editionTemplatesFS{}

func (editionTemplatesFS) Open(name string) (fs.File, error) {
	file, err := builtinTemplateFS.Open(name)
	if err == nil {
		return file, nil
	}
	if editionName, ok := editionTemplatePath(name); ok {
		if editionFile, editionErr := editionAssetsFS.Open(editionName); editionErr == nil {
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
	editionEntries, err := fs.ReadDir(editionAssetsFS, editionTemplateDir)
	if err != nil {
		return nil, err
	}
	entries = append(entries, editionEntries...)
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	return entries, nil
}

// editionTemplatePath maps a shared-directory path to its template_ee/ asset.
func editionTemplatePath(name string) (string, bool) {
	rest, ok := strings.CutPrefix(name, sharedTemplateDir+"/")
	if !ok {
		return "", false
	}
	return path.Join(editionTemplateDir, rest), true
}

// editionTemplateFS unions the always-embedded templates with the
// template_ee/ assets, so the registry discovers both through its single
// NewRegistryFromFS path.
func editionTemplateFS() fs.ReadDirFS { return editionTemplatesFS{} }
