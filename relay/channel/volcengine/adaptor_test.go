package volcengine

import "testing"

func TestIsOfficialVolcHost(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"https://ark.cn-beijing.volces.com", true},
		{"https://ark.ap-southeast.bytepluses.com", true},
		{"https://ark.cn-beijing.volces.com/api/coding", true},
		{"https://ark.cn-shanghai.volces.com", true},
		{"https://api.cloudwise.ai", false},
		{"https://my-proxy.example.com", false},
		{"", false},
		{"not-a-url", false},
		{"https://evil-volces.com.attacker.net", false},
	}
	for _, tc := range cases {
		if got := isOfficialVolcHost(tc.in); got != tc.want {
			t.Errorf("isOfficialVolcHost(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
