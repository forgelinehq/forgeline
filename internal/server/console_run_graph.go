package server

import (
	"context"
	"fmt"
)

// The graph a run card draws for a workflow run: what runs, in what order, and how far it got. It carries names and
// statuses only; what a node was given or produced stays on the run's own page.

// RunViewGraph is a run as nodes and wires.
type RunViewGraph struct {
	Nodes []RunViewNode `json:"nodes"`
	Edges []RunViewEdge `json:"edges"`
}

// RunViewNode is one thing the run does. Status is pending, running, waiting, succeeded, failed, skipped or cancelled.
type RunViewNode struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Label  string `json:"label"`
	Status string `json:"status"`
}

// RunViewEdge is a wire; Taken says the run went along it.
type RunViewEdge struct {
	From  string `json:"from"`
	To    string `json:"to"`
	When  string `json:"when,omitempty"`
	Taken bool   `json:"taken,omitempty"`
}

// workflowRunGraph draws a run: a graph workflow as the version it ran, a staged one as the stages the run recorded.
// It is nil when that version is gone, and the card then lists the steps only.
func (s *Server) workflowRunGraph(ctx context.Context, run WFRun) *RunViewGraph {
	agentName := func(id string) string {
		if a, err := s.loadAgent(ctx, id); err == nil {
			return a.Name
		}
		return id
	}
	if !run.Graph {
		return stageRunGraph(run, agentName)
	}
	w, err := s.loadWorkflow(ctx, run.Workflow)
	if err != nil {
		return nil
	}
	v, ok := w.version(run.Version)
	// A workflow deleted and made again has the same id and version numbers but another graph, so the hash decides.
	if !ok || v.Graph == nil || v.Hash != run.Hash {
		return nil
	}
	return graphRunGraph(run, *v.Graph, agentName)
}

func graphRunGraph(run WFRun, g WFGraph, agentName func(string) string) *RunViewGraph {
	last := map[string]WFNodeRun{}
	for _, v := range run.Visits {
		last[v.Node] = v
	}
	out := &RunViewGraph{}
	for _, n := range g.Nodes {
		status := "pending"
		if v, ok := last[n.ID]; ok {
			status = v.Status
		}
		if run.Status == wfCancelled && (status == wfRunning || status == wfWaiting) {
			status = wfCancelled
		}
		label := n.Name
		if label == "" {
			switch n.Kind {
			case "agent":
				label = agentName(n.Agent)
			case "lookup":
				label = n.Tool
			case "ask":
				label = "Question"
			case "decide":
				label = "Decide"
			case "merge":
				label = "Merge"
			case "start":
				label = "Start"
			default:
				label = n.ID
			}
		}
		out.Nodes = append(out.Nodes, RunViewNode{ID: n.ID, Kind: n.Kind, Label: label, Status: status})
	}
	for _, e := range g.Edges {
		v, ok := last[e.From]
		taken := ok && v.Status == wfSucceeded && (e.When == "" || v.Branch == e.When)
		out.Edges = append(out.Edges, RunViewEdge{From: e.From, To: e.To, When: e.When, Taken: taken})
	}
	return out
}

// stageRunGraph is a staged run as columns: the start, then per stage an approval when it is gated and its steps.
func stageRunGraph(run WFRun, agentName func(string) string) *RunViewGraph {
	out := &RunViewGraph{}
	var before []RunViewNode
	column := func(nodes []RunViewNode) {
		for _, a := range before {
			for _, b := range nodes {
				taken := a.Status == wfSucceeded && b.Status != "pending" && b.Status != "skipped"
				out.Edges = append(out.Edges, RunViewEdge{From: a.ID, To: b.ID, Taken: taken})
			}
		}
		out.Nodes = append(out.Nodes, nodes...)
		before = nodes
	}
	column([]RunViewNode{{ID: "start", Kind: "start", Label: "Start", Status: wfSucceeded}})
	for i, st := range run.Stages {
		if st.Gate {
			status := "pending"
			switch {
			case st.Status == wfWaiting:
				status = wfWaiting
			case st.GateBy != "":
				status = wfSucceeded
			case st.Status == wfSkipped:
				status = wfSkipped
			}
			column([]RunViewNode{{ID: fmt.Sprintf("gate-%d", i), Kind: "ask", Label: "Approve " + st.Name, Status: status}})
		}
		var steps []RunViewNode
		for j, sp := range st.Steps {
			status := sp.Status
			// A cancelled or interrupted run marks the stage only; its steps will not run or finish either.
			if st.Status == wfSkipped && (status == wfPending || status == wfRunning) {
				status = wfSkipped
			}
			steps = append(steps, RunViewNode{ID: fmt.Sprintf("step-%d-%d", i, j), Kind: "agent", Label: agentName(sp.Agent), Status: status})
		}
		if len(steps) > 0 {
			column(steps)
		}
	}
	return out
}
