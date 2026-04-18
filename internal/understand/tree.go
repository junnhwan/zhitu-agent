package understand

import (
	"fmt"
	"os"

	"github.com/goccy/go-yaml"
)

type Category struct {
	Name  string `yaml:"name"`
	Route string `yaml:"route"`
}

type Domain struct {
	Name        string     `yaml:"name"`
	Description string     `yaml:"description"`
	Route       string     `yaml:"route,omitempty"`
	Categories  []Category `yaml:"categories,omitempty"`
}

type Tree struct {
	Domains []Domain `yaml:"domains"`
}

func LoadTree(path string) (*Tree, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read tree file: %w", err)
	}
	var tree Tree
	if err := yaml.Unmarshal(data, &tree); err != nil {
		return nil, fmt.Errorf("parse tree yaml: %w", err)
	}
	if len(tree.Domains) == 0 {
		return nil, fmt.Errorf("tree has no domains")
	}
	return &tree, nil
}
