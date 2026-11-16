package server

import (
	"encoding/json"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/forgelinehq/forgeline/internal/blueprint"
)

// Moving a block to a new image (design note 040, step 4). A block with a source has its own private ECR repository,
// made by the blueprint. Forgeline lists what was pushed there (a read) and, when a person picks one, saves the
// blueprint with that image as a new revision. Running it is still a preview that is sent for approval: nothing here
// changes the cloud. Only images from the block's own repository are accepted, so this path cannot point a block at
// an image from somewhere else; that stays an ordinary edit on the canvas.

const maxBlockImages = 20

// BlockImageJSON is one image in a block's repository.
type BlockImageJSON struct {
	Image    string    `json:"image"` // the full reference to run: <repository>:<tag>, or @<digest> without a tag
	Tags     []string  `json:"tags"`
	Digest   string    `json:"digest"`
	PushedAt time.Time `json:"pushed_at"`
	SizeMB   int64     `json:"size_mb"`
	Current  bool      `json:"current"` // the block runs this image now (in the saved blueprint)
}

var imageTagRE = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]{0,127}$`)

// blockRepository is the ECR repository a built blueprint made for a block, or a refusal saying what is missing.
func (s *Server) blockRepository(r *http.Request, b StoredBlueprint, blockID string) (string, *httpErr) {
	bl, ok := blockOf(b.Blueprint, blockID)
	if !ok {
		return "", refusal(http.StatusNotFound, "no block "+blockID+" in this blueprint")
	}
	if _, runs := b.BlockImage(blockID); !runs {
		return "", refusal(http.StatusConflict, bl.Name+" does not run a container image")
	}
	if bl.Source == nil {
		return "", refusal(http.StatusConflict, bl.Name+" has no source repository, so the blueprint makes no image repository for it")
	}
	st, built, err := s.loadBPState(r.Context(), b.ID)
	if err != nil {
		return "", refusal(http.StatusInternalServerError, "could not read the blueprint's state")
	}
	if !built || st.Empty || st.AWSAccount == "" {
		return "", refusal(http.StatusConflict, "build the blueprint first: "+bl.Name+"'s image repository is made when it is built")
	}
	return st.AWSAccount + ".dkr.ecr." + st.Region + ".amazonaws.com/" + b.ImageRepoName(blockID), nil
}

func blockOf(b blueprint.Blueprint, id string) (blueprint.Block, bool) {
	for _, bl := range b.Blocks {
		if bl.ID == id {
			return bl, true
		}
	}
	return blueprint.Block{}, false
}

// handleBlockImages lists the newest images pushed to a block's own repository.
func (s *Server) handleBlockImages(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	b, err := s.loadBlueprint(ctx, r.PathValue("id"))
	if err != nil {
		s.defError(w, "blueprint", err)
		return
	}
	block := r.PathValue("block")
	repo, e := s.blockRepository(r, b, block)
	if e != nil {
		writeError(w, e.status, e.msg)
		return
	}
	st, err := s.awsFor(ctx, b.Target.Account)
	if err != nil || !st.Enabled {
		writeError(w, http.StatusConflict, "the blueprint's AWS account is not connected")
		return
	}
	st.Region = b.Target.Region
	name := repo[strings.Index(repo, "/")+1:]
	out, err := s.aws(ctx, st, "ecr", "describe-images", "--repository-name", name, "--filter", "tagStatus=ANY")
	if err != nil {
		writeError(w, http.StatusBadGateway, "could not list the images in "+name+": "+err.Error())
		return
	}
	var resp struct {
		ImageDetails []struct {
			ImageDigest      string   `json:"imageDigest"`
			ImageTags        []string `json:"imageTags"`
			ImagePushedAt    any      `json:"imagePushedAt"`
			ImageSizeInBytes int64    `json:"imageSizeInBytes"`
		} `json:"imageDetails"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		writeError(w, http.StatusBadGateway, "ECR answered with something Forgeline does not understand")
		return
	}
	current, _ := b.BlockImage(block)
	images := []BlockImageJSON{}
	for _, d := range resp.ImageDetails {
		img := BlockImageJSON{Tags: d.ImageTags, Digest: d.ImageDigest, PushedAt: ecrTime(d.ImagePushedAt), SizeMB: d.ImageSizeInBytes >> 20}
		if img.Tags == nil {
			img.Tags = []string{}
		}
		sort.Strings(img.Tags)
		if len(img.Tags) > 0 {
			img.Image = repo + ":" + img.Tags[0]
		} else {
			img.Image = repo + "@" + d.ImageDigest
		}
		img.Current = current == repo+"@"+d.ImageDigest
		for _, t := range img.Tags {
			img.Current = img.Current || current == repo+":"+t
		}
		images = append(images, img)
	}
	sort.SliceStable(images, func(i, j int) bool { return images[i].PushedAt.After(images[j].PushedAt) })
	if len(images) > maxBlockImages {
		images = images[:maxBlockImages]
	}
	writeJSON(w, http.StatusOK, map[string]any{"repository": repo, "current": current, "images": images})
}

