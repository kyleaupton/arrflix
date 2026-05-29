package fakelibrary

// Real-world fixtures derived from an actual user's library. These contain
// NO embedded IDs — everything here relies on the matcher's name-parse
// resolver hitting TMDB search to resolve. Each fixture targets a specific
// naming pattern commonly found in the wild.

// ---------------------------------------------------------------------------
// Movie fixtures — real-world naming (no embedded IDs)
// ---------------------------------------------------------------------------

// RealWorldMoviesFlat contains movies that sit directly in the library root
// with no containing directory at all. The filename is the only source of metadata.
var RealWorldMoviesFlat = Library{
	Name: "realworld-movies-flat",
	Type: "movie",
	Files: []string{
		// Spaces, no parens around year
		"28 Years Later 2025 1080p WEB-DL HEVC x265 5.1 BONE.mkv",
		"A Minecraft Movie 2025 1080p WEB-DL HEVC x265 5.1 BONE.mkv",
		"Anora 2024 1080p WEB-DL HEVC x265 5.1 BONE.mkv",

		// Scene dots, no parens
		"A.Charlie.Brown.Christmas.1965.1080p.BluRay.DDP.5.1.H.265-EDGE2020.mkv",
		"Anchorman.The.Legend.of.Ron.Burgundy.2004.Unrated.1080p.BluRay.DDP.5.1.H.265-EDGE2020.mkv",
		"Bee.Movie.2007.1080p.BluRay.DDP.5.1.H.265-EDGE2020.mkv",

		// YTS bracket naming
		"Ant-Man.And.The.Wasp.Quantumania.2023.1080p.WEBRip.x264.AAC5.1-[YTS.MX].mp4",

		// Year in parens, foreign language tag
		"American Assassin (2017) 1080p h264 Ac3 5.1 Ita Eng Sub Ita Eng-MIRCrew.mkv",
	},
}

// RealWorldMoviesDir contains movies in a directory with various naming styles.
// The directory name is often the best source of title+year info.
var RealWorldMoviesDir = Library{
	Name: "realworld-movies-dir",
	Type: "movie",
	Files: []string{
		// Spaces in dir, no parens for year, filename matches dir
		"10 Cloverfield Lane 2016 1080p HDRip x264 AAC-JYK/10 Cloverfield Lane 2016 1080p HDRip x264 AAC-JYK.mkv",
		"A Dogs Way Home 2019 1080p BluRay x264-OFT/A Dogs Way Home 2019 1080p BluRay x264-OFT.mkv",
		"Against the Sun 2014 1080p BluRay x264 DTS-JYK/Against the Sun 2014 1080p BluRay x264 DTS-JYK.mkv",

		// Year in parens, clean dir name, scene filename
		"10 Things I Hate About You (1999) - 1080p/10.Things.I.Hate.About.You.1999.1080p.BluRay.x264.VPPV.mp4",
		"American Beauty (1999) [1080p]/American.Beauty.1999.1080p.BrRip.x264.BOKUTOX.YIFY.mp4",
		"50 First Dates (2004) [1080p]/50.First.Dates.2004.1080p.BrRip.x264.YIFY.mp4",

		// YTS bracket style dir
		"1917 (2019) [1080p] [WEBRip] [5.1] [YTS.MX]/1917.2019.1080p.WEBRip.x264.AAC5.1-[YTS.MX].mp4",
		"A House Of Dynamite (2025) [1080p] [WEBRip] [5.1] [YTS.MX]/A.House.Of.Dynamite.2025.1080p.WEBRip.x264.AAC5.1-[YTS.MX].mp4",

		// Tigole release — parens for year and quality info
		"21 (2008) (1080p BluRay x265 HEVC 10bit AAC 5.1 Tigole)/21 (2008) (1080p x265 10bit Tigole).mkv",

		// Scene dots in dir name, TGx tag
		"28.Days.Later.2002.1080p.BluRay.DDP5.1.x265.10bit-GalaxyRG265[TGx]/28.Days.Later.2002.1080p.BluRay.DDP5.1.x265.10bit-GalaxyRG265.mkv",
		"A.Beautiful.Mind.2001.1080p.NF.WEB-DL.H264-ETRG[EtHD]/A.Beautiful.Mind.2001.1080p.NF.WEB-DL.H264-ETRG[EtHD].mkv",

		// Inner file is generic "movie.mkv" — dir name is the only source
		"Anger.Management.2003.1080p.BluRay.x264.AC3-ETRG/Anger.Management.2003.1080p.BluRay.x264.AC3-ETRG.mp4",

		// Special character in title (!)
		"Airplane! 1980 1080p BluRay x264 AC3 - Ozlem/Airplane! 1980 1080p BluRay x264 AC3 - Ozlem.mp4",

		// Noise files that should be skipped
		"A Dogs Way Home 2019 1080p BluRay x264-OFT/A Dogs Way Home 2019 1080p BluRay x264-OFT.mkv.nfo",
		"1917 (2019) [1080p] [WEBRip] [5.1] [YTS.MX]/1917.2019.1080p.WEBRip.x264.AAC5.1-[YTS.MX].srt",
	},
}

