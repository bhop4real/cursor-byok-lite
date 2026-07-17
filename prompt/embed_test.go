package prompt

import (
	"strings"
	"testing"
	"unicode"
)

func TestPromptAssetsAreEnglish(t *testing.T) {
	t.Parallel()

	modePrompts := map[string]func() (string, error){
		"agent":      func() (string, error) { return ReadPrompt(ModeAgent) },
		"ask":        func() (string, error) { return ReadPrompt(ModeAsk) },
		"plan":       func() (string, error) { return ReadPrompt(ModePlan) },
		"debug":      func() (string, error) { return ReadPrompt(ModeDebug) },
		"multitask":  func() (string, error) { return ReadPrompt(ModeMultitask) },
		"subagent":   func() (string, error) { return ReadPrompt(ModeSubagent) },
		"commit":     ReadCommitPrompt,
		"compaction": ReadCompactionPrompt,
	}

	for name, readPrompt := range modePrompts {
		name, readPrompt := name, readPrompt
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			text, err := readPrompt()
			if err != nil {
				t.Fatalf("read prompt: %v", err)
			}
			if strings.TrimSpace(text) == "" {
				t.Fatal("prompt is empty")
			}
			for _, value := range text {
				if unicode.Is(unicode.Han, value) {
					t.Fatalf("prompt contains non-English Han character %q", value)
				}
			}
		})
	}
}

func TestCommonPrefixDetectsRequestLanguageAndDefaultsToEnglish(t *testing.T) {
	t.Parallel()

	text, err := ReadPrompt(ModeAgent)
	if err != nil {
		t.Fatalf("read agent prompt: %v", err)
	}
	for _, required := range []string{
		"Detect the language used in the user's current request",
		"An explicit language instruction takes precedence.",
		"If the language cannot be determined, default to English.",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("common prompt is missing language rule %q", required)
		}
	}
	for _, forbidden := range []string{
		"default to Simplified Chinese",
		"respond in Simplified Chinese",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("common prompt still contains Chinese fallback %q", forbidden)
		}
	}
}

func TestInteractivePromptsDefineDeterministicExecutionDiscipline(t *testing.T) {
	t.Parallel()

	modes := []Mode{ModeAgent, ModeAsk, ModePlan, ModeDebug, ModeMultitask, ModeSubagent}
	for _, mode := range modes {
		mode := mode
		t.Run(string(mode), func(t *testing.T) {
			t.Parallel()
			text, err := ReadPrompt(mode)
			if err != nil {
				t.Fatalf("read prompt: %v", err)
			}
			for _, required := range []string{
				"<execution_discipline>",
				"minimal",
				"Do not",
			} {
				if !strings.Contains(text, required) {
					t.Fatalf("prompt is missing deterministic execution constraint %q", required)
				}
			}
		})
	}
}
