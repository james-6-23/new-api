package dto

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
)

func TestSeedance3rdAssetEnabledRoundTrip(t *testing.T) {
	s := ChannelOtherSettings{Seedance3rdAssetEnabled: true}
	b, err := common.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back ChannelOtherSettings
	if err := common.Unmarshal(b, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !back.Seedance3rdAssetEnabled {
		t.Fatalf("Seedance3rdAssetEnabled lost in round-trip: %s", string(b))
	}
}

func TestChannelSettingsCostRatioRoundTrip(t *testing.T) {
	s := ChannelSettings{CostRatio: 2.5}
	data, err := common.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var out ChannelSettings
	if err := common.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out.CostRatio != 2.5 {
		t.Fatalf("cost_ratio = %v, want 2.5", out.CostRatio)
	}
	// zero value must be omitted (0 = "not set")
	data, _ = common.Marshal(ChannelSettings{})
	if strings.Contains(string(data), "cost_ratio") {
		t.Fatalf("zero cost_ratio must be omitted, got %s", data)
	}
}

func TestChannelSettingsSupplierFieldsRoundTrip(t *testing.T) {
	s := ChannelSettings{
		CostMode:     "discount",
		CostDiscount: 0.8,
		IsAggregator: true,
		SubSuppliers: []ChannelSubSupplier{
			{Name: "openai-src", CostRatio: 2.1},
		},
	}
	data, err := common.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var out ChannelSettings
	if err := common.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out.CostMode != "discount" {
		t.Fatalf("cost_mode = %v, want discount", out.CostMode)
	}
	if out.CostDiscount != 0.8 {
		t.Fatalf("cost_discount = %v, want 0.8", out.CostDiscount)
	}
	if !out.IsAggregator {
		t.Fatalf("is_aggregator = %v, want true", out.IsAggregator)
	}
	if len(out.SubSuppliers) != 1 {
		t.Fatalf("sub_suppliers length = %v, want 1", len(out.SubSuppliers))
	}
	if out.SubSuppliers[0].Name != "openai-src" {
		t.Fatalf("sub_suppliers[0].Name = %v, want openai-src", out.SubSuppliers[0].Name)
	}
	if out.SubSuppliers[0].CostRatio != 2.1 {
		t.Fatalf("sub_suppliers[0].CostRatio = %v, want 2.1", out.SubSuppliers[0].CostRatio)
	}

	// zero ChannelSettings must omit supplier fields
	data, _ = common.Marshal(ChannelSettings{})
	str := string(data)
	if strings.Contains(str, "cost_mode") {
		t.Fatalf("zero cost_mode must be omitted, got %s", data)
	}
	if strings.Contains(str, "cost_discount") {
		t.Fatalf("zero cost_discount must be omitted, got %s", data)
	}
	if strings.Contains(str, "is_aggregator") {
		t.Fatalf("zero is_aggregator must be omitted, got %s", data)
	}
	if strings.Contains(str, "sub_suppliers") {
		t.Fatalf("zero sub_suppliers must be omitted, got %s", data)
	}
}

func TestResolveAssetProvider(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty defaults to byteplus", "", AssetProviderBytePlus},
		{"explicit byteplus", "byteplus", AssetProviderBytePlus},
		{"explicit cloudwise", "cloudwise", AssetProviderCloudwise},
		{"unknown falls back to byteplus", "wat", AssetProviderBytePlus},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := ChannelOtherSettings{AssetProvider: tc.in}
			if got := s.ResolveAssetProvider(); got != tc.want {
				t.Errorf("ResolveAssetProvider() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestResolveAssetGroupId pins the provider scoping of the stored group id: an id
// minted by one asset library is meaningless to the other, so a marker that names a
// different provider makes the stored id read as absent (the caller then bootstraps a
// fresh group instead of destroying the operator's id on the first rotation).
func TestResolveAssetGroupId(t *testing.T) {
	cases := []struct {
		name          string
		provider      string
		groupProvider string
		groupId       string
		want          string
	}{
		// Existing rows predate the marker; they must keep working untouched.
		{"absent marker keeps stored id (legacy byteplus row)", "", "", "bp-1", "bp-1"},
		{"absent marker keeps stored id on cloudwise too", AssetProviderCloudwise, "", "cw-1", "cw-1"},
		{"matching marker keeps stored id", AssetProviderCloudwise, AssetProviderCloudwise, "cw-1", "cw-1"},
		{"byteplus marker with empty provider still matches (empty means byteplus)", "", AssetProviderBytePlus, "bp-1", "bp-1"},
		{"byteplus id under cloudwise reads as absent", AssetProviderCloudwise, AssetProviderBytePlus, "bp-1", ""},
		{"cloudwise id under byteplus reads as absent", AssetProviderBytePlus, AssetProviderCloudwise, "cw-1", ""},
		{"no group id at all", AssetProviderCloudwise, AssetProviderCloudwise, "", ""},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			s := ChannelOtherSettings{
				AssetProvider:        tc.provider,
				AssetGroupProvider:   tc.groupProvider,
				BytePlusAssetGroupId: tc.groupId,
			}
			if got := s.ResolveAssetGroupId(); got != tc.want {
				t.Errorf("ResolveAssetGroupId() = %q, want %q", got, tc.want)
			}
		})
	}

	t.Run("nil receiver is empty", func(t *testing.T) {
		var s *ChannelOtherSettings
		if got := s.ResolveAssetGroupId(); got != "" {
			t.Errorf("ResolveAssetGroupId() = %q, want empty", got)
		}
	})

	t.Run("marker is omitted when unset", func(t *testing.T) {
		data, err := common.Marshal(ChannelOtherSettings{})
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		if strings.Contains(string(data), "asset_group_provider") {
			t.Fatalf("empty asset_group_provider must be omitted, got %s", data)
		}
	})
}
