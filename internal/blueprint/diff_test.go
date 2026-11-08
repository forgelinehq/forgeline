package blueprint

import (
	"strings"
	"testing"
)

func TestDiff(t *testing.T) {
	old := interviewer()
	nw := interviewer()
	nw.Blocks[1].Count, nw.Blocks[1].Image, nw.Blocks[1].X = 2, "nginx:1.30", 500 // a move is not a change
	nw.Blocks = nw.Blocks[:6]                                                     // remove the logs block
	nw.Links = []Link{nw.Links[0], nw.Links[1], {From: "app", To: "old-answers", Access: AccessReadWrite}, nw.Links[3], nw.Links[4]}
	nw.Blocks = append(nw.Blocks, Block{ID: "cache", Kind: KindTable, Name: "Cache", PartitionKey: "k", PartitionType: "S"})
	nw.Links = append(nw.Links, Link{From: "app", To: "cache", Access: AccessRead})
	got := strings.Join(Diff(old, nw), "\n")
	want := strings.Join([]string{
		"Changes Interviewer app: image, count",
		"Adds Cache (table)",
		"Removes Logs",
		"Links Interviewer app to Cache (reads)",
		"Changes the link from Interviewer app to Answers (reads and writes)",
		"Unlinks Interviewer app from Logs",
	}, "\n")
	if got != want {
		t.Errorf("diff:\n%s\nwant:\n%s", got, want)
	}
	if d := Diff(old, interviewer()); len(d) != 0 {
		t.Errorf("no change: %v", d)
	}
}
