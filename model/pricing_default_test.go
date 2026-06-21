package model

import (
	"strings"
	"testing"
)

// matchVendorByRules 复刻 initDefaultVendorMapping 中的模型名→供应商匹配逻辑
// （strings.ToLower + strings.Contains），不触达数据库，便于对规则做纯单元断言。
func matchVendorByRules(modelName string) string {
	lower := strings.ToLower(modelName)
	for pattern, vendorName := range defaultVendorRules {
		if strings.Contains(lower, pattern) {
			return vendorName
		}
	}
	return ""
}

func TestSeedanceModelsMapToByteDanceVendor(t *testing.T) {
	cases := []string{
		"dreamina-seedance-2-0-260128",
		"dreamina-seedance-2-0-fast-260128",
	}
	for _, name := range cases {
		vendor := matchVendorByRules(name)
		if vendor != "字节跳动" {
			t.Errorf("model %q vendor = %q, want 字节跳动", name, vendor)
		}
		if icon := getDefaultVendorIcon(vendor); icon != "Doubao.Color" {
			t.Errorf("vendor %q icon = %q, want Doubao.Color", vendor, icon)
		}
	}
}
