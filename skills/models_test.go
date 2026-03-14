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
				Description: strings.Repeat("a", 1025),
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
				Metadata: map[string]string{
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
