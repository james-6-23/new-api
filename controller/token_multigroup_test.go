package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
)

func TestValidateTokenGroups(t *testing.T) {
	usable := map[string]string{"default": "默认", "claude": "c", "gpt": "g", "auto": "自动"}

	cases := []struct {
		name    string
		groups  []string
		wantErr bool
	}{
		{"empty ok", []string{}, false},
		{"single ok", []string{"claude"}, false},
		{"multi ok", []string{"claude", "gpt"}, false},
		{"auto alone ok", []string{"auto"}, false},
		{"unknown group rejected", []string{"claude", "unknown"}, true},
		{"auto with others rejected", []string{"auto", "claude"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateTokenGroupsWithUsable(usable, tc.groups)
			if (err != nil) != tc.wantErr {
				t.Fatalf("validateTokenGroups(%v) err=%v, wantErr=%v", tc.groups, err, tc.wantErr)
			}
		})
	}
	_ = model.Token{}
}
