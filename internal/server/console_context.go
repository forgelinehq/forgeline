package server

import (
	"fmt"
	"strings"
)

// Context clearing (design note 052): a long task makes many lookups, each up to maxLookupResult, and the model is sent all
// of them again on every step. When the old results pass a budget, the oldest are replaced by a short note and the
// newest few are kept whole. The call each result answers stays, so the model still sees what it asked and can ask
// again; only the bulk goes. This is the "context editing" Anthropic's memory guidance describes, done for every model.

// contextBudget is the bytes of lookup results sent with a request before the oldest are cleared (a variable so a test can
// make it small).
var contextBudget = 96 << 10

const (
	keepRecentResults  = 4 // the newest results are never cleared
	resultTurnPrefix   = "Result of lookup "
	clearedResultNote  = "Result of lookup %s was cleared to save space. Its text is gone; call the tool again if you still need it, and do not rely on what it said earlier unless you wrote it down in your plan or answer."
	resultTurnLeadSize = 80 // how much of a result turn is read to find the tool's name
)

// isResultTurn says whether a turn is a lookup result and, if so, which tool answered.
func isResultTurn(t chatTurn) (tool string, ok bool) {
	if t.Role != "user" || !strings.HasPrefix(t.Content, resultTurnPrefix) {
		return "", false
	}
	rest := t.Content[len(resultTurnPrefix):]
	lead := rest[:min(len(rest), resultTurnLeadSize)]
	if i := strings.IndexAny(lead, ". \n"); i > 0 {
		return lead[:i], true
	}
	return "", false
}

// clearOldResults returns turns with the oldest lookup results replaced by a note, so that the results left total at
// most budget bytes, never touching the newest keep. It returns the same slice when nothing needs clearing, and says how
// many results it cleared.
func clearOldResults(turns []chatTurn, budget, keep int) ([]chatTurn, int) {
	var idx []int
	total := 0
	for i, t := range turns {
		if _, ok := isResultTurn(t); ok {
			idx = append(idx, i)
			total += len(t.Content)
		}
	}
	if total <= budget || len(idx) <= keep {
		return turns, 0
	}
	out := make([]chatTurn, len(turns))
	copy(out, turns)
	cleared := 0
	for _, i := range idx[:len(idx)-keep] {
		if total <= budget {
			break
		}
		tool, _ := isResultTurn(out[i])
		note := fmt.Sprintf(clearedResultNote, tool)
		total -= len(out[i].Content) - len(note)
		out[i].Content = note
		cleared++
	}
	return out, cleared
}
