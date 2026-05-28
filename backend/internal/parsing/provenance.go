package parsing

// provenance.go owns the type-erased per-field provenance map exposed via
// ParsedRelease.Provenance(). The map mirrors the Values() layout key-for-key
// and is what powers the Parse Inspector and the parity harness; keeping it
// separate from types.go keeps the data model in types.go uncluttered by the
// mechanical key→Field[any] mapping.

// Provenance returns the per-field confidence and evidence, keyed by a dotted
// path that matches the Values layout ("quality.source", "identity.title",
// "identity.numbering.season"). It powers the Parse Inspector and the parity
// harness; Numbering.Kind is a discriminant, not a parsed field, so it is
// absent. Values are type-erased to Field[any].
func (p ParsedRelease) Provenance() map[string]Field[any] {
	return map[string]Field[any]{
		"identity.title":                       eraseField(p.Identity.Title),
		"identity.year":                        eraseField(p.Identity.Year),
		"identity.type_hint":                   eraseField(p.Identity.TypeHint),
		"identity.edition":                     eraseField(p.Identity.Edition),
		"identity.all_titles":                  eraseField(p.Identity.AllTitles),
		"identity.numbering.season":            eraseField(p.Identity.Numbering.Season),
		"identity.numbering.episode_numbers":   eraseField(p.Identity.Numbering.EpisodeNumbers),
		"identity.numbering.absolute_numbers":  eraseField(p.Identity.Numbering.AbsoluteNumbers),
		"identity.numbering.air_date":          eraseField(p.Identity.Numbering.AirDate),
		"identity.numbering.full_season":       eraseField(p.Identity.Numbering.FullSeason),
		"identity.numbering.season_part":       eraseField(p.Identity.Numbering.SeasonPart),
		"identity.numbering.is_partial_season": eraseField(p.Identity.Numbering.IsPartialSeason),
		"identity.numbering.is_multi_season":   eraseField(p.Identity.Numbering.IsMultiSeason),
		"identity.numbering.is_mini_series":    eraseField(p.Identity.Numbering.IsMiniSeries),
		"identity.numbering.special":           eraseField(p.Identity.Numbering.Special),
		"identity.numbering.is_split_episode":  eraseField(p.Identity.Numbering.IsSplitEpisode),
		"identity.numbering.is_season_extra":   eraseField(p.Identity.Numbering.IsSeasonExtra),
		"identity.numbering.daily_part":        eraseField(p.Identity.Numbering.DailyPart),
		"identity.numbering.special_absolute":  eraseField(p.Identity.Numbering.SpecialAbsolute),
		"quality.name":                         eraseField(p.Quality.Name),
		"quality.full":                         eraseField(p.Quality.Full),
		"quality.resolution":                   eraseField(p.Quality.Resolution),
		"quality.source":                       eraseField(p.Quality.Source),
		"quality.modifier":                     eraseField(p.Quality.Modifier),
		"quality.is_repack":                    eraseField(p.Quality.IsRepack),
		"quality.version":                      eraseField(p.Quality.Version),
		"quality.real":                         eraseField(p.Quality.Real),
		"release.release_group":                eraseField(p.Release.ReleaseGroup),
		"release.codec":                        eraseField(p.Release.Codec),
		"release.audio_format":                 eraseField(p.Release.AudioFormat),
		"release.audio_channels":               eraseField(p.Release.AudioChannels),
		"release.hdr":                          eraseField(p.Release.HDR),
		"release.dual_audio":                   eraseField(p.Release.DualAudio),
		"release.languages":                    eraseField(p.Release.Languages),
		"release.release_hash":                 eraseField(p.Release.ReleaseHash),
		"release.hardcoded_subs":               eraseField(p.Release.HardcodedSubs),
	}
}

// eraseField converts a typed Field[T] to Field[any] for the Provenance map.
func eraseField[T any](f Field[T]) Field[any] {
	return Field[any]{Value: f.Value, Confidence: f.Confidence, Evidence: f.Evidence}
}
