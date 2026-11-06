// Package diff compares JSON-RPC responses with order-aware rules.
package diff

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/NethermindEth/starknet-golden-tests/internal/canon"
	"github.com/NethermindEth/starknet-golden-tests/internal/corpus"
	"github.com/NethermindEth/starknet-golden-tests/internal/rpc"
)

// MaxDifferences is the most differences that Responses returns.
const MaxDifferences = 20

const maxValueLen = 200

// Difference is one place where two responses differ.
type Difference struct {
	// Path is a JSON pointer into the response, such as /result/transactions/3.
	Path string `json:"path"`
	Want string `json:"want"`
	Got  string `json:"got"`
}

func (d Difference) String() string {
	return fmt.Sprintf("%s: want %s, got %s", d.Path, d.Want, d.Got)
}

// Options changes how responses are compared.
type Options struct {
	// StrictErrorData also compares the data member of errors.
	StrictErrorData bool
}

// Normalize sorts the arrays that the spec defines as sets (see sortRules)
// and returns the canonical JSON. Records are stored in this form.
func Normalize(data json.RawMessage) (json.RawMessage, error) {
	if len(data) == 0 {
		return data, nil
	}
	v, err := canon.Decode(data)
	if err != nil {
		return nil, err
	}
	return canon.Encode(sortSets(v, nil))
}

// Responses compares a response with the expected response to a method.
func Responses(method string, want, got rpc.Response, opts Options) ([]Difference, error) {
	wantTree, err := tree(method, want, opts)
	if err != nil {
		return nil, fmt.Errorf("expected response: %w", err)
	}
	gotTree, err := tree(method, got, opts)
	if err != nil {
		return nil, fmt.Errorf("response: %w", err)
	}
	var diffs []Difference
	compare(nil, wantTree, gotTree, &diffs)
	return diffs, nil
}

// tree decodes a response into the form that is compared: sets sorted, and
// values that are not part of the result made fixed.
func tree(method string, r rpc.Response, opts Options) (map[string]any, error) {
	out := make(map[string]any)
	if r.Error != nil {
		v, err := canon.Decode(r.Error)
		if err != nil {
			return nil, err
		}
		if e, ok := v.(map[string]any); ok && !opts.StrictErrorData {
			delete(e, "data")
		}
		out["error"] = v
	}
	if r.Result != nil {
		v, err := canon.Decode(r.Result)
		if err != nil {
			return nil, err
		}
		out["result"] = fixResult(method, sortSets(v, nil))
	}
	return out, nil
}

func fixResult(method string, result any) any {
	switch method {
	case "starknet_specVersion":
		// A patch release does not change the API, so compare major.minor.
		if s, ok := result.(string); ok {
			if mm, err := corpus.MajorMinor(s); err == nil {
				return mm
			}
		}
	case "starknet_getEvents":
		// The continuation token is opaque: compare only that it is there.
		if m, ok := result.(map[string]any); ok {
			if _, has := m["continuation_token"]; has {
				m["continuation_token"] = "<present>"
			}
		}
	}
	return result
}

func sortSets(v any, path []string) any {
	switch v := v.(type) {
	case map[string]any:
		for k, child := range v {
			v[k] = sortSets(child, append(path, k))
		}
	case []any:
		for i, child := range v {
			v[i] = sortSets(child, append(path, "*"))
		}
		if rule, ok := findSortRule(path); ok {
			sortItems(v, rule.keys)
		}
	}
	return v
}

func sortItems(items []any, keys []string) {
	sortKeys := make([]string, len(items))
	for i, item := range items {
		sortKeys[i] = itemSortKey(item, keys)
	}
	idx := make([]int, len(items))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool { return sortKeys[idx[a]] < sortKeys[idx[b]] })
	sorted := make([]any, len(items))
	for i, j := range idx {
		sorted[i] = items[j]
	}
	copy(items, sorted)
}

func itemSortKey(item any, keys []string) string {
	m, ok := item.(map[string]any)
	if len(keys) == 0 || !ok {
		data, _ := canon.Encode(item)
		return string(data)
	}
	parts := make([]string, len(keys))
	for i, k := range keys {
		data, _ := canon.Encode(m[k])
		parts[i] = string(data)
	}
	return strings.Join(parts, "\x00")
}

func compare(path []string, want, got any, diffs *[]Difference) {
	if len(*diffs) >= MaxDifferences {
		return
	}
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			addDiff(path, want, got, diffs)
			return
		}
		keys := make([]string, 0, len(w)+len(g))
		for k := range w {
			keys = append(keys, k)
		}
		for k := range g {
			if _, ok := w[k]; !ok {
				keys = append(keys, k)
			}
		}
		slices.Sort(keys)
		for _, k := range keys {
			wv, wok := w[k]
			gv, gok := g[k]
			if !wok || !gok {
				addMissing(append(path, k), wv, wok, gv, gok, diffs)
				continue
			}
			compare(append(path, k), wv, gv, diffs)
		}
	case []any:
		g, ok := got.([]any)
		if !ok {
			addDiff(path, want, got, diffs)
			return
		}
		if len(w) != len(g) {
			*diffs = append(*diffs, Difference{
				Path: pointer(path),
				Want: fmt.Sprintf("%d items", len(w)),
				Got:  fmt.Sprintf("%d items", len(g)),
			})
		}
		for i := range min(len(w), len(g)) {
			compare(append(path, strconv.Itoa(i)), w[i], g[i], diffs)
		}
	default:
		// Strings, json.Number, bool and nil: compare the exact text, so
		// "0x01" differs from "0x1" and 1.0 differs from 1.
		if want != got {
			addDiff(path, want, got, diffs)
		}
	}
}

func addMissing(path []string, want any, wantOK bool, got any, gotOK bool, diffs *[]Difference) {
	d := Difference{Path: pointer(path), Want: "<absent>", Got: "<absent>"}
	if wantOK {
		d.Want = short(want)
	}
	if gotOK {
		d.Got = short(got)
	}
	*diffs = append(*diffs, d)
}

func addDiff(path []string, want, got any, diffs *[]Difference) {
	*diffs = append(*diffs, Difference{Path: pointer(path), Want: short(want), Got: short(got)})
}

func short(v any) string {
	data, err := canon.Encode(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	if len(data) > maxValueLen {
		return string(data[:maxValueLen]) + "..."
	}
	return string(data)
}

func pointer(path []string) string {
	if len(path) == 0 {
		return "/"
	}
	escaped := make([]string, len(path))
	for i, p := range path {
		escaped[i] = strings.ReplaceAll(strings.ReplaceAll(p, "~", "~0"), "/", "~1")
	}
	return "/" + strings.Join(escaped, "/")
}
