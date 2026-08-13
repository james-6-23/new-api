package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
)

// TestCostVersionChanged 钉住渠道保存时的追版本闸门：只有计价字段真的变了才算
// "改价"。少了这道比对，改 key、改模型列表这类与价格无关的保存都会插一条内容
// 重复的版本，价格历史读不出"这个价从哪天开始"。
// empty-mode-equals-ratio 一例单独存在：空 CostMode 等同 "ratio"，不归一化的话
// 一次纯写法变更就会被记成改价。
func TestCostVersionChanged(t *testing.T) {
	cases := []struct {
		name   string
		latest model.ChannelCostVersion
		s      dto.ChannelSettings
		want   bool
	}{
		{"identical-ratio", model.ChannelCostVersion{CostMode: "ratio", CostRatio: 2.5},
			dto.ChannelSettings{CostMode: "ratio", CostRatio: 2.5}, false},
		{"empty-mode-equals-ratio", model.ChannelCostVersion{CostMode: "", CostRatio: 2.5},
			dto.ChannelSettings{CostMode: "ratio", CostRatio: 2.5}, false},
		{"ratio-changed", model.ChannelCostVersion{CostMode: "ratio", CostRatio: 2.5},
			dto.ChannelSettings{CostMode: "ratio", CostRatio: 2.3}, true},
		{"mode-switched", model.ChannelCostVersion{CostMode: "ratio", CostRatio: 2.5},
			dto.ChannelSettings{CostMode: "discount", CostDiscount: 0.8}, true},
		{"discount-changed", model.ChannelCostVersion{CostMode: "discount", CostDiscount: 0.8},
			dto.ChannelSettings{CostMode: "discount", CostDiscount: 0.75}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := costVersionChanged(tc.latest, &tc.s); got != tc.want {
				t.Fatalf("changed = %v, want %v", got, tc.want)
			}
		})
	}
}
