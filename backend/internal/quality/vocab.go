package quality

import "github.com/kyleaupton/arrflix/internal/parsing"

// seriesBins is the Sonarr quality vocabulary, ported verbatim from
// submodules/sonarr/src/NzbDrone.Core/Qualities/Quality.cs. Ordering matches
// Sonarr's All list (Quality.cs:128-152) so default quality lists drawn from
// this table stay faithful to upstream.
//
// Sonarr's Quality is a (Id, Name, QualitySource, Resolution) tuple with no
// Modifier axis — REMUX is folded into the source value BlurayRaw (carried as
// the Bluray-{res} Remux Name), and RAWHD is folded into TelevisionRaw (carried
// as the Raw-HD Name). The orthogonal core our parsing emits uses BluRay / TV
// for both cases; the Modifier axis (REMUX, RAWHD) is what disambiguates here.
//
// "Unknown" is intentionally absent: it is the not-detected sentinel
// (Quality.cs:77 Quality.Unknown maps to BinFor returning the zero Bin), not a
// real bin a profile orders or gates.
var seriesBins = []Bin{
	// Quality.cs:78 — SDTV
	{Name: "SDTV", Source: parsing.SourceTV, Resolution: parsing.Res480p, Modifier: parsing.ModNone},
	// Quality.cs:79 — DVD
	{Name: "DVD", Source: parsing.SourceDVD, Resolution: parsing.Res480p, Modifier: parsing.ModNone},
	// Quality.cs:90 — WEBRip-480p
	{Name: "WEBRip-480p", Source: parsing.SourceWEBRip, Resolution: parsing.Res480p, Modifier: parsing.ModNone},
	// Quality.cs:85 — WEBDL-480p
	{Name: "WEBDL-480p", Source: parsing.SourceWEBDL, Resolution: parsing.Res480p, Modifier: parsing.ModNone},
	// Quality.cs:95 — Bluray-480p
	{Name: "Bluray-480p", Source: parsing.SourceBluRay, Resolution: parsing.Res480p, Modifier: parsing.ModNone},
	// Quality.cs:100 — Bluray-576p
	{Name: "Bluray-576p", Source: parsing.SourceBluRay, Resolution: parsing.Res576p, Modifier: parsing.ModNone},
	// Quality.cs:81 — HDTV-720p
	{Name: "HDTV-720p", Source: parsing.SourceTV, Resolution: parsing.Res720p, Modifier: parsing.ModNone},
	// Quality.cs:105 — WEBRip-720p
	{Name: "WEBRip-720p", Source: parsing.SourceWEBRip, Resolution: parsing.Res720p, Modifier: parsing.ModNone},
	// Quality.cs:82 — WEBDL-720p
	{Name: "WEBDL-720p", Source: parsing.SourceWEBDL, Resolution: parsing.Res720p, Modifier: parsing.ModNone},
	// Quality.cs:83 — Bluray-720p
	{Name: "Bluray-720p", Source: parsing.SourceBluRay, Resolution: parsing.Res720p, Modifier: parsing.ModNone},
	// Quality.cs:84 — Bluray-1080p
	{Name: "Bluray-1080p", Source: parsing.SourceBluRay, Resolution: parsing.Res1080p, Modifier: parsing.ModNone},
	// Quality.cs:86 — HDTV-1080p
	{Name: "HDTV-1080p", Source: parsing.SourceTV, Resolution: parsing.Res1080p, Modifier: parsing.ModNone},
	// Quality.cs:110 — WEBRip-1080p
	{Name: "WEBRip-1080p", Source: parsing.SourceWEBRip, Resolution: parsing.Res1080p, Modifier: parsing.ModNone},
	// Quality.cs:80 — WEBDL-1080p
	{Name: "WEBDL-1080p", Source: parsing.SourceWEBDL, Resolution: parsing.Res1080p, Modifier: parsing.ModNone},
	// Quality.cs:87 — Raw-HD (QualitySource.TelevisionRaw, resolution 1080).
	// Orthogonal core: (TV, _, RAWHD). The resolution carried here matches
	// upstream's nominal value but the bin is resolution-agnostic in practice.
	{Name: "Raw-HD", Source: parsing.SourceTV, Resolution: parsing.Res1080p, Modifier: parsing.ModRawHD},
	// Quality.cs:115 — HDTV-2160p
	{Name: "HDTV-2160p", Source: parsing.SourceTV, Resolution: parsing.Res2160p, Modifier: parsing.ModNone},
	// Quality.cs:116 — WEBRip-2160p
	{Name: "WEBRip-2160p", Source: parsing.SourceWEBRip, Resolution: parsing.Res2160p, Modifier: parsing.ModNone},
	// Quality.cs:121 — WEBDL-2160p
	{Name: "WEBDL-2160p", Source: parsing.SourceWEBDL, Resolution: parsing.Res2160p, Modifier: parsing.ModNone},
	// Quality.cs:122 — Bluray-2160p
	{Name: "Bluray-2160p", Source: parsing.SourceBluRay, Resolution: parsing.Res2160p, Modifier: parsing.ModNone},
	// Quality.cs:123 — Bluray-1080p Remux (QualitySource.BlurayRaw in Sonarr;
	// orthogonal core (BluRay, 1080p, REMUX)).
	{Name: "Bluray-1080p Remux", Source: parsing.SourceBluRay, Resolution: parsing.Res1080p, Modifier: parsing.ModRemux},
	// Quality.cs:124 — Bluray-2160p Remux (QualitySource.BlurayRaw; orthogonal
	// core (BluRay, 2160p, REMUX)).
	{Name: "Bluray-2160p Remux", Source: parsing.SourceBluRay, Resolution: parsing.Res2160p, Modifier: parsing.ModRemux},
}