// RealWorldMoviesNoise contains movies with sample/featurette files that the
// scanner needs to distinguish from the actual movie.
var RealWorldMoviesNoise = Library{
	Name: "realworld-movies-noise",
	Type: "movie",
	Files: []string{
		// Main movie + sample file
		"A.Bridge.Too.Far.1977.1080p.BluRay.x265.HEVC.EAC3-SARTRE/A.Bridge.Too.Far.1977.1080p.BluRay.x265.HEVC.EAC3-SARTRE.mkv",
		"A.Bridge.Too.Far.1977.1080p.BluRay.x265.HEVC.EAC3-SARTRE/sample.mkv",

		// Main movie + featurettes in subdir
		"21 (2008) (1080p BluRay x265 HEVC 10bit AAC 5.1 Tigole)/21 (2008) (1080p x265 10bit Tigole).mkv",
		"21 (2008) (1080p BluRay x265 HEVC 10bit AAC 5.1 Tigole)/Featurettes/Basic Strategy - A Complete Film Journal.mkv",
		"21 (2008) (1080p BluRay x265 HEVC 10bit AAC 5.1 Tigole)/Featurettes/Money Plays - A Tour of the Good Life.mkv",

		// Collection pack — multiple movies nested under one dir
		"Harry.Potter.Complete.Collection.2001-2011.1080p.BluRay.x264.DTS-ETRG/Chamber.Of.Secrets.2002.Extended.1080p.BluRay.x264.DTS-ETRG/Chamber.Of.Secrets.2002.Extended.1080p.BluRay.x264.DTS-ETRG.mkv",

		// Double-nested dir (same name repeated)
		"Grown Ups 2010 1080p BluRay x264-nikt0/Grown.Ups.2010.1080p.BluRay.x264-nikt0/Grown.Ups.2010.1080p.BluRay.x264-nikt0.mkv",
	},
}

// ---------------------------------------------------------------------------
// Series fixtures — real-world naming (no embedded IDs)
// ---------------------------------------------------------------------------

