package channel

import (
	"reflect"
	"testing"
)

func TestExtractPhrasesMixed(t *testing.T) {
	got := ExtractPhrases("什么是 Eino Graph？")
	want := []string{"什么是", "Eino", "Graph"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestExtractPhrasesStopwords(t *testing.T) {
	got := ExtractPhrases("什么 is the RAG 系统")
	// "什么" len 2 CJK → kept; "is"/"the" stopwords → dropped; "RAG" len 3 → kept; "系统" → kept
	want := []string{"什么", "RAG", "系统"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestExtractPhrasesEmpty(t *testing.T) {
	if got := ExtractPhrases("   ,.?"); len(got) != 0 {
		t.Errorf("got %v", got)
	}
}

func TestExtractPhrasesLenFilter(t *testing.T) {
	got := ExtractPhrases("a b cd efg 啊 你")
	// a/b len 1 → drop; cd len 2 ASCII → drop; efg len 3 → keep; 啊 len 1 CJK → drop; 你 len 1 → drop
	want := []string{"efg"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
}
