package main

import (
	"strings"
	"testing"
)

func TestAddCoreKeys(t *testing.T) {
	t.Parallel()
	both := []identityKey{{"user_name", "Ada"}, {"project_name", "widget"}}
	tests := []struct {
		name      string
		content   string
		want      string
		wantAdded string
	}{
		{"empty file", "", "[core]\nuser_name = \"Ada\"\nproject_name = \"widget\"\n", "user_name,project_name"},
		{"core without keys", "[core]\nlanguage = \"en\"\n", "[core]\nuser_name = \"Ada\"\nproject_name = \"widget\"\nlanguage = \"en\"\n", "user_name,project_name"},
		{"other table only", "[agents.x]\nk = 1\n", "[agents.x]\nk = 1\n\n[core]\nuser_name = \"Ada\"\nproject_name = \"widget\"\n", "user_name,project_name"},
		{"key in another table is not core's", "[agents.x]\nuser_name = \"Other\"\n", "[agents.x]\nuser_name = \"Other\"\n\n[core]\nuser_name = \"Ada\"\nproject_name = \"widget\"\n", "user_name,project_name"},
		{"dotted key defined", "core.user_name = \"Mine\"\n", "core.user_name = \"Mine\"\n\n[core]\nproject_name = \"widget\"\n", "project_name"},
		{"one key defined", "[core]\nproject_name = \"mine\"\n", "[core]\nuser_name = \"Ada\"\nproject_name = \"mine\"\n", "user_name"},
		{"both defined", "[core]\nuser_name = \"a\"\nproject_name = \"b\"\n", "[core]\nuser_name = \"a\"\nproject_name = \"b\"\n", ""},
		{"header with comment", "[core] # mine\n", "[core] # mine\nuser_name = \"Ada\"\nproject_name = \"widget\"\n", "user_name,project_name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, added := addCoreKeys(tt.content, both)
			if got != tt.want {
				t.Errorf("content:\n%s\nwant:\n%s", got, tt.want)
			}
			if strings.Join(added, ",") != tt.wantAdded {
				t.Errorf("added = %v, want %q", added, tt.wantAdded)
			}
		})
	}
}

func TestTomlString_EscapesQuotesAndBackslashes(t *testing.T) {
	t.Parallel()
	if got, want := tomlString(`A "B" \ <c>`), `"A \"B\" \\ <c>"`; got != want {
		t.Errorf("tomlString = %s, want %s", got, want)
	}
}
