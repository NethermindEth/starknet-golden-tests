package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// corpusFile is a corpus file found under a --corpus root.
type corpusFile struct {
	// name is the corpus path relative to the --corpus root, or the file name
	// if the root is a file.
	name string
	path string
	// inDir is true if the --corpus root is a directory.
	inDir bool
}

// recordsPath returns the records file of the corpus under recordsRoot: the
// same relative path with a .jsonl extension, or recordsRoot itself if the
// --corpus root is a file.
func (f corpusFile) recordsPath(recordsRoot string) string {
	if !f.inDir {
		return recordsRoot
	}
	return filepath.Join(recordsRoot, strings.TrimSuffix(f.name, ".json")+".jsonl")
}

// corpusFiles lists the corpora under root: the file itself, or each *.json
// file under the directory, recursively.
func corpusFiles(root string) ([]corpusFile, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return []corpusFile{{name: filepath.Base(root), path: root}}, nil
	}
	var files []corpusFile
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".json" {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, corpusFile{name: rel, path: path, inDir: true})
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("%s has no *.json corpus", root)
	}
	slices.SortFunc(files, func(a, b corpusFile) int { return strings.Compare(a.name, b.name) })
	return files, nil
}

// isDir reports whether path is an existing directory.
func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
