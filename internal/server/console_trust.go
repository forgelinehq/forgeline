package server

import (
	"strings"

	"github.com/forgelinehq/forgeline/internal/store"
)

// How much a run is trusted (design note 005, T4 and T5, slice 3). It follows from how the run started, so it cannot be claimed
// by the pipeline file: a run started in the console or through the API by a signed-in person is trusted; a run for a push to a
// branch an administrator allowed is trusted, with secrets only where the repository's rule says so; a run for a pull request
// is untrusted, because its pipeline file and its code come from whoever opened it. An untrusted run is refused every secret.

const (
	trustTrusted   = "trusted"
	trustUntrusted = "untrusted"
)

func trustOf(r store.Run) string {
	if r.Trigger == store.TriggerGitHub && strings.HasPrefix(r.SourceRef, "refs/pull/") {
		return trustUntrusted
	}
	return trustTrusted
}
