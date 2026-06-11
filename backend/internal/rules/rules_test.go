package rules

import (
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/parsing"
)

// Compact constructors for hand-rolled trees.

func fld(path string) *Operand { return &Operand{Kind: OperandField, Path: path} }
func lit(l Literal) *Operand   { return &Operand{Kind: OperandLiteral, Literal: &l} }

func litStr(s string) *Operand    { return lit(Literal{Type: LitString, Str: s}) }
func litEnum(s string) *Operand   { return lit(Literal{Type: LitEnum, Str: s}) }
func litInt(n int64) *Operand     { return lit(Literal{Type: LitInt, Int: n}) }
func litFloat(f float64) *Operand { return lit(Literal{Type: LitFloat, Float: f}) }
func litBool(b bool) *Operand     { return lit(Literal{Type: LitBool, Bool: b}) }
func litList(ss ...string) *Operand {
	return lit(Literal{Type: LitList, List: ss})
}

func leaf(op Operator, left, right *Operand) *Condition {
	return &Condition{Op: op, Left: left, Right: right}
}

func branch(op Operator, children ...*Condition) *Condition {
	return &Condition{Op: op, Children: children}
}

// searchSubject is a hand-rolled search-moment subject: indexer candidate +
// parse + media match; no MediaInfo, no Want yet.
func searchSubject() model.Subject {
	return model.Subject{
		Release: model.Release{
			Candidate: model.CandidateFields{
				Size:       4_000_000_000,
				Title:      "Dune.2021.2160p.BluRay.REMUX-FraMeSToR",
				Protocol:   "torrent",
				Seeders:    50,
				AgeHours:   5.5,
				Categories: []string{"Movies", "Movies/UHD"},
			},
			Quality: model.QualityFields{
				Resolution: parsing.Field[string]{Value: "2160p", Confidence: 0.9, Evidence: "2160p token"},
				Source:     parsing.Field[string]{Value: "BluRay"},
				Modifier:   parsing.Field[string]{Value: "REMUX"},
				IsRepack:   parsing.Field[bool]{Value: true},
				Version:    parsing.Field[int]{Value: 2},
			},
			Encode: model.EncodeFields{
				ReleaseGroup: parsing.Field[string]{Value: "FraMeSToR"},
			},
		},
		Media: model.MediaFields{
			Type:       "movie",
			Title:      "Dune",
			CleanTitle: "Dune",
			Year:       2021,
			TmdbID:     438631,
		},
	}
}

// importSubject is searchSubject after download: MediaInfo populated by
// ffprobe.
func importSubject() model.Subject {
	s := searchSubject()
	s.Release.MediaInfo = &model.MediaInfoFields{
		VideoCodec:     "H.265",
		VideoBitDepth:  10,
		HDR:            "HDR10",
		AudioLanguages: []string{"eng", "jpn"},
		Container:      "MKV",
	}
	return s
}
