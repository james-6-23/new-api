package minimaximage

var ModelList = []string{
	"canvas-20",
	"MM-H3-sft-Mlogic-High-25-image",
	"MM-H3-sft-Mlogic-Max-25-image",
	"gpt-image-2",
	"gpt-image-2.5-flare",
	"gpt-image-2.5-sunburst",
}

// modelAliasMap maps client-facing GPT-style names to MiniMax upstream model IDs.
// Billing always uses the original client name; only the upstream request uses the mapped name.
var modelAliasMap = map[string]string{
	"gpt-image-2":            "canvas-20",
	"gpt-image-2.5-flare":    "MM-H3-sft-Mlogic-High-25-image",
	"gpt-image-2.5-sunburst": "MM-H3-sft-Mlogic-Max-25-image",
}

// resolveModel returns the MiniMax upstream model name for a given client model name.
// If name is not a known alias, it is returned unchanged.
func resolveModel(name string) string {
	if upstream, ok := modelAliasMap[name]; ok {
		return upstream
	}
	return name
}

const ChannelName = "MiniMax Image"
