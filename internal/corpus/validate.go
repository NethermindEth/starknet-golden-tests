package corpus

import (
	"errors"
	"fmt"
	"strconv"
)

// Validate returns every problem in the corpus, joined, or nil.
func (c *Corpus) Validate() error {
	var errs []error
	add := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }

	m := c.Meta
	if m.Method == "" {
		add("meta.method is empty")
	}
	if m.ChainID == "" {
		add("meta.chainId is empty")
	}
	if _, err := MajorMinor(m.RPCVersion); err != nil {
		add("meta.rpcVersion: %v", err)
	}
	if m.Count != len(c.Entries) {
		add("meta.count is %d, but the corpus has %d entries", m.Count, len(c.Entries))
	}

	ids := make(map[int]bool)
	for i, e := range c.Entries {
		switch {
		case m.Batch == 0 && e.Batch:
			add("entry %d is a batch, but meta.batch is 0", i)
		case m.Batch > 0 && !e.Batch:
			add("entry %d is not a batch, but meta.batch is %d", i, m.Batch)
		case m.Batch > 0 && len(e.Requests) != m.Batch:
			add("entry %d has %d requests, but meta.batch is %d", i, len(e.Requests), m.Batch)
		}
		for _, r := range e.Requests {
			if r.ID < 1 {
				add("entry %d: id must be a positive integer (got %d)", i, r.ID)
			} else if ids[r.ID] {
				add("entry %d: id %d is used more than once", i, r.ID)
			}
			ids[r.ID] = true
			if r.Method == "" {
				add("id %d: method is empty", r.ID)
			} else if m.Method != MixedMethod && r.Method != m.Method {
				add("id %d: method %s is not meta.method %s", r.ID, r.Method, m.Method)
			}
		}
	}

	for id := range m.Labels {
		n, err := strconv.Atoi(id)
		if err != nil || !ids[n] {
			add("meta.labels: no request has id %q", id)
		}
	}
	for g, group := range m.Equivalent {
		if len(group) < 2 {
			add("meta.equivalent[%d]: a group needs two or more ids", g)
		}
		for _, id := range group {
			if !ids[id] {
				add("meta.equivalent[%d]: no request has id %d", g, id)
			}
		}
	}
	return errors.Join(errs...)
}
