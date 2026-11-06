package diff

// sortRule sorts the arrays at paths that end with suffix ("*" matches any
// array index) by the string values of keys. With no keys, it sorts by the
// canonical JSON of each item.
type sortRule struct {
	suffix []string
	keys   []string
}

// sortRules lists the arrays that the spec defines as sets or mappings. Their
// order is not part of the result: Juno builds state diffs from Go maps, so
// the order changes on each call. All other arrays are compared in order.
var sortRules = []sortRule{
	// STATE_DIFF: "a mapping of addresses to the new values".
	{suffix: []string{"state_diff", "storage_diffs"}, keys: []string{"address"}},
	{suffix: []string{"state_diff", "storage_diffs", "*", "storage_entries"}, keys: []string{"key"}},
	{suffix: []string{"state_diff", "nonces"}, keys: []string{"contract_address"}},
	{suffix: []string{"state_diff", "deployed_contracts"}, keys: []string{"address"}},
	{suffix: []string{"state_diff", "declared_classes"}, keys: []string{"class_hash"}},
	{suffix: []string{"state_diff", "deprecated_declared_classes"}},
	{suffix: []string{"state_diff", "replaced_classes"}, keys: []string{"contract_address"}},
	{suffix: []string{"state_diff", "migrated_compiled_classes"}, keys: []string{"class_hash"}},
	// INITIAL_READS: "the set of state values".
	{suffix: []string{"initial_reads", "storage"}, keys: []string{"contract_address", "key"}},
	{suffix: []string{"initial_reads", "nonces"}, keys: []string{"contract_address"}},
	{suffix: []string{"initial_reads", "class_hashes"}, keys: []string{"contract_address"}},
	{suffix: []string{"initial_reads", "declared_contracts"}, keys: []string{"class_hash"}},
	// NODE_HASH_TO_NODE_MAPPING: "a node_hash -> node mapping". Not used yet:
	// getStorageProof is head-dependent, so it is not recorded or compared.
	{suffix: []string{"classes_proof"}, keys: []string{"node_hash"}},
	{suffix: []string{"contracts_proof", "nodes"}, keys: []string{"node_hash"}},
	{suffix: []string{"contracts_storage_proofs", "*"}, keys: []string{"node_hash"}},
}

func (r sortRule) matches(path []string) bool {
	if len(path) < len(r.suffix) {
		return false
	}
	tail := path[len(path)-len(r.suffix):]
	for i, segment := range r.suffix {
		if segment != tail[i] {
			return false
		}
	}
	return true
}

func findSortRule(path []string) (sortRule, bool) {
	for _, r := range sortRules {
		if r.matches(path) {
			return r, true
		}
	}
	return sortRule{}, false
}
