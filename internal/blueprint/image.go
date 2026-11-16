package blueprint

import (
	"fmt"
	"maps"
	"slices"

	"github.com/forgelinehq/forgeline/internal/template"
)

// imageInput is the template input that holds the container image a template runs (the Lambda template's "image").
const imageInput = "image"

// BlockImage is the container image a block runs, and whether the block runs one Forgeline can point at a new image:
// an app, an agent, or a template with an image input.
func (b Blueprint) BlockImage(blockID string) (string, bool) {
	bl := blockByID(b, blockID)
	switch bl.Kind {
	case KindService, KindAgent:
		return bl.Image, true
	case KindTemplate:
		t, err := template.Get(bl.Template)
		if err != nil || !slices.Contains(t.Inputs.Order, imageInput) {
			return "", false
		}
		s, _ := bl.Inputs[imageInput].(string)
		return s, true
	}
	return "", false
}

// WithImage is a copy of the blueprint with one block running image. The copy is not validated here; the caller
// validates it like any other edit.
func (b Blueprint) WithImage(blockID, image string) (Blueprint, error) {
	if _, ok := b.BlockImage(blockID); !ok {
		return b, fmt.Errorf("%s does not run a container image", blockID)
	}
	out := b
	out.Blocks = slices.Clone(b.Blocks)
	for i := range out.Blocks {
		bl := &out.Blocks[i]
		if bl.ID != blockID {
			continue
		}
		if bl.Kind == KindTemplate {
			bl.Inputs = maps.Clone(bl.Inputs)
			if bl.Inputs == nil {
				bl.Inputs = map[string]any{}
			}
			bl.Inputs[imageInput] = image
		} else {
			bl.Image = image
		}
	}
	return out, nil
}
