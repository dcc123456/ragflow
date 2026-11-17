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
	"encoding/json"
	"io/fs"
	"path"
	"reflect"
	"strings"
	"testing"
)

// TestDefaultRegistryIncludesEditionTemplates pins the init contract in
// builtin_template_ee.go: DefaultRegistry must be built over the merged
// template FS, not over builtinTemplateFS alone.
func TestDefaultRegistryIncludesEditionTemplates(t *testing.T) {
	registry, err := DefaultRegistry()
	if err != nil {
		t.Fatalf("DefaultRegistry: %v", err)
	}
	refs := editionTemplateRefs(t)
	if len(refs) == 0 {
		t.Fatal("no edition templates embedded; template_ee/ must ship at least one JSON")
	}
	for _, ref := range refs {
		if !registry.IsValid(ref) {
			t.Fatalf("DefaultRegistry() dropped edition template %q; refs=%v", ref, registry.Refs())
		}
	}
}

// TestEditionTemplatesStayOutOfSharedEmbed guards the reason template_ee/
// exists: an enterprise template under template/ is embedded by the community
// build too, where its components are not registered.
func TestEditionTemplatesStayOutOfSharedEmbed(t *testing.T) {
	entries, err := fs.ReadDir(editionTemplateFS, editionTemplateDir)
	if err != nil {
		t.Fatalf("read %s: %v", editionTemplateDir, err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if _, err := fs.Stat(builtinTemplateFS, path.Join(sharedTemplateDir, entry.Name())); err == nil {
			t.Fatalf("edition template %q is also embedded from %s/; move it out of the shared template directory", entry.Name(), sharedTemplateDir)
		}
	}

	sharedEntries, err := fs.ReadDir(builtinTemplateFS, sharedTemplateDir)
	if err != nil {
		t.Fatalf("read %s: %v", sharedTemplateDir, err)
	}
	for _, entry := range sharedEntries {
		if entry.IsDir() {
			continue
		}
		if strings.HasSuffix(strings.TrimSuffix(entry.Name(), ".json"), "_ee") {
			t.Fatalf("edition template %q lives in the shared %s/ directory; the community build embeds it and cannot register its components", entry.Name(), sharedTemplateDir)
		}
	}
}

// TestEditionTemplatesStructuralInvariants mirrors the shared
// directory-driven template checks that only walk template/: every edition
// template must parse into canonical components, and every Extractor node must
// keep its canvas form equal to its DSL params.
func TestEditionTemplatesStructuralInvariants(t *testing.T) {
	entries, err := fs.ReadDir(editionTemplateFS, editionTemplateDir)
	if err != nil {
		t.Fatalf("read %s: %v", editionTemplateDir, err)
	}
	tested := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, builtinTemplatePrefix) || !strings.HasSuffix(name, ".json") {
			continue
		}
		tested++
		t.Run(name, func(t *testing.T) {
			raw, err := fs.ReadFile(editionTemplateFS, path.Join(editionTemplateDir, name))
			if err != nil {
				t.Fatalf("read template: %v", err)
			}
			var wrapper struct {
				DSL json.RawMessage `json:"dsl"`
			}
			if err := json.Unmarshal(raw, &wrapper); err != nil {
				t.Fatalf("unmarshal wrapper: %v", err)
			}
			if len(wrapper.DSL) == 0 {
				t.Fatal("missing dsl key")
			}
			schemas, err := ExtractAllComponentParams(wrapper.DSL)
			if err != nil {
				t.Fatalf("ExtractAllComponentParams: %v", err)
			}
			if len(schemas) == 0 {
				t.Fatal("expected at least 1 component, got 0")
			}
			for _, schema := range schemas {
				if schema.CpnID == "" || schema.ComponentName == "" {
					t.Fatalf("component %+v has an empty CpnID or ComponentName", schema)
				}
				if schema.ParamsDefaults == nil {
					t.Fatalf("component %q has nil ParamsDefaults", schema.CpnID)
				}
				if _, ok := schema.ParamsDefaults["outputs"]; ok {
					t.Fatalf("component %q ParamsDefaults contains excluded key %q", schema.CpnID, "outputs")
				}
			}
			assertEditionExtractorFormsMatchParams(t, wrapper.DSL)
		})
	}
	if tested == 0 {
		t.Fatal("no edition templates found")
	}
}

// assertEditionExtractorFormsMatchParams is the enterprise half of
// TestAgentTemplateExtractorsUseModularParams: the shared test globs
// template/ only, so an edition template's canvas form would otherwise drift
// from its DSL params unnoticed.
func assertEditionExtractorFormsMatchParams(t *testing.T, dslJSON []byte) {
	t.Helper()
	var dsl map[string]any
	if err := json.Unmarshal(dslJSON, &dsl); err != nil {
		t.Fatalf("unmarshal DSL: %v", err)
	}
	components, _ := dsl["components"].(map[string]any)
	graph, _ := dsl["graph"].(map[string]any)
	nodes, _ := graph["nodes"].([]any)

	forms := make(map[string]map[string]any, len(nodes))
	for _, raw := range nodes {
		node, _ := raw.(map[string]any)
		id, _ := node["id"].(string)
		form, _ := node["data"].(map[string]any)["form"].(map[string]any)
		if id != "" && form != nil {
			forms[id] = form
		}
	}

	checked := 0
	for cpnID, raw := range components {
		component, _ := raw.(map[string]any)
		obj, _ := component["obj"].(map[string]any)
		if obj["component_name"] != "Extractor" {
			continue
		}
		params, _ := obj["params"].(map[string]any)
		form, ok := forms[cpnID]
		if !ok {
			t.Fatalf("Extractor %s has no canvas data.form node to compare against params", cpnID)
		}
		if !reflect.DeepEqual(form, params) {
			t.Fatalf("Extractor %s params and canvas data.form diverge; both must be updated together", cpnID)
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("edition template has no Extractor component to check")
	}
}

// editionTemplateRefs returns the parser_id for every embedded edition
// template, using the same filename normalization as the registry.
func editionTemplateRefs(t *testing.T) []string {
	t.Helper()
	entries, err := fs.ReadDir(editionTemplateFS, editionTemplateDir)
	if err != nil {
		t.Fatalf("read %s: %v", editionTemplateDir, err)
	}
	refs := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, builtinTemplatePrefix) || !strings.HasSuffix(name, ".json") {
			continue
		}
		ref := builtinRefFromFilename(name)
		if ref == "" {
			t.Fatalf("edition template %q has no parser_id", name)
		}
		refs = append(refs, ref)
	}
	return refs
}