// RealWorldSeriesSeasonPack contains shows where episodes are inside a
// season-pack directory that includes quality/release info in the dir name.
// Pattern: Show/SeasonPack/File
var RealWorldSeriesSeasonPack = Library{
	Name: "realworld-series-season-pack",
	Type: "series",
	Files: []string{
		// "(year) Season N SNN (quality info)" pack naming
		"Abbott Elementary/Abbott Elementary (2021) Season 1 S01 (1080p HMAX WEB-DL x265 HEVC 10bit AC3 5.1 Silence)/Abbott Elementary (2021) - S01E01 - Pilot (1080p HMAX WEB-DL x265 Silence).mkv",
		"Abbott Elementary/Abbott Elementary (2021) Season 1 S01 (1080p HMAX WEB-DL x265 HEVC 10bit AC3 5.1 Silence)/Abbott Elementary (2021) - S01E02 - Light Bulb (1080p HMAX WEB-DL x265 Silence).mkv",
		"Abbott Elementary/Abbott Elementary (2021) Season 2 S02 (1080p AMZN WEB-DL x265 HEVC 10bit EAC3 5.1 Silence)/Abbott Elementary (2021) - S02E01 - Development Day (1080p AMZN WEB-DL x265 Silence).mkv",

		// "Season N S0N (quality)" with different release group
		"Arcane 2021/Arcane (2021) Season 1 S01 (1080p NF WEB-DL x265 HEVC 10bit EAC3 5.1 t3nzin)/Arcane (2021) - S01E01 - Welcome to the Playground (1080p NF WEB-DL x265 t3nzin).mkv",
		"Arcane 2021/Arcane (2021) Season 1 S01 (1080p NF WEB-DL x265 HEVC 10bit EAC3 5.1 t3nzin)/Arcane (2021) - S01E02 - Some Mysteries Are Better Left Unsolved (1080p NF WEB-DL x265 t3nzin).mkv",

		// Full season complete pack naming
		"Bob's Burgers/Bobs Burgers S14 Season 14 Complete [1080p WEB-DL DDP5 1 H 264]-NTb/Bobs Burgers S14E01 Fight at the Not Okay Chore-ral 1080p DSNP WEB-DL DDP5 1 H 264-NTb.mkv",
		"Bob's Burgers/Bobs Burgers S14 Season 14 Complete [1080p WEB-DL DDP5 1 H 264]-NTb/Bobs Burgers S14E02 The Amazing Rudy 1080p DSNP WEB-DL DDP5 1 H 264-NTb.mkv",
	},
}

// RealWorldSeriesScenePerEpisode contains shows where each episode is in its
// own scene release directory inside a season folder.
// Pattern: Show/Season/ReleaseDir/File
var RealWorldSeriesScenePerEpisode = Library{
	Name: "realworld-series-scene-per-episode",
	Type: "series",
	Files: []string{
		// Standard: Show/Season N/Scene.Release[TGx]/file
		"Abbott Elementary/Season 3/Abbott.Elementary.S03E01.1080p.WEB.h264-ETHEL[TGx]/Abbott.Elementary.S03E01.1080p.WEB.h264-ETHEL.mkv",
		"Abbott Elementary/Season 3/Abbott.Elementary.S03E01.1080p.WEB.h264-ETHEL[TGx]/abbott.elementary.s03e01.1080p.web.h264-ethel.nfo",
		"Abbott Elementary/Season 3/Abbott.Elementary.S03E03.1080p.WEB.H264-NHTFS[TGx]/abbott.elementary.s03e03.1080p.web.h264-nhtfs.mkv",
		"Abbott Elementary/Season 3/Abbott.Elementary.S03E05.1080p.WEB.H264-SuccessfulCrab[TGx]/abbott.elementary.s03e05.1080p.web.h264-successfulcrab.mkv",

		// x265 release
		"Abbott Elementary/Season 4/Abbott.Elementary.S04E01.1080p.HEVC.x265-MeGusta[TGx]/Abbott.Elementary.S04E01.1080p.HEVC.x265-MeGusta.mkv",
		"Abbott Elementary/Season 4/Abbott.Elementary.S04E02.1080p.x265-ELiTE/Abbott.Elementary.S04E02.1080p.x265-ELiTE.mkv",

		// Mixed with a flat file (no release dir) in the same season
		"Abbott Elementary/Season 3/abbott.elementary.s03e08.1080p.web.h264-successfulcrab[EZTVx.to].mkv",

		// Different show
		"American Dad!/Season 20/American.Dad.S20E01.1080p.HEVC.x265-MeGusta[TGx]/American.Dad.S20E01.1080p.HEVC.x265-MeGusta.mkv",
	},
}

// RealWorldSeriesFlat contains shows where episodes sit directly in the show
// directory with no season folder at all.
// Pattern: Show/File
var RealWorldSeriesFlat = Library{
	Name: "realworld-series-flat",
	Type: "series",
	Files: []string{
		// Scene dots, EZTVx tag, no season dir
		"American Manhunt Osama bin Laden/American.Manhunt.Osama.bin.Laden.S01E01.1080p.HEVC.x265-MeGusta[EZTVx.to].mkv",
		"American Manhunt Osama bin Laden/American.Manhunt.Osama.bin.Laden.S01E02.1080p.HEVC.x265-MeGusta[EZTVx.to].mkv",
		"American Manhunt Osama bin Laden/American.Manhunt.Osama.bin.Laden.S01E03.1080p.HEVC.x265-MeGusta[EZTVx.to].mkv",

		// Year in show name, similar pattern
		"American Nightmare/American.Nightmare.2024.S01E01.1080p.HEVC.x265-MeGusta[EZTVx.to].mkv",
		"American Nightmare/American.Nightmare.2024.S01E02.1080p.HEVC.x265-MeGusta[EZTVx.to].mkv",
	},
}

