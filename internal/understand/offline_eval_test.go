//go:build eval

package understand

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/cloudwego/eino-ext/components/model/qwen"
)

type evalSample struct {
	Query            string `json:"query"`
	ExpectedDomain   string `json:"expected_domain"`
	ExpectedCategory string `json:"expected_category"`
}

func TestOfflineIntentEval(t *testing.T) {
	apiKey := os.Getenv("DASHSCOPE_API_KEY")
	if apiKey == "" {
		t.Skip("DASHSCOPE_API_KEY not set; skipping offline eval")
	}

	model := os.Getenv("UNDERSTAND_LLM_MODEL")
	if model == "" {
		model = "qwen-turbo"
	}

	jsonlPath := filepath.Join("..", "..", "docs", "research", "phase2", "eval", "wave1-intent.jsonl")
	samples, err := loadEvalSet(jsonlPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) == 0 {
		t.Fatal("no eval samples found")
	}

	tree, err := LoadTree("tree.yaml")
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	chatModel, err := qwen.NewChatModel(ctx, &qwen.ChatModelConfig{
		BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1",
		APIKey:  apiKey,
		Model:   model,
	})
	if err != nil {
		t.Fatal(err)
	}
	classifier := NewClassifier(chatModel, tree)

	domainCorrect := 0
	categoryCorrect := 0
	confusion := map[string]map[string]int{}

	for _, s := range samples {
		res, err := classifier.Classify(ctx, s.Query)
		if err != nil {
			t.Errorf("classify %q: %v", s.Query, err)
			continue
		}
		if confusion[s.ExpectedDomain] == nil {
			confusion[s.ExpectedDomain] = map[string]int{}
		}
		confusion[s.ExpectedDomain][res.Domain]++
		if res.Domain == s.ExpectedDomain {
			domainCorrect++
			if s.ExpectedCategory == "" || res.Category == s.ExpectedCategory {
				categoryCorrect++
			}
		}
	}

	total := len(samples)
	t.Logf("=== Wave 1 Intent Eval (model=%s, samples=%d) ===", model, total)
	t.Logf("Domain accuracy:   %d/%d = %.1f%%", domainCorrect, total, 100*float64(domainCorrect)/float64(total))
	t.Logf("Category accuracy: %d/%d = %.1f%%", categoryCorrect, total, 100*float64(categoryCorrect)/float64(total))
	t.Logf("Confusion matrix (rows=expected, cols=predicted):")
	domains := sortedKeys(confusion)
	for _, exp := range domains {
		preds := sortedKeys(confusion[exp])
		for _, pred := range preds {
			t.Logf("  %-10s -> %-10s : %d", exp, pred, confusion[exp][pred])
		}
	}
}

func loadEvalSet(path string) ([]evalSample, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	var out []evalSample
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var s evalSample
		if err := json.Unmarshal(line, &s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, sc.Err()
}

func sortedKeys[T any](m map[string]T) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
