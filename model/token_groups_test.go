package model

import (
	"reflect"
	"testing"
)

func TestTokenGetGroups(t *testing.T) {
	cases := []struct {
		name  string
		group string
		want  []string
	}{
		{"empty", "", []string{}},
		{"single", "vip", []string{"vip"}},
		{"auto", "auto", []string{"auto"}},
		{"multi", "claude,gpt", []string{"claude", "gpt"}},
		{"spaces", " claude , gpt ", []string{"claude", "gpt"}},
		{"empty-items", "claude,,gpt,", []string{"claude", "gpt"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			token := &Token{Group: tc.group}
			got := token.GetGroups()
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("GetGroups(%q) = %#v, want %#v", tc.group, got, tc.want)
			}
		})
	}
}
