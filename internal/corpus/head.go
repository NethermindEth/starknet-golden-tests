package corpus

import "github.com/NethermindEth/starknet-golden-tests/internal/canon"

// headMethods give a different result each time the chain head moves.
var headMethods = map[string]bool{
	"starknet_blockNumber":        true,
	"starknet_blockHashAndNumber": true,
	"starknet_syncing":            true,
	// Juno serves storage proofs only at the head.
	"starknet_getStorageProof": true,
}

// headTags are block tags that point to a block that changes over time.
var headTags = map[string]bool{
	"latest":        true,
	"pre_confirmed": true,
	"l1_accepted":   true,
}

// HeadDependent reports whether the response can change when the chain head
// moves: the head methods, any head block tag in the params, and getEvents
// without to_block. Such requests are not recorded or compared.
func (r Request) HeadDependent() bool {
	if headMethods[r.Method] {
		return true
	}
	if len(r.Params) == 0 {
		return false
	}
	params, err := canon.Decode(r.Params)
	if err != nil {
		return false
	}
	if r.Method == "starknet_getEvents" && !hasToBlock(params) {
		return true
	}
	return hasHeadTag(params)
}

func hasToBlock(params any) bool {
	var filter any
	switch p := params.(type) {
	case map[string]any:
		filter = p["filter"]
	case []any:
		if len(p) > 0 {
			filter = p[0]
		}
	}
	f, ok := filter.(map[string]any)
	return ok && f["to_block"] != nil
}

func hasHeadTag(v any) bool {
	switch v := v.(type) {
	case string:
		return headTags[v]
	case map[string]any:
		for _, child := range v {
			if hasHeadTag(child) {
				return true
			}
		}
	case []any:
		for _, child := range v {
			if hasHeadTag(child) {
				return true
			}
		}
	}
	return false
}