// movieBins is the Radarr quality vocabulary, ported verbatim from
// submodules/radarr/src/NzbDrone.Core/Qualities/Quality.cs. Ordering matches
// Radarr's All list (Quality.cs:128-160) so default quality lists drawn from
// this table stay faithful to upstream.
//
// Radarr's Quality is a (Id, Name, QualitySource, Resolution, Modifier) tuple
// with the modifier axis carried directly — Remux-1080p, BR-DISK, DVD-R, and
// Raw-HD are all top-level bins with explicit Modifier values. The pre-release
// sources (WORKPRINT/CAM/TELESYNC/TELECINE/DVDSCR/REGIONAL) are first-class
// bins here; Sonarr has no analogue. DVDSCR (Modifier.SCREENER) and REGIONAL
// (Modifier.REGIONAL) carry the modifier values from
// submodules/radarr/.../Modifier.cs.
//
// "Unknown" is intentionally absent for the same reason as seriesBins.
var movieBins = []Bin{
	// Quality.cs:83 — WORKPRINT (QualitySource.WORKPRINT, resolution 0).
	{Name: "WORKPRINT", Source: parsing.SourceWorkprint, Resolution: parsing.ResUnknown, Modifier: parsing.ModNone},
	// Quality.cs:84 — CAM
	{Name: "CAM", Source: parsing.SourceCAM, Resolution: parsing.ResUnknown, Modifier: parsing.ModNone},
	// Quality.cs:85 — TELESYNC
	{Name: "TELESYNC", Source: parsing.SourceTelesync, Resolution: parsing.ResUnknown, Modifier: parsing.ModNone},
	// Quality.cs:86 — TELECINE
	{Name: "TELECINE", Source: parsing.SourceTelecine, Resolution: parsing.ResUnknown, Modifier: parsing.ModNone},
	// Quality.cs:87 — DVDSCR (Modifier.SCREENER; deferred in parsing)
	{Name: "DVDSCR", Source: parsing.SourceDVD, Resolution: parsing.Res480p, Modifier: parsing.ModScreener},
	// Quality.cs:88 — REGIONAL (Modifier.REGIONAL; deferred in parsing)
	{Name: "REGIONAL", Source: parsing.SourceDVD, Resolution: parsing.Res480p, Modifier: parsing.ModRegional},
	// Quality.cs:91 — SDTV
	{Name: "SDTV", Source: parsing.SourceTV, Resolution: parsing.Res480p, Modifier: parsing.ModNone},
	// Quality.cs:92 — DVD (resolution 0 in Radarr; the plain-DVD bin is
	// resolution-agnostic, see engine.findBySourceAndResolution).
	{Name: "DVD", Source: parsing.SourceDVD, Resolution: parsing.ResUnknown, Modifier: parsing.ModNone},
	// Quality.cs:93 — DVD-R (Modifier.REMUX at DVD/480p).
	{Name: "DVD-R", Source: parsing.SourceDVD, Resolution: parsing.Res480p, Modifier: parsing.ModRemux},
	// Quality.cs:96 — HDTV-720p
	{Name: "HDTV-720p", Source: parsing.SourceTV, Resolution: parsing.Res720p, Modifier: parsing.ModNone},
	// Quality.cs:97 — HDTV-1080p
	{Name: "HDTV-1080p", Source: parsing.SourceTV, Resolution: parsing.Res1080p, Modifier: parsing.ModNone},
	// Quality.cs:98 — HDTV-2160p
	{Name: "HDTV-2160p", Source: parsing.SourceTV, Resolution: parsing.Res2160p, Modifier: parsing.ModNone},
	// Quality.cs:101 — WEBDL-480p
	{Name: "WEBDL-480p", Source: parsing.SourceWEBDL, Resolution: parsing.Res480p, Modifier: parsing.ModNone},
	// Quality.cs:102 — WEBDL-720p
	{Name: "WEBDL-720p", Source: parsing.SourceWEBDL, Resolution: parsing.Res720p, Modifier: parsing.ModNone},
	// Quality.cs:103 — WEBDL-1080p
	{Name: "WEBDL-1080p", Source: parsing.SourceWEBDL, Resolution: parsing.Res1080p, Modifier: parsing.ModNone},
	// Quality.cs:104 — WEBDL-2160p
	{Name: "WEBDL-2160p", Source: parsing.SourceWEBDL, Resolution: parsing.Res2160p, Modifier: parsing.ModNone},
	// Quality.cs:121 — WEBRip-480p
	{Name: "WEBRip-480p", Source: parsing.SourceWEBRip, Resolution: parsing.Res480p, Modifier: parsing.ModNone},
	// Quality.cs:122 — WEBRip-720p
	{Name: "WEBRip-720p", Source: parsing.SourceWEBRip, Resolution: parsing.Res720p, Modifier: parsing.ModNone},
	// Quality.cs:123 — WEBRip-1080p
	{Name: "WEBRip-1080p", Source: parsing.SourceWEBRip, Resolution: parsing.Res1080p, Modifier: parsing.ModNone},
	// Quality.cs:124 — WEBRip-2160p
	{Name: "WEBRip-2160p", Source: parsing.SourceWEBRip, Resolution: parsing.Res2160p, Modifier: parsing.ModNone},
	// Quality.cs:107 — Bluray-480p
	{Name: "Bluray-480p", Source: parsing.SourceBluRay, Resolution: parsing.Res480p, Modifier: parsing.ModNone},
	// Quality.cs:108 — Bluray-576p
	{Name: "Bluray-576p", Source: parsing.SourceBluRay, Resolution: parsing.Res576p, Modifier: parsing.ModNone},
	// Quality.cs:109 — Bluray-720p
	{Name: "Bluray-720p", Source: parsing.SourceBluRay, Resolution: parsing.Res720p, Modifier: parsing.ModNone},
	// Quality.cs:110 — Bluray-1080p
	{Name: "Bluray-1080p", Source: parsing.SourceBluRay, Resolution: parsing.Res1080p, Modifier: parsing.ModNone},
	// Quality.cs:111 — Bluray-2160p
	{Name: "Bluray-2160p", Source: parsing.SourceBluRay, Resolution: parsing.Res2160p, Modifier: parsing.ModNone},
	// Quality.cs:113 — Remux-1080p (Modifier.REMUX at BLURAY/1080p)
	{Name: "Remux-1080p", Source: parsing.SourceBluRay, Resolution: parsing.Res1080p, Modifier: parsing.ModRemux},
	// Quality.cs:114 — Remux-2160p (Modifier.REMUX at BLURAY/2160p)
	{Name: "Remux-2160p", Source: parsing.SourceBluRay, Resolution: parsing.Res2160p, Modifier: parsing.ModRemux},
	// Quality.cs:116 — BR-DISK (Modifier.BRDISK at BLURAY/1080p)
	{Name: "BR-DISK", Source: parsing.SourceBluRay, Resolution: parsing.Res1080p, Modifier: parsing.ModBRDisk},
	// Quality.cs:119 — Raw-HD (Modifier.RAWHD at TV/1080p)
	{Name: "Raw-HD", Source: parsing.SourceTV, Resolution: parsing.Res1080p, Modifier: parsing.ModRawHD},
}
