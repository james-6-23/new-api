package common

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/require"
)

func TestTypeSafeRegistration(t *testing.T) {
	require.Len(t, constant.ChannelBaseURLs, constant.ChannelTypeTypeSafe+1)
	require.Equal(t, "https://api.typesafe.ai", constant.ChannelBaseURLs[constant.ChannelTypeTypeSafe])
	require.Equal(t, "TypeSafe", constant.GetChannelTypeName(constant.ChannelTypeTypeSafe))
	apiType, ok := ChannelType2APIType(constant.ChannelTypeTypeSafe)
	require.True(t, ok)
	require.Equal(t, constant.APITypeTypeSafe, apiType)
	require.Equal(t, []constant.EndpointType{constant.EndpointTypeTypeSafeSystemOne}, GetEndpointTypesByChannelType(constant.ChannelTypeTypeSafe, "jev-latest"))
	endpoint, ok := GetDefaultEndpointInfo(constant.EndpointTypeTypeSafeSystemOne)
	require.True(t, ok)
	require.Equal(t, EndpointInfo{Path: "/v1/systemone", Method: "POST"}, endpoint)
}
