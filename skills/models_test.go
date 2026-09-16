package skills

import (
	"strings"
	"testing"
)

func TestSkill_Valid(t *testing.T) {
	tests := []struct {
		name    string
		skill   Frontmatter
		wantErr bool
	}{
		{
			name: "valid skill",
			skill: Frontmatter{
				Name:        "pdf-processing",
				Description: "Extracts text and tables from PDF files.",
			},
			wantErr: false,
		},
		{
			name: "valid skill with compatibility",
			skill: Frontmatter{
				Name:          "data-analysis",
				Description:   "Analyzes data.",
				Compatibility: "Requires python 3.9",
			},
			wantErr: false,
		},
		{
			name: "valid multibyte description uses character count",
			skill: Frontmatter{
				Name:        "chinese-description",
				Description: strings.Repeat("示例", 600),
			},
			wantErr: false,
		},
		{
			name: "invalid name - empty",
			skill: Frontmatter{
				Name:        "",
				Description: "Valid description",
			},
			wantErr: true,
		},
		{
			name: "invalid name - too long",
			skill: Frontmatter{
				Name:        strings.Repeat("a", 65),
				Description: "Valid description",
			},
			wantErr: true,
		},
		{
			name: "invalid name - uppercase",
			skill: Frontmatter{
				Name:        "PDF-Processing",
				Description: "Valid description",
			},
			wantErr: true,
		},
		{
			name: "invalid name - starts with hyphen",
			skill: Frontmatter{
				Name:        "-pdf",
				Description: "Valid description",
			},
			wantErr: true,
		},
		{
			name: "invalid name - ends with hyphen",
			skill: Frontmatter{
				Name:        "pdf-",
				Description: "Valid description",
			},
			wantErr: true,
		},
		{
			name: "invalid name - consecutive hyphens",
			skill: Frontmatter{
				Name:        "pdf--processing",
				Description: "Valid description",
			},
			wantErr: true,
		},
		{
			name: "invalid description - empty",
			skill: Frontmatter{
				Name:        "valid-name",
				Description: "",
			},
			wantErr: true,
		},
		{
			name: "invalid description - too long",
			skill: Frontmatter{
				Name:        "valid-name",
				Description: strings.Repeat("a", 4097),
			},
			wantErr: true,
		},
		{
			name: "invalid compatibility - too long",
			skill: Frontmatter{
				Name:          "valid-name",
				Description:   "Valid description",
				Compatibility: strings.Repeat("a", 501),
			},
			wantErr: true,
		},
		{
			name: "valid skill with license",
			skill: Frontmatter{
				Name:        "valid-name",
				Description: "Valid description",
				License:     "MIT",
			},
			wantErr: false,
		},
		{
			name: "valid skill with metadata",
			skill: Frontmatter{
				Name:        "valid-name",
				Description: "Valid description",
				Metadata: map[string]any{
					"author":  "example-org",
					"version": "1.0",
				},
			},
			wantErr: false,
		},
		{
			name: "valid skill with allowed tools",
			skill: Frontmatter{
				Name:         "valid-name",
				Description:  "Valid description",
				AllowedTools: "Bash(git:*) Bash(jq:*) Read",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.skill.Validate(); (err != nil) != tt.wantErr {
				t.Errorf("Skill.Valid() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestFrontmatterSkillPromptEntryOmitsTriggers(t *testing.T) {
	frontmatter := Frontmatter{
		Name:        "extended-skill",
		Description: "Handle an extended skill fixture.",
		Triggers:    []string{"示例触发词", "example trigger"},
	}

	got := frontmatter.SkillPromptEntry()
	want := "- name: extended-skill, description: Handle an extended skill fixture."
	if got != want {
		t.Fatalf("SkillPromptEntry() = %q, want %q", got, want)
	}
}
