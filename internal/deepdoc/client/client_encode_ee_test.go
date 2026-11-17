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
//  For commercial licensing inquiries: sales@infiniflow.ai
//

//go:build ee

package client

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/jpeg"
	"testing"
)

// TestEncodeImageB64UsesJPEG locks in the wire format shared by every EE
// deepdoc inference client. All Python deepdoc clients submit
// img.save(format="jpeg"), and the 2-class TSR model is calibrated on that
// lossy encoding: feeding the identical crop as PNG dropped the column
// separator score from ~0.95 to ~0.61 and merged header/data cells
// (学位授予门类 工学, 专业代码 专业名称). A regression back to PNG must fail here.
func TestEncodeImageB64UsesJPEG(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for i := range img.Pix {
		img.Pix[i] = uint8(i * 7)
	}

	raw, err := base64.StdEncoding.DecodeString(encodeImageB64(img))
	if err != nil {
		t.Fatalf("payload is not valid base64: %v", err)
	}
	if len(raw) < 3 || raw[0] != 0xff || raw[1] != 0xd8 || raw[2] != 0xff {
		t.Fatalf("payload is not JPEG (want SOI marker ff d8 ff), got % x", raw[:min(8, len(raw))])
	}

	// And it must be a JPEG that decodes back to the original dimensions, so a
	// header-only fake cannot satisfy the test.
	decoded, err := jpeg.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("payload does not decode as JPEG: %v", err)
	}
	if got := decoded.Bounds().Dx(); got != 32 {
		t.Fatalf("decoded width = %d, want 32", got)
	}
	if got := decoded.Bounds().Dy(); got != 32 {
		t.Fatalf("decoded height = %d, want 32", got)
	}
}
