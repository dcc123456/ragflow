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

package utility

import (
	"strings"
	"testing"
)

func TestWPSFileTypes(t *testing.T) {
	for _, tc := range []struct {
		ext  string
		want FileType
	}{
		{"wps", FileTypeDOC}, {"wpt", FileTypeDOC},
		{"et", FileTypeXLS}, {"ett", FileTypeXLS},
		{"dps", FileTypePPT}, {"dpt", FileTypePPT},
	} {
		for _, name := range []string{"sample." + tc.ext, "dir/示例." + strings.ToUpper(tc.ext)} {
			t.Run(name, func(t *testing.T) {
				if got := GetFileType(name); got != tc.want {
					t.Errorf("GetFileType(%q) = %q, want %q", name, got, tc.want)
				}
				if got := FilenameType(name); got != FileTypeDOC {
					t.Errorf("FilenameType(%q) = %q, want document upload bucket", name, got)
				}
			})
		}
	}
}

func TestWPSFileTypesRejectUnrelatedAndInvalidNames(t *testing.T) {
	for _, name := range []string{"notes.wpsx", "book.etx", "deck.dpts", "document.wps.bak", "deck.dpt.tmp", "noext", ""} {
		if got := GetFileType(name); got != FileTypeOTHER {
			t.Errorf("GetFileType(%q) = %q, want other", name, got)
		}
		if got := FilenameType(name); got != FileTypeOTHER {
			t.Errorf("FilenameType(%q) = %q, want other", name, got)
		}
	}
	if got := FilenameType(strings.Repeat("字", 256) + ".wps"); got != FileTypeOTHER {
		t.Errorf("overlong WPS filename accepted: %q", got)
	}
}

func TestVisioFileTypes(t *testing.T) {
	for _, name := range []string{"arch.vsdx", "dir/拓扑图.VSDX", "arch.vsd", "dir/拓扑图.VSD", "arch.vsdm", "dir/拓扑图.VSDM"} {
		t.Run(name, func(t *testing.T) {
			if got := GetFileType(name); got != FileTypeVisio {
				t.Errorf("GetFileType(%q) = %q, want %q", name, got, FileTypeVisio)
			}
			if got := FilenameType(name); got != FileTypeVisio {
				t.Errorf("FilenameType(%q) = %q, want %q", name, got, FileTypeVisio)
			}
		})
	}
}
