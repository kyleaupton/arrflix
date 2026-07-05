package jobutil

import (
	"encoding/json"

	"github.com/google/uuid"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/parsing"
	"github.com/kyleaupton/arrflix/internal/qualityprofile"
	"github.com/kyleaupton/arrflix/internal/repo"
)

// FileOriginParams builds the domain-shaped params for a file_origin capture from
// the parse that produced a file's name. It keeps the three write sites (grab
// import, scan discovery, manual create) uniform: each passes the ParsedRelease
// it already computed plus the origin discriminator, and this projects the
// storable shape.
//
//   - bin_* are the projected Sonarr/Radarr bin (the exact projection proposals
//     store, via qualityprofile.BinOf) so library quality is directly comparable
//     across the app. All three are set together, or all nil when the parse
//     yielded no recognized bin — the columns' nullability distinguishes
//     "unknown" from a real bin, so a zero bin becomes NULL, not empty strings.
//   - release_group / edition are raw parsed reads (not binned); empty → nil.
//   - parsed is the full release blob for future tokens and parser-drift audit,
//     with MediaInfo stripped: it's re-gatherable and deliberately out of scope
//     for provenance, and dropping it keeps the blob a self-contained, FK-free
//     record that could later mirror to an FS sidecar/xattr.
//
// downloadJobID/indexerID/guid are the grab origin's durable release identity
// and soft backref; nil for scan/manual.
func FileOriginParams(
	fileID uuid.UUID,
	origin, sourceTitle string,
	parsed parsing.ParsedRelease,
	domain parsing.Domain,
	downloadJobID *uuid.UUID,
	indexerID *int64,
	guid *string,
) repo.CreateFileOriginParams {
	subj := model.NewSubject(model.DownloadCandidate{Title: sourceTitle}, parsed)

	var binSource, binResolution, binModifier *string
	if bin := qualityprofile.BinOf(subj, domain); bin != (parsing.BinKey{}) {
		s, r, m := string(bin.Source), string(bin.Resolution), string(bin.Modifier)
		binSource, binResolution, binModifier = &s, &r, &m
	}

	// Strip MediaInfo before marshaling: it's excluded from provenance and, for
	// the grab path, the subject carries an ffprobe MediaInfo by capture time.
	rel := subj.Release
	rel.MediaInfo = nil
	var parsedBlob []byte
	if b, err := json.Marshal(rel); err == nil {
		parsedBlob = b
	}

	return repo.CreateFileOriginParams{
		FileID:        fileID,
		Origin:        origin,
		SourceTitle:   strPtrOrNil(sourceTitle),
		BinSource:     binSource,
		BinResolution: binResolution,
		BinModifier:   binModifier,
		ReleaseGroup:  strPtrOrNil(subj.Release.Encode.ReleaseGroup.Value),
		Edition:       strPtrOrNil(subj.Release.Identity.Edition.Value),
		Parsed:        parsedBlob,
		IndexerID:     indexerID,
		Guid:          guid,
		DownloadJobID: downloadJobID,
	}
}

// strPtrOrNil returns a pointer to s, or nil when s is empty — mapping "no
// value" to a NULL column rather than an empty string.
func strPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