// RealWorldSeriesCustomSeasons contains shows using non-standard season dir
// names (e.g. "Book One" instead of "Season 01").
// Pattern: Show/CustomSeasonName/File
var RealWorldSeriesCustomSeasons = Library{
	Name: "realworld-series-custom-seasons",
	Type: "series",
	Files: []string{
		// "Book One - Water" instead of "Season 01"
		"Avatar - The Last Airbender (2005 - 2008) [1080p]/Book One - Water/Avatar - The Last Airbender - S01E01 - The Boy in the Iceberg.mkv",
		"Avatar - The Last Airbender (2005 - 2008) [1080p]/Book One - Water/Avatar - The Last Airbender - S01E02 - The Avatar Returns.mkv",
		"Avatar - The Last Airbender (2005 - 2008) [1080p]/Book One - Water/Avatar - The Last Airbender - S01E03 - The Southern Air Temple.mkv",
	},
}

// RealWorldSeriesMixed contains a single show with multiple naming conventions
// across different seasons — a common real-world scenario where users download
// from different sources over time.
var RealWorldSeriesMixed = Library{
	Name: "realworld-series-mixed",
	Type: "series",
	Files: []string{
		// Season 1-2: season pack naming (clean)
		"Abbott Elementary/Abbott Elementary (2021) Season 1 S01 (1080p HMAX WEB-DL x265 HEVC 10bit AC3 5.1 Silence)/Abbott Elementary (2021) - S01E01 - Pilot (1080p HMAX WEB-DL x265 Silence).mkv",
		"Abbott Elementary/Abbott Elementary (2021) Season 1 S01 (1080p HMAX WEB-DL x265 HEVC 10bit AC3 5.1 Silence)/Abbott Elementary (2021) - S01E02 - Light Bulb (1080p HMAX WEB-DL x265 Silence).mkv",
		"Abbott Elementary/Abbott Elementary (2021) Season 2 S02 (1080p AMZN WEB-DL x265 HEVC 10bit EAC3 5.1 Silence)/Abbott Elementary (2021) - S02E01 - Development Day (1080p AMZN WEB-DL x265 Silence).mkv",

		// Season 3: scene release dirs under "Season 3"
		"Abbott Elementary/Season 3/Abbott.Elementary.S03E01.1080p.WEB.h264-ETHEL[TGx]/Abbott.Elementary.S03E01.1080p.WEB.h264-ETHEL.mkv",
		"Abbott Elementary/Season 3/Abbott.Elementary.S03E03.1080p.WEB.H264-NHTFS[TGx]/abbott.elementary.s03e03.1080p.web.h264-nhtfs.mkv",
		// Season 3 also has a flat file mixed in
		"Abbott Elementary/Season 3/abbott.elementary.s03e08.1080p.web.h264-successfulcrab[EZTVx.to].mkv",

		// Season 4: different scene groups
		"Abbott Elementary/Season 4/Abbott.Elementary.S04E01.1080p.HEVC.x265-MeGusta[TGx]/Abbott.Elementary.S04E01.1080p.HEVC.x265-MeGusta.mkv",
		"Abbott Elementary/Season 4/Abbott.Elementary.S04E02.1080p.x265-ELiTE/Abbott.Elementary.S04E02.1080p.x265-ELiTE.mkv",
	},
}

// allRealWorld is every real-world fixture.
var allRealWorld = []Library{
	RealWorldMoviesFlat,
	RealWorldMoviesDir,
	RealWorldMoviesNoise,
	RealWorldSeriesSeasonPack,
	RealWorldSeriesScenePerEpisode,
	RealWorldSeriesFlat,
	RealWorldSeriesCustomSeasons,
	RealWorldSeriesMixed,
}
