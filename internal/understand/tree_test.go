package understand

import (
	"path/filepath"
	"testing"
)

func TestLoadTree(t *testing.T) {
	tree, err := LoadTree(filepath.Join(".", "tree.yaml"))
	if err != nil {
		t.Fatalf("LoadTree error: %v", err)
	}
	if len(tree.Domains) != 3 {
		t.Fatalf("expected 3 domains, got %d", len(tree.Domains))
	}
	names := map[string]bool{}
	for _, d := range tree.Domains {
		names[d.Name] = true
	}
	for _, want := range []string{"KNOWLEDGE", "TOOL", "CHITCHAT"} {
		if !names[want] {
			t.Errorf("missing domain %s", want)
		}
	}
}

func TestLoadTreeMissingFile(t *testing.T) {
	if _, err := LoadTree("nonexistent.yaml"); err == nil {
		t.Errorf("expected error for missing file")
	}
}

func TestLoadTreeCategoriesParsed(t *testing.T) {
	tree, err := LoadTree(filepath.Join(".", "tree.yaml"))
	if err != nil {
		t.Fatalf("LoadTree error: %v", err)
	}
	for _, d := range tree.Domains {
		if d.Name == "TOOL" {
			if len(d.Categories) != 2 {
				t.Errorf("TOOL expected 2 categories, got %d", len(d.Categories))
			}
			return
		}
	}
	t.Errorf("TOOL domain not found")
}
