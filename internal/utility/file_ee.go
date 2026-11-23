//go:build ee

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

package utility

// FileTypeCAD is the enterprise-only CAD drawing type (DWG/DXF/DXFB). It is
// registered into the community extension map by the parser package's init so
// GetFileType/FilenameType route CAD uploads to the CAD parser without the
// community switch changing.
const FileTypeCAD FileType = "cad"

// FileTypeVSDX is the enterprise-only Microsoft Visio OpenXML drawing type (.vsdx).
const FileTypeVSDX FileType = "visio"

func init() {
	RegisterExtensionFileType("wps", FileTypeDOC)
	RegisterExtensionFileType("wpt", FileTypeDOC)
	RegisterExtensionFileType("et", FileTypeXLS)
	RegisterExtensionFileType("ett", FileTypeXLS)
	RegisterExtensionFileType("dps", FileTypePPT)
	RegisterExtensionFileType("dpt", FileTypePPT)
	RegisterExtensionFileType("vsdx", FileTypeVSDX)
}
