package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/NethermindEth/starknet-golden-tests/internal/diff"
	"github.com/NethermindEth/starknet-golden-tests/internal/rpc"
)

// maxReportResponse is the most bytes of each response in failures.jsonl.
const maxReportResponse = 64 << 10

type counts struct {
	pass, fail, skip, notRecorded int
}

func (c *counts) add(s status) {
	switch s {
	case statusPass:
		c.pass++
	case statusFail:
		c.fail++
	case statusSkip:
		c.skip++
	case statusNotRecorded:
		c.notRecorded++
	}
}

type summary struct {
	methods  []string
	byMethod map[string]*counts
	total    counts
}

func summarize(results []result) summary {
	s := summary{byMethod: make(map[string]*counts)}
	for _, r := range results {
		m := r.request.Method
		if r.group != nil {
			m += " (equivalent)"
		}
		if s.byMethod[m] == nil {
			s.byMethod[m] = &counts{}
			s.methods = append(s.methods, m)
		}
		s.byMethod[m].add(r.status)
		s.total.add(r.status)
	}
	slices.Sort(s.methods)
	return s
}

// name is how a failure is shown: the label, or the corpus and request id.
func (r result) name() string {
	if r.group != nil {
		return fmt.Sprintf("%s group %v", r.corpus, r.group)
	}
	if r.label != "" {
		return r.corpus + " " + r.label
	}
	return fmt.Sprintf("%s id %d", r.corpus, r.request.ID)
}

func printResults(w io.Writer, results []result, s summary) {
	for _, r := range results {
		switch r.status {
		case statusFail:
			fmt.Fprintf(w, "FAIL %s %s\n", r.name(), r.request.Method)
			if r.err != "" {
				fmt.Fprintf(w, "  %s: %s\n", r.other, r.err)
			}
			if len(r.diffs) > 0 {
				fmt.Fprintf(w, "  %s differs from %s:\n", r.other, r.reference)
			}
			for _, d := range r.diffs {
				fmt.Fprintf(w, "    %s\n", d)
			}
		case statusNotRecorded:
			fmt.Fprintf(w, "NOT RECORDED %s %s in %s\n", r.name(), r.request.Method, r.other)
		}
	}
	fmt.Fprintln(w)
	writeTable(w, s, false)
}

// writeTable writes the counts for each method, as text or as Markdown.
func writeTable(w io.Writer, s summary, markdown bool) {
	header := []string{"method", "pass", "fail", "skip (head)", "not recorded"}
	rows := make([][]string, 0, len(s.methods)+1)
	row := func(name string, c counts) []string {
		return []string{name, fmt.Sprint(c.pass), fmt.Sprint(c.fail), fmt.Sprint(c.skip), fmt.Sprint(c.notRecorded)}
	}
	for _, m := range s.methods {
		rows = append(rows, row(m, *s.byMethod[m]))
	}
	rows = append(rows, row("total", s.total))

	if markdown {
		fmt.Fprintf(w, "| %s |\n|%s\n", strings.Join(header, " | "), strings.Repeat(" --- |", len(header)))
		for _, r := range rows {
			fmt.Fprintf(w, "| %s |\n", strings.Join(r, " | "))
		}
		return
	}
	widths := make([]int, len(header))
	for _, r := range append([][]string{header}, rows...) {
		for i, cell := range r {
			widths[i] = max(widths[i], len(cell))
		}
	}
	for _, r := range append([][]string{header}, rows...) {
		cells := make([]string, len(r))
		for i, cell := range r {
			cells[i] = fmt.Sprintf("%-*s", widths[i], cell)
		}
		fmt.Fprintln(w, strings.TrimRight(strings.Join(cells, "  "), " "))
	}
}

type failureLine struct {
	Corpus    string            `json:"corpus"`
	ID        int               `json:"id"`
	Label     string            `json:"label,omitempty"`
	Group     []int             `json:"group,omitempty"`
	Method    string            `json:"method"`
	Key       string            `json:"key"`
	Params    json.RawMessage   `json:"params,omitempty"`
	Status    string            `json:"status"`
	Reference string            `json:"reference,omitempty"`
	Other     string            `json:"other,omitempty"`
	Error     string            `json:"error,omitempty"`
	Diffs     []diff.Difference `json:"diffs,omitempty"`
	Want      string            `json:"want,omitempty"`
	Got       string            `json:"got,omitempty"`
}

func writeReport(dir string, results []result, s summary) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	md, err := os.Create(filepath.Join(dir, "summary.md"))
	if err != nil {
		return err
	}
	fmt.Fprintf(md, "## corpus-diff\n\n")
	writeTable(md, s, true)
	if err := md.Close(); err != nil {
		return err
	}

	f, err := os.Create(filepath.Join(dir, "failures.jsonl"))
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	for _, r := range results {
		if r.status != statusFail && r.status != statusNotRecorded {
			continue
		}
		line := failureLine{
			Corpus: r.corpus, ID: r.request.ID, Label: r.label, Group: r.group,
			Method: r.request.Method, Key: r.request.Key, Params: r.request.Params,
			Status: "fail", Reference: r.reference, Other: r.other, Error: r.err, Diffs: r.diffs,
			Want: responseText(r.want), Got: responseText(r.got),
		}
		if r.status == statusNotRecorded {
			line.Status = "not recorded"
		}
		if err := enc.Encode(line); err != nil {
			return errors.Join(err, f.Close())
		}
	}
	return f.Close()
}

// responseText returns a response as JSON, cut to maxReportResponse bytes.
func responseText(r rpc.Response) string {
	if r.Result == nil && r.Error == nil {
		return ""
	}
	data, err := json.Marshal(r)
	if err != nil {
		return err.Error()
	}
	if len(data) > maxReportResponse {
		return string(data[:maxReportResponse]) + "..."
	}
	return string(data)
}
