package blueprint

import (
	"encoding/json"
	"fmt"
)

// Removing what a blueprint built (design note 041). A removal is an ordinary plan of different code, so it goes
// through the same preview, guardrails, approval and exact-plan apply as a build:
//
//   - keep-data: the code keeps only the blocks that hold data (tables, buckets, databases, secrets), so the plan
//     removes everything else and those stay, still protected and still managed by the blueprint;
//   - unprotect: step 1 of removing everything. The same blocks, with deletion protection off (and a bucket allowed
//     to be deleted with what is in it), so the plan removes everything else and changes only that protection.
//     AWS refuses to delete a protected database or table, so this has to be applied first;
//   - all: no blocks at all, so the plan removes everything that is left.
const (
	RemoveKeepData  = "keep-data"
	RemoveUnprotect = "unprotect"
	RemoveAll       = "all"
)

// HoldsData says whether losing a block of this kind loses what people stored in it. A cache is not counted: what
// it holds can be rebuilt, and keeping it keeps a bill.
func HoldsData(kind string) bool {
	switch kind {
	case KindTable, KindBucket, KindDatabase, KindSecret:
		return true
	}
	return false
}

// RemovalTofu is the code whose plan removes what the blueprint built, for one stage. finalSnapshot keeps a last
// snapshot of each database when it is deleted (it costs a little storage until someone deletes it in AWS).
func (b Blueprint) RemovalTofu(stage string, finalSnapshot bool) ([]byte, error) {
	if err := b.Validate(); err != nil {
		return nil, err
	}
	kept := b
	kept.Blocks, kept.Links = nil, nil
	switch stage {
	case RemoveKeepData, RemoveUnprotect:
		ids := map[string]bool{}
		for _, bl := range b.Blocks {
			if HoldsData(bl.Kind) {
				kept.Blocks = append(kept.Blocks, bl)
				ids[bl.ID] = true
			}
		}
		for _, l := range b.Links {
			if ids[l.From] && ids[l.To] {
				kept.Links = append(kept.Links, l)
			}
		}
	case RemoveAll:
	default:
		return nil, fmt.Errorf("removal: unknown stage %q", stage)
	}
	return kept.tofu(removal{unprotect: stage == RemoveUnprotect, finalSnapshot: finalSnapshot})
}

// removal changes how data blocks are generated for step 1 of removing everything.
type removal struct {
	unprotect     bool
	finalSnapshot bool
}

// NeedsUnprotect reads a blueprint's OpenTofu state and says whether something in it must have its protection
// turned off before it can be deleted: a protected database or table, a bucket that cannot be deleted while it
// holds files, or a database whose final-snapshot setting is not the one asked for.
func NeedsUnprotect(state []byte, finalSnapshot bool) (bool, error) {
	var s struct {
		Resources []struct {
			Mode, Type string
			Instances  []struct {
				Attributes map[string]any `json:"attributes"`
			} `json:"instances"`
		} `json:"resources"`
	}
	if err := json.Unmarshal(state, &s); err != nil {
		return false, fmt.Errorf("could not read the state: %w", err)
	}
	for _, r := range s.Resources {
		if r.Mode != "managed" {
			continue
		}
		for _, in := range r.Instances {
			a := in.Attributes
			switch r.Type {
			case "aws_db_instance":
				if a["deletion_protection"] == true || a["skip_final_snapshot"] != !finalSnapshot {
					return true, nil
				}
			case "aws_dynamodb_table":
				if a["deletion_protection_enabled"] == true {
					return true, nil
				}
			case "aws_s3_bucket":
				if a["force_destroy"] != true {
					return true, nil
				}
			}
		}
	}
	return false, nil
}

// Resources counts the managed resources in a state: zero means nothing the blueprint built is left.
func Resources(state []byte) (int, error) {
	var s struct {
		Resources []struct {
			Mode      string
			Instances []json.RawMessage
		} `json:"resources"`
	}
	if err := json.Unmarshal(state, &s); err != nil {
		return 0, err
	}
	n := 0
	for _, r := range s.Resources {
		if r.Mode == "managed" {
			n += len(r.Instances)
		}
	}
	return n, nil
}
