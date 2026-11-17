package main

import (
	"fmt"
	"io"

	"github.com/NethermindEth/starknet-golden-tests/internal/corpus"
)

func runValidate(args []string, stdout, stderr io.Writer) error {
	fs := newFlagSet("validate", stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		return fmt.Errorf("validate needs one or more corpus files or directories")
	}
	invalid := 0
	for _, root := range fs.Args() {
		files, err := corpusFiles(root)
		if err != nil {
			return err
		}
		for _, f := range files {
			c, err := corpus.Load(f.path)
			if err == nil {
				err = c.Validate()
			}
			if err != nil {
				invalid++
				fmt.Fprintf(stdout, "INVALID %s:\n  %v\n", f.path, indent(err.Error()))
				continue
			}
			fmt.Fprintf(stdout, "ok %s (%d requests)\n", f.path, len(c.Requests()))
		}
	}
	if invalid > 0 {
		return fmt.Errorf("%d invalid corpora: %w", invalid, errFailed)
	}
	return nil
}
