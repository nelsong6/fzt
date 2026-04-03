package yamlsrc

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/nelsong6/fzh/internal/model"
	"gopkg.in/yaml.v3"
)

// Entry represents a single node in the YAML tree.
// Children can be either inline entries or a file path string.
type Entry struct {
	Name        string      `yaml:"name"`
	Description string      `yaml:"description,omitempty"`
	Children    interface{} `yaml:"children,omitempty"` // []Entry or string (file path)
}

// LoadFromString parses YAML content directly without file I/O.
// File-reference children (children: "path/to/file.yaml") are not supported
// and will return an error.
func LoadFromString(content string) ([]model.Item, error) {
	var entries []Entry
	if err := yaml.Unmarshal([]byte(content), &entries); err != nil {
		return nil, fmt.Errorf("parsing YAML: %w", err)
	}
	var items []model.Item
	if err := flatten(entries, "", 0, -1, "", &items); err != nil {
		return nil, err
	}
	return items, nil
}

// Load reads a YAML file and recursively resolves file pointers,
// returning a flat list of model.Items with depth, parent, and children indices.
func Load(path string) ([]model.Item, error) {
	entries, err := readFile(path)
	if err != nil {
		return nil, fmt.Errorf("loading %s: %w", path, err)
	}

	baseDir := filepath.Dir(path)
	var items []model.Item
	if err := flatten(entries, baseDir, 0, -1, "", &items); err != nil {
		return nil, err
	}
	return items, nil
}

func readFile(path string) ([]Entry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var entries []Entry
	if err := yaml.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return entries, nil
}

func flatten(entries []Entry, baseDir string, depth int, parentIdx int, parentPath string, items *[]model.Item) error {
	for _, e := range entries {
		fields := []string{e.Name}
		if e.Description != "" {
			fields = append(fields, e.Description)
		}

		myIdx := len(*items)
		hasChildren := e.Children != nil

		path := e.Name
		if parentPath != "" {
			path = parentPath + " › " + e.Name
		}

		*items = append(*items, model.Item{
			Fields:      fields,
			Depth:       depth,
			ParentIdx:   parentIdx,
			HasChildren: hasChildren,
			Path:        path,
		})

		// Register this item as a child of its parent
		if parentIdx >= 0 {
			(*items)[parentIdx].Children = append((*items)[parentIdx].Children, myIdx)
		}

		if !hasChildren {
			continue
		}

		switch children := e.Children.(type) {
		case string:
			childPath := children
			if !filepath.IsAbs(childPath) {
				childPath = filepath.Join(baseDir, childPath)
			}
			childEntries, err := readFile(childPath)
			if err != nil {
				return fmt.Errorf("resolving children for %q: %w", e.Name, err)
			}
			childBaseDir := filepath.Dir(childPath)
			if err := flatten(childEntries, childBaseDir, depth+1, myIdx, path, items); err != nil {
				return err
			}

		case []interface{}:
			inlineEntries, err := parseInlineChildren(children)
			if err != nil {
				return fmt.Errorf("parsing inline children for %q: %w", e.Name, err)
			}
			if err := flatten(inlineEntries, baseDir, depth+1, myIdx, path, items); err != nil {
				return err
			}
		}
	}
	return nil
}

func parseInlineChildren(raw []interface{}) ([]Entry, error) {
	data, err := yaml.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var entries []Entry
	if err := yaml.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}