// ecrTime reads imagePushedAt, which the AWS CLI prints as an ISO time (or, in older versions, epoch seconds).
func ecrTime(v any) time.Time {
	switch t := v.(type) {
	case string:
		if p, err := time.Parse(time.RFC3339Nano, t); err == nil {
			return p.UTC()
		}
	case float64:
		return time.Unix(int64(t), int64((t-float64(int64(t)))*1e9)).UTC()
	}
	return time.Time{}
}

// handleUseBlockImage saves the blueprint with a block running an image from its own repository, as a new revision.
// The console then asks for a preview; approving that preview is what runs the image.
func (s *Server) handleUseBlockImage(w http.ResponseWriter, r *http.Request) {
	ctx, p := r.Context(), principalFrom(r.Context())
	var body struct {
		Image    string `json:"image"`
		Revision int    `json:"revision"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	cur, err := s.loadBlueprint(ctx, r.PathValue("id"))
	if err != nil {
		s.defError(w, "blueprint", err)
		return
	}
	if body.Revision != cur.Revision {
		writeError(w, http.StatusConflict, cur.UpdatedBy+" changed this blueprint after you opened it; reload to see their version")
		return
	}
	block := r.PathValue("block")
	repo, e := s.blockRepository(r, cur, block)
	if e != nil {
		writeError(w, e.status, e.msg)
		return
	}
	if !ownImage(body.Image, repo) {
		s.audit(ctx, p, clientIP(r), "blueprint.image", cur.ID, block+": not its own repository", false)
		writeError(w, http.StatusBadRequest, "image: choose an image from "+repo+" (a tag or a sha256 digest); other images are set on the canvas")
		return
	}
	d, err := cur.WithImage(block, body.Image)
	if err == nil {
		err = d.Validate()
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	cur.Blueprint, cur.Revision, cur.UpdatedBy, cur.UpdatedAt = d, cur.Revision+1, p.Name, time.Now().UTC()
	if err := s.docs.Put(ctx, kindBlueprint, cur.ID, cur); err != nil {
		s.internalError(w, "saving the blueprint", err)
		return
	}
	s.audit(ctx, p, clientIP(r), "blueprint.image", cur.ID, block+" → "+body.Image, true)
	writeJSON(w, http.StatusOK, BlueprintJSON{StoredBlueprint: cur, Checks: checksOf(d)})
}

// ownImage says whether image is repo with a tag or a digest, and nothing else.
func ownImage(image, repo string) bool {
	if rest, ok := strings.CutPrefix(image, repo+":"); ok {
		return imageTagRE.MatchString(rest)
	}
	if rest, ok := strings.CutPrefix(image, repo+"@sha256:"); ok {
		return len(rest) == 64 && strings.Trim(rest, "0123456789abcdef") == ""
	}
	return false
}
