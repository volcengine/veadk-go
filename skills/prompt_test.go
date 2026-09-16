package skills

import (
	"strings"
	"testing"
)

func TestFormatSkillsAsXMLOmitsTriggers(t *testing.T) {
	xml := FormatSkillsAsXML([]*Skill{
		{
			Frontmatter: &Frontmatter{
				Name:        "extended-skill",
				Description: "Handle an extended skill fixture.",
				Triggers:    []string{"示例触发词", "example trigger", "<escaped trigger>"},
			},
		},
	})

	for _, want := range []string{
		"<name>\nextended-skill\n</name>",
		"<description>\nHandle an extended skill fixture.\n</description>",
	} {
		if !strings.Contains(xml, want) {
			t.Fatalf("FormatSkillsAsXML() = %q, want to contain %q", xml, want)
		}
	}
	for _, unwanted := range []string{
		"<triggers>",
		"示例触发词",
		"example trigger",
		"escaped trigger",
	} {
		if strings.Contains(xml, unwanted) {
			t.Fatalf("FormatSkillsAsXML() = %q, want to omit %q", xml, unwanted)
		}
	}
}
