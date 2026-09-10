package skills

import (
	"strings"
	"testing"
)

func TestFormatSkillsAsXMLIncludesTriggers(t *testing.T) {
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
		"<triggers>",
		"- 示例触发词",
		"- example trigger",
		"- &lt;escaped trigger&gt;",
	} {
		if !strings.Contains(xml, want) {
			t.Fatalf("FormatSkillsAsXML() = %q, want to contain %q", xml, want)
		}
	}
}
