package tools

import "encoding/json"

func init() {
	registry = append(registry, entry{Definition{
		Name: "make_word_search", Category: "study",
		Description: "Build a deterministic English-letter word search from 1 to 30 supplied words, 3 to 18 letters each. Accept ASCII letters and interior spaces, hyphens, apostrophes. Reject duplicate/reversed targets. Return native grid and zero-based verified placements plus separate printable script-free puzzle/answer-key HTML. Every target occurs exactly once across all eight directions. Bounded generation may fail: retry the same words with a larger size or another seed. No model, network, storage, or theme/fact verification.",
		InputSchema: object(map[string]any{"title": str(120), "words": array(str(64), 1, 30), "difficulty": choice("easy", "medium", "hard"), "size": integer(8, 24), "seed": integer(0, 2147483647)}, "title", "words", "difficulty"),
		Example:     json.RawMessage(`{"title":"Garden words","words":["flower","garden","honey"],"difficulty":"easy","seed":42}`),
	}, makeWordSearch})
}
