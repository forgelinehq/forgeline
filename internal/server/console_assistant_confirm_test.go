package server

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/forgelinehq/forgeline/internal/docstore"
)

// spyDocs reads a card at the moment the action it stands for starts to change something.
type spyDocs struct {
	docstore.Store
	onCreate func(kind string)
}

func (d *spyDocs) Create(ctx context.Context, kind, id string, v any) error {
	if d.onCreate != nil {
		d.onCreate(kind)
	}
	return d.Store.Create(ctx, kind, id, v)
}

// A card is no longer pending by the time its action starts, so a server that stops part-way, or a browser that goes
// away, cannot leave a card that can be pressed a second time and do the action twice.
func TestACardIsNotPendingWhileItsActionRuns(t *testing.T) {
	m := newMCPHarness(t)
	evs := m.run(m.dev, map[string]any{"message": "make me a demo pipeline"}, prepareSave(twoSteps))
	id := actionsIn(evs)[0]["id"].(string)

	var seen string
	spy := &spyDocs{Store: m.srv.docs}
	spy.onCreate = func(kind string) {
		if kind != kindPipeline {
			return
		}
		var a AssistAction
		if err := spy.Store.Get(context.Background(), kindAssistAction, id, &a); err == nil {
			seen = a.Status
		}
	}
	m.srv.docs = spy

	var a AssistAction
	json.Unmarshal(m.dev.expect(200, "POST", "/v1/assistant/actions/"+id+"/confirm", nil), &a)
	if a.Status != actDone {
		t.Fatalf("action = %+v", a)
	}
	if seen == "" {
		t.Fatal("the action never reached the pipeline store, so the test checked nothing")
	}
	if seen == actPending {
		t.Error("the card was still pending while its action ran: a stop part-way would let it be pressed again")
	}
}
