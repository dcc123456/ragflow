//go:build ee

package service

import "testing"

func TestEnterpriseBuiltinTimeline(t *testing.T) {
	t.Chdir("../..")
	templates, err := loadBuiltinTemplates()
	if err != nil {
		t.Fatal(err)
	}
	for _, template := range templates {
		if template.Kind != "timeline" {
			continue
		}
		if template.Config["kind"] != "timeline" {
			t.Fatalf("config kind = %v", template.Config["kind"])
		}
		if err := ValidateTemplatePayload(map[string]interface{}{"name": template.DisplayName, "kind": template.Kind, "config": template.Config}, true); err != nil {
			t.Fatal(err)
		}
		return
	}
	t.Fatal("enterprise builtin timeline missing")
}
