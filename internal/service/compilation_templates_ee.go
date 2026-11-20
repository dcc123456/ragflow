//go:build ee

package service

import (
	"embed"
	"io/fs"
)

//go:embed templates_ee/*.yaml
var enterpriseCompilationTemplates embed.FS

func additionalBuiltinTemplates() ([]*BuiltinTemplate, error) {
	files, err := fs.Sub(enterpriseCompilationTemplates, "templates_ee")
	if err != nil {
		return nil, err
	}
	return loadBuiltinTemplatesFromFS(files)
}
