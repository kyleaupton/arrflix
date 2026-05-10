package release

import (
	"testing"
)

func TestQualityMethods(t *testing.T) {
	tests := []struct {
		name       string
		quality    Quality
		source     string
		resolution string
		isRemux    bool
	}{
		{"Bluray1080p", Bluray1080p, "BluRay", "1080p", false},
		{"Bluray1080pRemux", Bluray1080pRemux, "BluRay", "1080p", true},
		{"WEBDL2160p", WEBDL2160p, "WEB-DL", "2160p", false},
		{"HDTV720p", HDTV720p, "HDTV", "720p", false},
		{"SDTV", SDTV, "SDTV", "SD", false},
		{"DVD", DVD, "DVD", "SD", false},
		{"Unknown", Unknown, "Unknown", "Unknown", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.quality.Source(); got != tt.source {
				t.Errorf("Quality.Source() = %v, want %v", got, tt.source)
			}
			if got := tt.quality.Resolution(); got != tt.resolution {
				t.Errorf("Quality.Resolution() = %v, want %v", got, tt.resolution)
			}
			if got := tt.quality.IsRemux(); got != tt.isRemux {
				t.Errorf("Quality.IsRemux() = %v, want %v", got, tt.isRemux)
			}
		})
	}
}

// TestParseQuality tests the quality parser against expected values
// These expected values are derived from Sonarr's QualityParser behavior
func TestParse(t *testing.T) {
	tests := []struct {
		title    string
		expected Quality
	}{
		// ===== SDTV =====
		{"S07E23 .avi", SDTV},
		{"The.Series.S01E13.x264-CtrlSD", SDTV},
		{"The Series S02E01 HDTV XviD 2HD", SDTV},
		{"The Series S05E11 PROPER HDTV XviD 2HD", SDTV},
		{"The Series Show S02E08 HDTV x264 FTP", SDTV},
		{"The.Series.2011.S02E01.WS.PDTV.x264-TLA", SDTV},
		{"The.Series.2011.S02E01.WS.PDTV.x264-REPACK-TLA", SDTV},
		{"The Series S01E04 DSR x264 2HD", SDTV},
		{"The Series S01E04 Series Death Train DSR x264 MiNDTHEGAP", SDTV},
		{"The Series S11E03 has no periods or extension HDTV", SDTV},
		{"The.Series.S04E05.HDTV.XviD-LOL", SDTV},
		{"The.Series.S02E15.avi", SDTV},
		{"The.Series.S02E15.xvid", SDTV},
		{"The.Series.S02E15.divx", SDTV},
		{"The.Series.S03E06.HDTV-WiDE", SDTV},
		{"Series.S10E27.WS.DSR.XviD-2HD", SDTV},
		{"[HorribleSubs] The Series - 32 [480p]", SDTV},
		{"[CR] The Series - 004 [480p][48CE2D0F]", SDTV},
		{"[Hatsuyuki] The Series - 363 [848x480][ADE35E38]", SDTV},
		{"The.Series.S03.TVRip.XviD-NOGRP", SDTV},
		{"[HorribleSubs] The Series - 03 [360p].mkv", SDTV},

		// 540p without source returns Unknown (Sonarr parity)
		{"[SubsPlease] Series Title (540p) [AB649D32].mkv", Unknown},
		{"[Erai-raws] Series Title [540p][Multiple Subtitle].mkv", Unknown},

		// ===== DVD =====
		{"The.Series.S01E13.NTSC.x264-CtrlSD", DVD},
		{"The.Series.S03E06.DVDRip.XviD-WiDE", DVD},
		{"The.Series.S03E06.DVD.Rip.XviD-WiDE", DVD},
		{"the.Series.1x13.circles.ws.xvidvd-tns", DVD},
		{"the_Series.9x18.sunshine_days.ac3.ws_dvdrip_xvid-fov.avi", DVD},
		{"[FroZen] Series - 23 [DVD][7F6170E6]", DVD},
		{"[AniDL] Series - 26 -[360p][DVD][D - A][Exiled - Destiny]", DVD},

		// ===== WEBDL-480p =====
		{"The.Series.S01E10.The.Leviathan.480p.WEB-DL.x264-mSD", WEBDL480p},
		{"The.Series.S04E10.Glee.Actually.480p.WEB-DL.x264-mSD", WEBDL480p},
		{"The.SeriesS06E11.The.Santa.Simulation.480p.WEB-DL.x264-mSD", WEBDL480p},
		{"The.Series.S02E04.480p.WEB.DL.nSD.x264-NhaNc3", WEBDL480p},
		{"The.Series.S01E08.Das.geloeschte.Ich.German.Dubbed.DL.AmazonHD.x264-TVS", WEBDL480p},
		{"The.Series.S01E04.Rod.Trip.mit.meinem.Onkel.German.DL.NetflixUHD.x264", WEBDL480p},
		{"[HorribleSubs] Series Title! S01 [Web][MKV][h264][480p][AAC 2.0][Softsubs (HorribleSubs)]", WEBDL480p},
		{"Series.Title.S13E11.Ausgebacken.German.AmazonSD.h264-4SF", WEBDL480p},

		// ===== Bluray-480p =====
		{"SERIES.S03E01-06.DUAL.XviD.Bluray.AC3-REPACK.-HELLYWOOD.avi", Bluray480p},
		{"SERIES.S03E01-06.DUAL.BDRip.XviD.AC3.-HELLYWOOD", Bluray480p},
		{"SERIES.S03E01-06.DUAL.BDRip.X-viD.AC3.-HELLYWOOD", Bluray480p},
		{"SERIES.S03E01-06.DUAL.BDRip.AC3.-HELLYWOOD", Bluray480p},
		{"SERIES.S03E01-06.DUAL.BDRip.XviD.AC3.-HELLYWOOD.avi", Bluray480p},
		{"SERIES.S03E01-06.DUAL.XviD.Bluray.AC3.-HELLYWOOD.avi", Bluray480p},
		{"The.Series.S01E05.480p.BluRay.DD5.1.x264-HiSD", Bluray480p},
		{"The Series (BD)(640x480(RAW) (BATCH 1) (1-13)", Bluray480p},
		{"[Doki] Series - 02 (848x480 XviD BD MP3) [95360783]", Bluray480p},
		{"Adventures.of.Sonic.the.Hedgehog.S01.BluRay.480i.DD.2.0.AVC.REMUX-FraMeSToR", Bluray480p},
		{"Adventures.of.Sonic.the.Hedgehog.S01E01.Best.Hedgehog.480i.DD.2.0.AVC.REMUX-FraMeSToR", Bluray480p},

		// ===== WEBRip-480p =====
		{"The.Series.S02E10.480p.HULU.WEBRip.x264-Puffin", WEBRip480p},
		{"The.Series.S10E14.Techs.And.Balances.480p.AE.WEBRip.AAC2.0.x264-SEA", WEBRip480p},
		{"Series.Title.1x04.ITA.WEBMux.x264-NovaRip", WEBRip480p},

		// ===== Bluray-576p =====
		{"The.Series.S01E05.576p.BluRay.DD5.1.x264-HiSD", Bluray576p},

		// ===== HDTV-720p =====
		{"Series - S01E01 - Title [HDTV]", HDTV720p},
		{"Series - S01E01 - Title [HDTV-720p]", HDTV720p},
		{"The Series S04E87 REPACK 720p HDTV x264 aAF", HDTV720p},
		{"The.Series.S02E15.720p", HDTV720p},
		{"S07E23 - [HDTV-720p].mkv", HDTV720p},
		{"Series - S22E03 - MoneyBART - HD TV.mkv", HDTV720p},
		{"S07E23.mkv", HDTV720p},
		{"The.Series.S08E05.720p.HDTV.X264-DIMENSION", HDTV720p},
		{"The.Series.S02E15.mkv", HDTV720p},
		{"The.Series.S01E08.Tourmaline.Nepal.720p.HDTV.x264-DHD", HDTV720p},
		{"[Underwater-FFF] The Series - 01 (720p) [27AAA0A0]", HDTV720p},
		{"[Doki] The Series - 07 (1280x720 Hi10P AAC) [80AF7DDE]", HDTV720p},
		{"[Doremi].The.Series.5.Go.Go!.31.[1280x720].[C65D4B1F].mkv", HDTV720p},
		{"[HorribleSubs]_Series_Title_-_145_[720p]", HDTV720p},
		{"[Eveyuu] Series Title - 10 [Hi10P 1280x720 H264][10B23BD8]", HDTV720p},
		{"The.Series.US.S12E17.HR.WS.PDTV.X264-DIMENSION", HDTV720p},
		{"The Series S01E07 - Motor zmen (CZ)[TvRip][HEVC][720p]", HDTV720p},
		{"The.Series.S05E06.720p.HDTV.x264-FHD", HDTV720p},
		{"Series.Title.1x01.ITA.720p.x264-RlsGrp [01/54] - \"series.title.1x01.ita.720p.x264-rlsgrp.nfo\"", HDTV720p},
		{"[TMS-Remux].Series.Title.X.21.720p.[76EA1C53].mkv", HDTV720p},

		// ===== HDTV-1080p =====
		{"Under the Series S01E10 Let the Sonarr Begin 1080p", HDTV1080p},
		{"Series.S07E01.ARE.YOU.1080P.HDTV.X264-QCF", HDTV1080p},
		{"Series.S07E01.ARE.YOU.1080P.HDTV.x264-QCF", HDTV1080p},
		{"Series.S07E01.ARE.YOU.1080P.HDTV.proper.X264-QCF", HDTV1080p},
		{"Series - S01E01 - Title [HDTV-1080p]", HDTV1080p},
		{"[HorribleSubs] Series Title - 32 [1080p]", HDTV1080p},
		{"Series S01E07 - Sonarr zmen (CZ)[TvRip][HEVC][1080p]", HDTV1080p},
		{"The Online Series Alicization 04 vostfr FHD", HDTV1080p},
		{"Series Slayer 04 vostfr FHD.mkv", HDTV1080p},
		{"[Onii-ChanSub] The.Series - 02 vostfr (FHD 1080p 10bits).mkv", HDTV1080p},
		{"[Miaou] Series Title 02 VOSTFR FHD 10 bits", HDTV1080p},
		{"[mhastream.com]_Episode_05_FHD.mp4", HDTV1080p},
		{"[Kousei]_One_Series_ - _609_[FHD][648A87C7].mp4", HDTV1080p},
		{"Series culpable 1x02 Culpabilidad [HDTV 1080i AVC MP2 2.0 Sub][GrupoHDS]", HDTV1080p},
		{"Series como paso - 19x15 [344] Cuarenta anos de baile [HDTV 1080i AVC MP2 2.0 Sub][GrupoHDS]", HDTV1080p},
		{"Super.Seires.Go.S01E02.Depths.of.Sonarr.1080i.HDTV.DD5.1.H.264-NOGRP", HDTV1080p},

		// ===== HDTV-2160p =====
		{"My Title - S01E01 - EpTitle [HEVC 4k DTSHD-MA-6ch]", HDTV2160p},
		{"My Title - S01E01 - EpTitle [HEVC-4k DTSHD-MA-6ch]", HDTV2160p},
		{"My Title - S01E01 - EpTitle [4k HEVC DTSHD-MA-6ch]", HDTV2160p},

		// ===== WEBDL-720p =====
		{"Series S01E04 Mexicos Death Train 720p WEB DL", WEBDL720p},
		{"Series Five 0 S02E21 720p WEB DL DD5 1 H 264", WEBDL720p},
		{"Series S04E22 720p WEB DL DD5 1 H 264 NFHD", WEBDL720p},
		{"Series - S11E06 - D-Yikes! - 720p WEB-DL.mkv", WEBDL720p},
		{"The.Series.S02E15.720p.WEB-DL.DD5.1.H.264-SURFER", WEBDL720p},
		{"S07E23 - [WEBDL].mkv", WEBDL720p},
		{"Series S04E22 720p WEB-DL DD5.1 H264-EbP.mkv", WEBDL720p},
		{"Series.S04.720p.Web-Dl.Dd5.1.h264-P2PACK", WEBDL720p},
		{"Da.Series.Shows.S02E04.720p.WEB.DL.nSD.x264-NhaNc3", WEBDL720p},
		{"Series.Miami.S04E25.720p.iTunesHD.AVC-TVS", WEBDL720p},
		{"Series.S06E23.720p.WebHD.h264-euHD", WEBDL720p},
		{"Series.Title.2016.03.14.720p.WEB.x264-spamTV", WEBDL720p},
		{"Series.Title.2016.03.14.720p.WEB.h264-spamTV", WEBDL720p},
		{"Series.S01E08.Das.geloeschte.Ich.German.DD51.Dubbed.DL.720p.AmazonHD.x264-TVS", WEBDL720p},
		{"Series.Polo.S01E11.One.Hundred.Sonarrs.2015.German.DD51.DL.720p.NetflixUHD.x264.NewUp.by.Wunschtante", WEBDL720p},
		{"Series 2016 German DD51 DL 720p NetflixHD x264-TVS", WEBDL720p},
		{"Series.6x10.Basic.Sonarr.Repair.and.Replace.ITA.ENG.720p.WEB-DLMux.H.264-GiuseppeTnT", WEBDL720p},
		{"Series.6x11.Modern.Spy.ITA.ENG.720p.WEB.DLMux.H.264-GiuseppeTnT", WEBDL720p},
		{"The Series Was Dead 2010 S09E13 [MKV / H.264 / AC3/AAC / WEB / Dual Audio / Ingles / 720p]", WEBDL720p},
		{"into.the.Series.s03e16.h264.720p-web-handbrake.mkv", WEBDL720p},
		{"Series.S01E01.The.Sonarr.Principle.720p.WEB-DL.DD5.1.H.264-BD", WEBDL720p},
		{"Series.S03E05.Griebnitzsee.German.720p.MaxdomeHD.AVC-TVS", WEBDL720p},
		{"[HorribleSubs] Series Title! S01 [Web][MKV][h264][720p][AAC 2.0][Softsubs (HorribleSubs)]", WEBDL720p},
		{"[HorribleSubs] Series Title! S01 [Web][MKV][h264][AAC 2.0][Softsubs (HorribleSubs)]", WEBDL720p},
		{"Series.Title.S04E13.960p.WEB-DL.AAC2.0.H.264-squalor", WEBDL720p},
		{"Series.Title.S16.DP.WEB.720p.DDP.5.1.H.264.PLEX", WEBDL720p},
		{"Series.Title.S01E01.Erste.Begegnungen.German.DD51.Synced.DL.720p.HBOMaxHD.AVC-TVS", WEBDL720p},
		{"Series.Title.S01E05.Tavora.greift.an.German.DL.720p.DisneyHD.h264-4SF", WEBDL720p},

		// ===== WEBRip-720p =====
		{"Series.Title.S04E01.720p.WEBRip.AAC2.0.x264-NFRiP", WEBRip720p},
		{"Series.Title.S01E07.A.Prayer.For.Mad.Sweeney.720p.AMZN.WEBRip.DD5.1.x264-NTb", WEBRip720p},
		{"Series.Title.S07E01.A.New.Home.720p.DSNY.WEBRip.AAC2.0.x264-TVSmash", WEBRip720p},
		{"Series.Title.1x04.ITA.720p.WEBMux.x264-NovaRip", WEBRip720p},

		// ===== WEBDL-1080p =====
		{"Series S09E03 1080p WEB DL DD5 1 H264 NFHD", WEBDL1080p},
		{"Two and a Half Developers of the Series S10E03 1080p WEB DL DD5 1 H 264 NFHD", WEBDL1080p},
		{"Series.S08E01.1080p.WEB-DL.DD5.1.H264-NFHD", WEBDL1080p},
		{"Its.Always.Sonarrs.Fault.S08E01.1080p.WEB-DL.proper.AAC2.0.H.264", WEBDL1080p},
		{"This is an Easter Egg S10E03 1080p WEB DL DD5 1 H 264 REPACK NFHD", WEBDL1080p},
		{"Series.S04E09.Swan.Song.1080p.WEB-DL.DD5.1.H.264-ECI", WEBDL1080p},
		{"The.Big.Easter.Theory.S06E11.The.Sonarr.Simulation.1080p.WEB-DL.DD5.1.H.264", WEBDL1080p},
		{"Sonarr's.Baby.S01E02.Night.2.[WEBDL-1080p].mkv", WEBDL1080p},
		{"Series.Title.2016.03.14.1080p.WEB.x264-spamTV", WEBDL1080p},
		{"Series.Title.2016.03.14.1080p.WEB.h264-spamTV", WEBDL1080p},
		{"Series.S01.1080p.WEB-DL.AAC2.0.AVC-TrollHD", WEBDL1080p},
		{"Series Title S06E08 1080p WEB h264-EXCLUSIVE", WEBDL1080p},
		{"Series Title S06E08 No One PROPER 1080p WEB DD5 1 H 264-EXCLUSIVE", WEBDL1080p},
		{"Series Title S06E08 No One PROPER 1080p WEB H 264-EXCLUSIVE", WEBDL1080p},
		{"The.Series.S25E21.Pay.No1.1080p.WEB-DL.DD5.1.H.264-NTb", WEBDL1080p},
		{"Series.S01E08.Das.geloeschte.Ich.German.DD51.Dubbed.DL.1080p.AmazonHD.x264-TVS", WEBDL1080p},
		{"Death.Series.2017.German.DD51.DL.1080p.NetflixHD.x264-TVS", WEBDL1080p},
		{"Series.S01E08.Pro.Gamer.1440p.BKPL.WEB-DL.H.264-LiGHT", WEBDL1080p},
		{"Series.Title.S04E11.Teddy's.Choice.FHD.1080p.Web-DL", WEBDL1080p},
		{"Series.S04E03.The.False.Bride.1080p.NF.WEB.DDP5.1.x264-NTb[rartv]", WEBDL1080p},
		{"Series.Title.S02E02.This.Year.Will.Be.Different.1080p.AMZN.WEB...", WEBDL1080p},
		{"Series.Title.S02E02.This.Year.Will.Be.Different.1080p.AMZN.WEB.", WEBDL1080p},
		{"Series Title - S01E11 2020 1080p Viva MKV WEB", WEBDL1080p},
		{"[HorribleSubs] Series Title! S01 [Web][MKV][h264][1080p][AAC 2.0][Softsubs (HorribleSubs)]", WEBDL1080p},
		{"[LostYears] Series Title - 01-17 (WEB 1080p x264 10-bit AAC) [Dual-Audio]", WEBDL1080p},
		{"Series.and.Titles.S01.1080p.NF.WEB.DD2.0.x264-SNEAkY", WEBDL1080p},
		{"Series.Title.S02E02.This.Year.Will.Be.Different.1080p.WEB.H 265", WEBDL1080p},
		{"Series Title Season 2 [WEB 1080p HEVC Opus] [Netaro]", WEBDL1080p},
		{"Series Title Season 2 (WEB 1080p HEVC Opus) [Netaro]", WEBDL1080p},
		{"Series.Title.S01E01.Erste.Begegnungen.German.DD51.Synced.DL.1080p.HBOMaxHD.AVC-TVS", WEBDL1080p},
		{"Series.Title.S01E05.Tavora.greift.an.German.DL.1080p.DisneyHD.h264-4SF", WEBDL1080p},
		{"Series.Title.S02E04.German.Dubbed.DL.AAC.1080p.WEB.AVC-GROUP", WEBDL1080p},

		// ===== WEBRip-1080p =====
		{"Series.Title.S04E01.iNTERNAL.1080p.WEBRip.x264-QRUS", WEBRip1080p},
		{"Series.Title.S07E20.1080p.AMZN.WEBRip.DDP5.1.x264-ViSUM ac3.(NLsub)", WEBRip1080p},
		{"Series.Title.S03E09.1080p.NF.WEBRip.DD5.1.x264-ViSUM", WEBRip1080p},
		{"The Series 42 S09E13 1.54 GB WEB-RIP 1080p Dual-Audio 2019 MKV", WEBRip1080p},
		{"Series.Title.1x04.ITA.1080p.WEBMux.x264-NovaRip", WEBRip1080p},
		{"Series.Title.2019.S02E07.Chapter.15.The.Believer.4Kto1080p.DSNYP.Webrip.x265.10bit.EAC3.5.1.Atmos.GokiTAoE", WEBRip1080p},
		{"Series.Title.S01.1080p.AMZN.WEB-Rip.DDP5.1.H.264-Telly", WEBRip1080p},

		// ===== WEBDL-2160p =====
		{"Series.Title.2016.03.14.2160p.WEB.x264-spamTV", WEBDL2160p},
		{"Series.Title.2016.03.14.2160p.WEB.h264-spamTV", WEBDL2160p},
		{"Series.Title.2016.03.14.2160p.WEB.PROPER.h264-spamTV", WEBDL2160p},
		{"House.of.Sonarr.AK.s05e13.4K.UHD.WEB.DL", WEBDL2160p},
		{"House.of.Sonarr.AK.s05e13.UHD.4K.WEB.DL", WEBDL2160p},
		{"[HorribleSubs] Series Title! S01 [Web][MKV][h264][2160p][AAC 2.0][Softsubs (HorribleSubs)]", WEBDL2160p},
		{"Series Title S02 2013 WEB-DL 4k H265 AAC 2Audio-HDSWEB", WEBDL2160p},
		{"Series.Title.S02E02.This.Year.Will.Be.Different.2160p.WEB.H.265", WEBDL2160p},
		{"Series.Title.S02E04.German.Dubbed.DL.AAC.2160p.DV.HDR.WEB.HEVC-GROUP", WEBDL2160p},

		// ===== WEBRip-2160p =====
		{"Series S01E01.2160P AMZN WEBRIP DD2.0 HI10P X264-TROLLUHD", WEBRip2160p},
		{"JUST ADD SONARR S01E01.2160P AMZN WEBRIP DD2.0 X264-TROLLUHD", WEBRip2160p},
		{"The.Man.In.The.Series.S01E01.2160p.AMZN.WEBRip.DD2.0.Hi10p.X264-TrollUHD", WEBRip2160p},
		{"The Man In the Series S01E01 2160p AMZN WEBRip DD2.0 Hi10P x264-TrollUHD", WEBRip2160p},
		{"House.of.Sonarr.AK.S05E08.Chapter.60.2160p.NF.WEBRip.DD5.1.x264-NTb.NLsubs", WEBRip2160p},
		{"Sonarr Saves the World S01 2160p Netflix WEBRip DD5.1 x264-TrollUHD", WEBRip2160p},

		// ===== Bluray-720p =====
		{"SERIES.S03E01-06.DUAL.Bluray.AC3.-HELLYWOOD.avi", Bluray720p},
		{"Series - S01E03 - Come Fly With Me - 720p BluRay.mkv", Bluray720p},
		{"The Big Series.S03E01.The Sonarr Can Opener.m2ts", Bluray720p},
		{"Series.S01E02.Chained.Sonarr.[Bluray720p].mkv", Bluray720p},
		{"[FFF] DATE A Sonarr Dev - 01 [BD][720p-AAC][0601BED4]", Bluray720p},
		{"[RandomRemux] Series - 01 [720p BD][043EA407].mkv", Bluray720p},
		{"[Kaylith] Series Friends Specials - 01 [BD 720p AAC][B7EEE164].mkv", Bluray720p},
		{"SERIES.S03E01-06.DUAL.Blu-ray.AC3.-HELLYWOOD.avi", Bluray720p},
		{"SERIES.S03E01-06.DUAL.720p.Blu-ray.AC3.-HELLYWOOD.avi", Bluray720p},
		{"[Elysium]Lucky.Series.01(BD.720p.AAC.DA)[0BB96AD8].mkv", Bluray720p},
		{"Series.Galaxy.S01E01.33.720p.HDDVD.x264-SiNNERS.mkv", Bluray720p},
		{"The.Series.S01E07.RERIP.720p.BluRay.x264-DEMAND", Bluray720p},
		{"Series.Black.1x01.Selezione.Naturale.ITA.720p.BDMux.x264-NovaRip", Bluray720p},
		{"Series.Hunter.S02.720p.Blu-ray.Remux.AVC.FLAC.2.0-SiCFoI", Bluray720p},
		{"Adventures.of.Sonic.the.Hedgehog.S01E01.Best.Hedgehog.720p.DD.2.0.AVC.REMUX-FraMeSToR", Bluray720p},

		// ===== Bluray-1080p =====
		{"Series - S01E03 - Come Fly With Me - 1080p BluRay.mkv", Bluray1080p},
		{"Sonarr.Of.Series.S02E13.1080p.BluRay.x264-AVCDVD", Bluray1080p},
		{"Series.S01E02.Chained.Heat.[Bluray1080p].mkv", Bluray1080p},
		{"[FFF] Series no Muromi-san - 10 [BD][1080p-FLAC][0C4091AF]", Bluray1080p},
		{"[Kaylith] Series Friends Specials - 01 [BD 1080p FLAC][429FD8C7].mkv", Bluray1080p},
		{"[Zurako] Log Series - 01 - The Sonarr (BD 1080p AAC) [7AE12174].mkv", Bluray1080p},
		{"SERIES.S03E01-06.DUAL.1080p.Blu-ray.AC3.-HELLYWOOD.avi", Bluray1080p},
		{"[Coalgirls]_Series!!_01_(1920x1080_Blu-ray_FLAC)_[8370CB8F].mkv", Bluray1080p},
		{"Planet.Series.S01E11.Code.Deep.1080p.HD-DVD.DD.VC1-TRB", Bluray1080p},
		{"S for Series 2005 1080p UHD BluRay DD+7.1 x264-LoRD.mkv", Bluray1080p},
		{"Series.Title.2011.1080p.UHD.BluRay.DD5.1.HDR.x265-CtrlHD.mkv", Bluray1080p},
		{"Fall.Of.The.Release.Groups.S02E13.1080p.BDLight.x265-AVCDVD", Bluray1080p},

		// ===== Bluray-1080p Remux =====
		{"Series!!! on ICE - S01E12[JP BD Remux][ENG subs]", Bluray1080pRemux},
		{"Series.Title.S01E08.The.Well.BluRay.1080p.AVC.DTS-HD.MA.5.1.REMUX-FraMeSToR", Bluray1080pRemux},
		{"Series.Title.2x11.Nato.Per.La.Truffa.Bluray.Remux.AVC.1080p.AC3.ITA", Bluray1080pRemux},
		{"Series.Title.2x11.Nato.Per.La.Truffa.Bluray.Remux.AVC.AC3.ITA", Bluray1080pRemux},
		{"Series.Title.S03E01.The.Calm.1080p.DTS-HD.MA.5.1.AVC.REMUX-FraMeSToR", Bluray1080pRemux},
		{"Series Title Season 2 (BDRemux 1080p HEVC FLAC) [Netaro]", Bluray1080pRemux},
		{"Adventures.of.Sonic.the.Hedgehog.S01E01.Best.Hedgehog.1080p.DD.2.0.AVC.REMUX-FraMeSToR", Bluray1080pRemux},
		{"Series Title S01 2018 1080p BluRay Hybrid-REMUX AVC TRUEHD 5.1 Dual Audio-ZR-", Bluray1080pRemux},
		{"Series.Title.S01.2018.1080p.BluRay.Hybrid-REMUX.AVC.TRUEHD.5.1.Dual.Audio-ZR-", Bluray1080pRemux},

		// ===== Bluray-2160p =====
		{"Series.Title.US.s05e13.4K.UHD.Bluray", Bluray2160p},
		{"Series.Title.US.s05e13.UHD.4K.Bluray", Bluray2160p},
		{"[DameDesuYo] Series Bundle - Part 1 (BD 4K 8bit FLAC)", Bluray2160p},
		{"Series.Title.2014.2160p.UHD.BluRay.X265-IAMABLE.mkv", Bluray2160p},

		// ===== Bluray-2160p Remux =====
		{"Series!!! on ICE - S01E12[JP BD 2160p Remux][ENG subs]", Bluray2160pRemux},
		{"Series.Title.S01E08.The.Sonarr.BluRay.2160p.AVC.DTS-HD.MA.5.1.REMUX-FraMeSToR", Bluray2160pRemux},
		{"Series.Title.2x11.Nato.Per.The.Sonarr.Bluray.Remux.AVC.2160p.AC3.ITA", Bluray2160pRemux},
		{"[Dolby Vision] Sonarr.of.Series.S07.MULTi.UHD.BLURAY.REMUX.DV-NoTag", Bluray2160pRemux},
		{"Adventures.of.Sonic.the.Hedgehog.S01E01.Best.Hedgehog.2160p.DD.2.0.AVC.REMUX-FraMeSToR", Bluray2160pRemux},
		{"Series Title S01 2018 2160p BluRay Hybrid-REMUX AVC TRUEHD 5.1 Dual Audio-ZR-", Bluray2160pRemux},
		{"Series.Title.S01.2018.2160p.BluRay.Hybrid-REMUX.AVC.TRUEHD.5.1.Dual.Audio-ZR-", Bluray2160pRemux},

		// ===== Raw-HD =====
		{"POI S02E11 1080i HDTV DD5.1 MPEG2-TrollHD", RAWHD},
		{"How I Met Your Developer S01E18 Nothing Good Happens After Sonarr 720p HDTV DD5.1 MPEG2-TrollHD", RAWHD},
		{"The Series S01E11 The Finals 1080i HDTV DD5.1 MPEG2-TrollHD", RAWHD},
		{"Series.Title.S07E11.1080i.HDTV.DD5.1.MPEG2-NTb.ts", RAWHD},
		{"Game of Series S04E10 1080i HDTV MPEG2 DD5.1-CtrlHD.ts", RAWHD},
		{"Series.Title.S02E05.1080i.HDTV.DD2.0.MPEG2-NTb.ts", RAWHD},
		{"Show - S03E01 - Episode Title Raw-HD.ts", RAWHD},
		{"Series.Title.S10E09.Title.1080i.UPSCALE.HDTV.DD5.1.MPEG2-zebra", RAWHD},
		{"Series.Title.2011-08-04.1080i.HDTV.MPEG-2-CtrlHD", RAWHD},

		// ===== Additional test cases =====
		// WEB-DL variants
		{"The.Show.S01E01.1080p.WEB-DL.DD5.1.H.264-GROUP", WEBDL1080p},
		{"The.Show.S01E02.720p.WEB-DL.AAC2.0.H.264-GROUP", WEBDL720p},
		{"The.Show.S01E03.2160p.WEB-DL.DDP5.1.H.265-GROUP", WEBDL2160p},
		{"The.Show.S01E04.480p.WEB-DL.AAC2.0.H.264-GROUP", WEBDL480p},

		// WEBRip variants
		{"The.Show.S02E01.1080p.WEBRip.x264-GROUP", WEBRip1080p},
		{"The.Show.S02E02.720p.WEBRip.AAC2.0.H.264-GROUP", WEBRip720p},
		{"The.Show.S02E03.2160p.WEBRip.x265-GROUP", WEBRip2160p},
		{"The.Show.S02E04.480p.WEBRip.x264-GROUP", WEBRip480p},

		// HDTV variants
		{"The.Show.S03E01.1080p.HDTV.x264-GROUP", HDTV1080p},
		{"The.Show.S03E02.720p.HDTV.x264-GROUP", HDTV720p},
		{"The.Show.S03E03.2160p.HDTV.x265-GROUP", HDTV2160p},
		{"The.Show.S03E04.HDTV.x264-GROUP", SDTV},

		// BluRay variants
		{"The.Show.S04E01.1080p.BluRay.x264-GROUP", Bluray1080p},
		{"The.Show.S04E02.720p.BluRay.x264-GROUP", Bluray720p},
		{"The.Show.S04E03.2160p.BluRay.x265-GROUP", Bluray2160p},
		{"The.Show.S04E04.480p.BluRay.x264-GROUP", Bluray480p},
		{"The.Show.S04E05.576p.BluRay.x264-GROUP", Bluray576p},

		// REMUX variants
		{"The.Show.S05E01.1080p.BluRay.REMUX.AVC.DTS-HD.MA.5.1-GROUP", Bluray1080pRemux},
		{"The.Show.S05E02.2160p.UHD.BluRay.REMUX.HDR.HEVC.DTS-HD.MA.5.1-GROUP", Bluray2160pRemux},

		// DVD
		{"The.Show.S06E01.DVDRip.x264-GROUP", DVD},
		{"The.Show.S06E02.DVD.x264-GROUP", DVD},

		// BDRip/BRRip variants
		{"The.Show.S01E01.1080p.BDRip.x264-GROUP", Bluray1080p},
		{"The.Show.S01E02.720p.BDRip.x264-GROUP", Bluray720p},
		{"The.Show.S01E03.1080p.BRRip.x264-GROUP", Bluray1080p},
		{"The.Show.S01E04.720p.BRRip.x264-GROUP", Bluray720p},
		{"The.Show.S01E05.BDRip.x264-GROUP", Bluray480p},

		// PDTV/DSR/TVRip/SDTV variants
		{"The.Show.S01E01.PDTV.x264-GROUP", SDTV},
		{"The.Show.S01E02.720p.PDTV.x264-GROUP", HDTV720p},
		{"The.Show.S01E03.1080p.PDTV.x264-GROUP", HDTV1080p},
		{"The.Show.S01E04.DSR.x264-GROUP", SDTV},
		{"The.Show.S01E05.WS.DSR.x264-GROUP", SDTV},
		{"The.Show.S01E06.TVRip.x264-GROUP", SDTV},
		{"The.Show.S01E07.SDTV.x264-GROUP", SDTV},

		// Alternative resolution detection
		{"The.Show.S01E01.UHD.BluRay.x265-GROUP", Bluray2160p},
		{"The.Show.S01E02.[4K].BluRay.x265-GROUP", Bluray2160p},
		{"The.Show.S01E03.UHD.WEB-DL.x265-GROUP", WEBDL2160p},

		// Anime patterns
		{"[SubGroup] Anime Title - 01 [BD 1080p]", Bluray1080p},
		{"[SubGroup] Anime Title - 02 [BD 720p]", Bluray720p},
		{"[SubGroup] Anime Title - 03 [BD 2160p]", Bluray2160p},
		{"[SubGroup] Anime Title - 04 [WEB 1080p]", WEBDL1080p},
		{"[SubGroup] Anime Title - 05 [WEB 720p]", WEBDL720p},
		{"[SubGroup] Anime Title - 06 (WEB 1080p)", WEBDL1080p},

		// BDLight variant
		{"The.Show.S01E01.1080p.BDLight.x264-GROUP", Bluray1080p},

		// Raw-HD variants
		{"The.Show.S01E01.1080i.RawHD.DD5.1-GROUP", RAWHD},
		{"The.Show.S01E02.Raw-HD.x264-GROUP", RAWHD},
		{"The.Show.S01E03.720p.HDTV.MPEG2-GROUP", RAWHD},

		// Streaming service specific patterns
		{"The.Show.S09E01.1080p.AMZN.WEB-DL.DDP5.1.H.264-GROUP", WEBDL1080p},
		{"The.Show.S09E02.1080p.NF.WEB-DL.DDP5.1.H.264-GROUP", WEBDL1080p},
		{"The.Show.S09E03.2160p.DSNP.WEB-DL.DDP5.1.H.265-GROUP", WEBDL2160p},

		// More edge cases
		{"The.Show.2024.S01E01.1080p.WEB.H264-GROUP", WEBDL1080p},
		{"The.Show.S01E01.1080i.HDTV.DD5.1.MPEG2-GROUP", RAWHD},
		{"The.Show.S01E01.4K.UHD.BluRay.x265-GROUP", Bluray2160p},
	}

	for _, tt := range tests {
		t.Run(tt.title, func(t *testing.T) {
			result := Parse(tt.title)
			if result.Quality.Quality != tt.expected {
				t.Errorf("Parse(%q) = %v (%s), want %v (%s)",
					tt.title, result.Quality.Quality, result.Quality.Quality.String(),
					tt.expected, tt.expected.String())
			}
		})
	}
}

func TestQualityModelMethods(t *testing.T) {
	qm := QualityModel{
		Quality: Bluray1080pRemux,
		Revision: Revision{
			Version:  2,
			IsRepack: true,
		},
	}

	if qm.Source() != "BluRay" {
		t.Errorf("QualityModel.Source() = %v, want %v", qm.Source(), "BluRay")
	}
	if qm.Resolution() != "1080p" {
		t.Errorf("QualityModel.Resolution() = %v, want %v", qm.Resolution(), "1080p")
	}
	if !qm.IsRemux() {
		t.Errorf("QualityModel.IsRemux() = %v, want %v", qm.IsRemux(), true)
	}
	if qm.String() != "Bluray-1080p Remux v2 [REPACK]" {
		t.Errorf("QualityModel.String() = %v, want %v", qm.String(), "Bluray-1080p Remux v2 [REPACK]")
	}
	if qm.Full() != "Bluray-1080p Remux" {
		t.Errorf("QualityModel.Full() = %v, want %v", qm.Full(), "Bluray-1080p Remux")
	}
	if qm.Version() != 2 {
		t.Errorf("QualityModel.Version() = %v, want %v", qm.Version(), 2)
	}
}

func TestFull(t *testing.T) {
	tests := []struct {
		name     string
		quality  Quality
		expected string
	}{
		{"HDTV720p", HDTV720p, "HDTV-720p"},
		{"WEBDL1080p", WEBDL1080p, "WEBDL-1080p"},
		{"Bluray2160pRemux", Bluray2160pRemux, "Bluray-2160p Remux"},
		{"SDTV", SDTV, "SDTV"},
		{"Unknown", Unknown, "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			qm := QualityModel{Quality: tt.quality}
			if got := qm.Full(); got != tt.expected {
				t.Errorf("QualityModel.Full() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestListFields(t *testing.T) {
	fields := ListFields()
	if len(fields) == 0 {
		t.Error("ListFields() returned empty slice")
	}

	// Check that expected fields are present
	fieldNames := make(map[string]bool)
	for _, field := range fields {
		fieldNames[field.Name] = true
	}

	expectedFields := []string{"Full", "Resolution", "Source", "IsRemux", "IsRepack", "Version"}
	for _, expected := range expectedFields {
		if !fieldNames[expected] {
			t.Errorf("ListFields() missing expected field: %s", expected)
		}
	}
}

func TestGetField(t *testing.T) {
	qm := QualityModel{
		Quality: HDTV720p,
		Revision: Revision{
			Version:  2,
			IsRepack: true,
		},
	}

	tests := []struct {
		name     string
		expected any
	}{
		{"Full", "HDTV-720p"},
		{"Resolution", "720p"},
		{"Source", "HDTV"},
		{"IsRemux", false},
		{"IsRepack", true},
		{"Version", 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetField(tt.name, qm)
			if err != nil {
				t.Errorf("GetField(%s) error = %v", tt.name, err)
				return
			}
			if got != tt.expected {
				t.Errorf("GetField(%s) = %v, want %v", tt.name, got, tt.expected)
			}
		})
	}

	// Test unknown field
	_, err := GetField("UnknownField", qm)
	if err == nil {
		t.Error("GetField() with unknown field should return error")
	}
}

func TestParseReleaseGroup(t *testing.T) {
	tests := []struct {
		name     string
		title    string
		expected string // empty string if should be nil/not found
	}{
		// Standard release groups with dash prefix
		{"Standard dash format", "Movie.2009.S01E14.English.HDTV.XviD-LOL", "LOL"},
		{"Without dash separator", "Movie 2009 S01E14 English HDTV XviD LOL", ""},
		{"With dash suffix", "Punky.Brewster.S01.EXTRAS.DVDRip.XviD-RUNNER", "RUNNER"},
		{"Standard format 2", "2020.NZ.2011.12.02.PDTV.XviD-C4TV", "C4TV"},
		{"Standard format 3", "Some.Movie.S03E115.DVDRip.XviD-OSiTV", "OSiTV"},
		{"Standard format 4", "Movie.Name.S04E13.720p.WEB-DL.AAC2.0.H.264-Cyphanix", "Cyphanix"},
		{"Standard bracket at end", "The.Movie.Title.2013.720p.BluRay.x264-ROUGH [PublicHD]", "ROUGH"},

		// No group detected
		{"No group - resolution only", "Some Movie - S01E01 - Pilot [HTDV-480p]", ""},
		{"No group - resolution 720p", "Some Movie - S01E01 - Pilot [HTDV-720p]", ""},
		{"No group - resolution 1080p", "Some Movie - S01E01 - Pilot [HTDV-1080p]", ""},
		{"No group - no separator", "Movie.Name.S02E01.720p.WEB-DL.DD5.1.H.264.mkv", ""},
		{"No group - title only", "Series Title S01E01 Episode Title", ""},
		{"No group - date format", "Movie.Name- 2014-06-02 - Some Movie.mkv", ""},
		{"No group - no extension", "Acropolis Now S05 EXTRAS DVDRip XviD RUNNER", ""},

		// Website prefix cleaning
		{"Website prefix www.Torrenting.com", "[ www.Torrenting.com ] - Movie.Name.S03E14.720p.HDTV.X264-DIMENSION", "DIMENSION"},

		// Tracker suffix cleaning
		{"Tracker suffix rarbg", "Movie.Name S02E09 HDTV x264-2HD [eztv]-[rarbg.com]", "2HD"},

		// Clean suffixes (Rakuv*, postbot, xpost, etc.)
		{"Clean Rakuv suffix", "Blue.Movie.Name.S08E05.The.Movie.1080p.AMZN.WEB-DL.DDP5.1.H.264-NTb-Rakuv", "NTb"},
		{"Clean Rakuvfinhel suffix", "Movie.Name.S01E13.720p.BluRay.x264-SiNNERS-Rakuvfinhel", "SiNNERS"},
		{"Clean RakuvUS suffix", "Movie.Name.S01E01.INTERNAL.720p.HDTV.x264-aAF-RakuvUS-Obfuscated", "aAF"},
		{"Clean postbot suffix", "Movie.Name.2018.720p.WEBRip.DDP5.1.x264-NTb-postbot", "NTb"},
		{"Clean xpost suffix", "Movie.Name.2018.720p.WEBRip.DDP5.1.x264-NTb-xpost", "NTb"},
		{"Clean AsRequested suffix", "Movie.Name.S02E24.1080p.AMZN.WEBRip.DD5.1.x264-CasStudio-AsRequested", "CasStudio"},
		{"Clean AlternativeToRequested", "Movie.Name.S04E11.Lamster.1080p.AMZN.WEB-DL.DDP5.1.H.264-NTb-AlternativeToRequested", "NTb"},
		{"Clean GEROV suffix", "Movie.Name.S16E04.Third.Wheel.1080p.AMZN.WEB-DL.DDP5.1.H.264-NTb-GEROV", "NTb"},
		{"Clean Z0iDS3N suffix", "Movie.NameS10E06.Kid.n.Play.1080p.AMZN.WEB-DL.DDP5.1.H.264-NTb-Z0iDS3N", "NTb"},
		{"Clean Chamele0n suffix", "Movie.Name.S02E06.The.House.of.Lords.DVDRip.x264-MaG-Chamele0n", "MaG"},

		// Exception groups (exact matches)
		{"Exception D-Z0N3", "SomeMovie.1080p.BluRay.DTS-X.264.-D-Z0N3.mkv", "D-Z0N3"},
		{"Exception D-Z0N3 variant", "Some.Dead.Movie.2006.1080p.BluRay.DTS.x264.D-Z0N3", "D-Z0N3"},
		{"Exception YTS.LT bracket", "Movie.Title.2010.720p.BluRay.x264.-[YTS.LT]", "YTS.LT"},
		{"Exception YTS.AG bracket", "Movie.Name.2022.1080p.BluRay.x264-[YTS.AG]", "YTS.AG"},
		{"Exception YTS.MX bracket", "Movie Name (2020) [1080p] [WEBRip] [5.1] [YTS.MX]", "YTS.MX"},
		{"Exception KRaLiMaRKo", "Movie Name.2018.1080p.Blu-ray.Remux.AVC.DTS-HD.MA.5.1.KRaLiMaRKo", "KRaLiMaRKo"},
		{"Exception E.N.D", "Movie Name (2001) 1080p NF WEB-DL DDP2.0 x264-E.N.D", "E.N.D"},
		{"Exception VARYG", "Movie.Name.2022.1080p.BluRay.x264-VARYG", "VARYG"},
		{"Exception TAoE parentheses", "Movie Name (2017) (Showtime) (1080p.BD.DD5.1.x265-TheSickle[TAoE])", "TAoE"},

		// Pattern exception groups (ending with ) or ])
		{"Pattern Joy closing paren", "Movie Name (2020) [2160p x265 10bit S82 Joy]", "Joy"},
		{"Pattern QxR closing bracket", "Movie Name (2003) (2160p BluRay X265 HEVC 10bit HDR AAC 7.1 Tigole) [QxR]", "QxR"},
		{"Pattern Joy closing paren 2", "Ode To Joy (2009) (2160p BluRay x265 10bit HDR Joy)", "Joy"},
		{"Pattern FreetheFish", "Ode To Joy (2009) (2160p BluRay x265 10bit HDR FreetheFish)", "FreetheFish"},
		{"Pattern afm72", "Ode To Joy (2009) (2160p BluRay x265 10bit HDR afm72)", "afm72"},
		{"Pattern Anna", "Movie Name (2012) (1080p BluRay x265 HEVC 10bit AC3 2.0 Anna)", "Anna"},
		{"Pattern Bandi", "Movie Name (2019) (2160p BluRay x265 HEVC 10bit HDR AAC 7.1 Bandi)", "Bandi"},
		{"Pattern Ghost", "Movie Name (2009) (1080p HDTV x265 HEVC 10bit AAC 2.0 Ghost)", "Ghost"},
		{"Pattern Tigole", "Movie Name in the Movie (2017) (1080p BluRay x265 HEVC 10bit AAC 7.1 Tigole)", "Tigole"},
		{"Pattern Tigole 2", "Mission - Movie Name - Movie Protocol (2011) (1080p BluRay x265 HEVC 10bit AAC 7.1 Tigole)", "Tigole"},
		{"Pattern Silence", "Movie Name (1990) (1080p BluRay x265 HEVC 10bit AAC 5.1 Silence)", "Silence"},
		{"Pattern Kappa", "Happy Movie Name (1999) (1080p BluRay x265 HEVC 10bit AAC 5.1 Korean Kappa)", "Kappa"},
		{"Pattern MONOLITH", "Movie Name (2007) Open Matte (1080p AMZN WEB-DL x265 HEVC 10bit AAC 5.1 MONOLITH)", "MONOLITH"},
		{"Pattern Qman", "Movie-Name (2019) (1080p BluRay x265 HEVC 10bit DTS 7.1 Qman)", "Qman"},
		{"Pattern RZeroX", "Movie Name - Hell to Ticket (2018) + Extras (1080p BluRay x265 HEVC 10bit AAC 5.1 RZeroX)", "RZeroX"},
		{"Pattern SAMPA", "Movie Name (2013) (Diamond Luxe Edition) + Extras (1080p BluRay x265 HEVC 10bit EAC3 7.1 SAMPA)", "SAMPA"},
		{"Pattern theincognito", "Movie Name 2016 (1080p BluRay x265 HEVC 10bit DDP 5.1 theincognito)", "theincognito"},
		{"Pattern t3nzin", "Movie Name - A History of Movie (2017) (1080p AMZN WEB-DL x265 HEVC 10bit EAC3 2.0 t3nzin)", "t3nzin"},
		{"Pattern Vyndros", "Movie Name (2019) (1080p BluRay x265 HEVC 10bit AAC 7.1 Vyndros)", "Vyndros"},
		{"Pattern HDO closing bracket", "Movie Name (2015) [BDRemux 1080p AVC ES-CAT-EN DTS-HD MA 5.1 Subs][HDO]", "HDO"},
		{"Pattern DusIctv", "Another Crappy Anime Movie Name 1999 [DusIctv] [Blu-ray][MKV][h264][1080p][DTS-HD MA 5.1][Dual Audio][Softsubs (DusIctv)", "DusIctv"},
		{"Pattern DHD", "Another Crappy Anime Movie Name 1999 [DHD] [Blu-ray][MKV][h264][1080p][AAC 5.1][Dual Audio][Softsubs (DHD)]", "DHD"},
		{"Pattern SEV", "Another Crappy Anime Movie Name 1999 [SEV] [Blu-ray][MKV][h265 10-bit][1080p][FLAC 5.1][Dual Audio][Softsubs (SEV)]", "SEV"},
		{"Pattern CtrlHD", "Another Crappy Anime Movie Name 1999 [CtrlHD] [Blu-ray][MKV][h264][720p][AC3 2.0][Dual Audio][Softsubs (CtrlHD)]", "CtrlHD"},

		// No pattern exception match (should use standard parsing)
		{"No pattern exception", "Ode To Joy (2009) (2160p BluRay x265 10bit HDR)", ""},
		{"No HDO match without bracket", "Movie Name (2015) [BDRemux 1080p AVC ES-CAT-EN DTS-HD MA 5.1 Subs]", ""},

		// REMUX without release group
		{"REMUX no group DTS-X", "Some.Movie.2013.1080p.BluRay.REMUX.AVC.DTS-X.MA.5.1", ""},
		{"REMUX no group DTS-MA", "Some.Movie.2013.1080p.BluRay.REMUX.AVC.DTS-MA.5.1", ""},
		{"REMUX no group DTS-ES", "Movie.Name.2013.1080p.BluRay.REMUX.AVC.DTS-ES.MA.5.1", ""},

		// Multi-part groups with dash
		{"Multi-part group", "SomeMovie.1080p.BluRay.DTS.x264.-Blu-bits.mkv", "Blu-bits"},
		{"Multi-part group 2", "SomeMovie.1080p.BluRay.DTS.x264.-DX-TV.mkv", "DX-TV"},
		{"Multi-part group 3", "SomeMovie.1080p.BluRay.DTS.x264.-FTW-HS.mkv", "FTW-HS"},
		{"Multi-part group 4", "SomeMovie.1080p.BluRay.DTS.x264.-VH-PROD.mkv", "VH-PROD"},

		// Various edge cases
		{"Pre suffix cleaned", "The.Movie.Name.720p.HEVC.x265-MeGusta-Pre", "MeGusta"},
		{"Dash rl group", "Movie.Name 10x11 - Wild Movies Cant Be Broken [rl].avi", "rl"},
		{"Standard PSA group", "The.Movie.of.the.Name.1991.REMASTERED.720p.10bit.BluRay.6CH.x265.HEVC-PSA", "PSA"},
		{"No WEB-DL group", "Movie.Title.2019.1080p.AMZN.WEB-Rip.DDP.5.1.HEVC", ""},
		{"DataLass with dash", "Movie Name (2017) [2160p REMUX] [HEVC DV HYBRID HDR10+ Dolby TrueHD Atmos 7 1 24-bit Audio English]-DataLass", "DataLass"},
		{"No group in REMUX", "Movie Name (2017) [2160p REMUX] [HEVC DV HYBRID HDR10+ Dolby TrueHD Atmos 7 1 24-bit Audio English] [Data Lass]", ""},

		// Audio channel patterns should NOT be detected as release groups
		{"Audio 5.1 at end", "Nuremberg (2025) [1080p] [WEBRip] [5.1]", ""},
		{"Audio 7.1 at end", "Movie.Title.2020.1080p.BluRay.x264.[7.1]", ""},
		{"Audio 2.0 at end", "Series.S01E01.720p.WEB-DL.[2.0]", ""},
		{"Audio DTS-X.MA.5.1", "SomeShow.S20E13.1080p.BluRay.DTS-X.MA.5.1.x264", ""},
		{"Audio DTS-MA.5.1", "SomeShow.S20E13.1080p.BluRay.DTS-MA.5.1.x264", ""},
		{"Audio DTS-ES.5.1", "SomeShow.S20E13.1080p.BluRay.DTS-ES.5.1.x264", ""},
		{"Audio with group after", "SomeShow.S20E13.1080p.Blu-Ray.DTS-ES.5.1.x264-ROUGH [PublicHD]", "ROUGH"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Parse(tt.title)
			got := result.Release.GetReleaseGroup()
			if got != tt.expected {
				t.Errorf("Parse(%q).Release.ReleaseGroup = %q, want %q",
					tt.title, got, tt.expected)
			}
		})
	}
}

func TestParseEdition(t *testing.T) {
	tests := []struct {
		name     string
		title    string
		expected string // empty if should be nil/empty
	}{
		// Basic editions
		{"Directors Cut with year after", "Movie Title 2012 Directors Cut", "Directors Cut"},
		{"Despecialized in parens", "Movie Title 1999 (Despecialized).mkv", "Despecialized"},
		{"Special Edition Remastered dots", "Movie Title.2012.(Special.Edition.Remastered).[Bluray-1080p].mkv", "Special Edition Remastered"},
		{"Extended simple", "Movie Title 2012 Extended", "Extended"},
		{"Extended Directors Cut Fan Edit", "Movie Title 2012 Extended Directors Cut Fan Edit", "Extended Directors Cut Fan Edit"},
		{"Director's Cut apostrophe", "Movie Title 2012 Director's Cut", "Director's Cut"},
		{"Directors Cut no apostrophe", "Movie Title 2012 Directors Cut", "Directors Cut"},
		{"Extended Theatrical Version IMAX", "Movie Title.2012.(Extended.Theatrical.Version.IMAX).BluRay.1080p.2012.asdf", "Extended Theatrical Version IMAX"},
		{"Director's Cut with weird year", "2021 A Movie (1968) Director's Cut .mkv", "Director's Cut"},
		{"Extended Directors Cut FanEdit parens", "2021 A Movie 1968 (Extended Directors Cut FanEdit)", "Extended Directors Cut FanEdit"},
		{"Directors only", "A Fake Movie 2035 2012 Directors.mkv", "Directors"},
		{"Director's Cut year in middle", "Movie 2049 Director's Cut.mkv", "Director's Cut"},
		{"50th Anniversary Edition", "Movie Title 2012 50th Anniversary Edition.mkv", "50th Anniversary Edition"},
		{"2in1 edition", "Movie 2012 2in1.mkv", "2in1"},
		{"IMAX simple", "Movie 2012 IMAX.mkv", "IMAX"},
		{"Restored edition", "Movie 2012 Restored.mkv", "Restored"},
		{"Special Edition Fan Edit", "Movie Title.Special.Edition.Fan Edit.2012..BRRip.x264.AAC-m2g", "Special Edition Fan Edit"},
		{"Despecialized parens year after", "Movie Title (Despecialized) 1999.mkv", "Despecialized"},
		{"Special Edition Remastered parens year after", "Movie Title.(Special.Edition.Remastered).2012.[Bluray-1080p].mkv", "Special Edition Remastered"},
		{"Extended year after", "Movie Title Extended 2012", "Extended"},
		{"Extended Directors Cut Fan Edit year after", "Movie Title Extended Directors Cut Fan Edit 2012", "Extended Directors Cut Fan Edit"},
		{"Director's Cut year after", "Movie Title Director's Cut 2012", "Director's Cut"},
		{"Directors Cut year after", "Movie Title Directors Cut 2012", "Directors Cut"},
		{"Extended Theatrical Version IMAX year after", "Movie Title.(Extended.Theatrical.Version.IMAX).2012.BluRay.1080p.asdf", "Extended Theatrical Version IMAX"},
		{"Director's Cut year in parens", "Movie Director's Cut (1968).mkv", "Director's Cut"},
		{"Extended Directors Cut FanEdit complex", "2021 A Movie (Extended Directors Cut FanEdit) 1968 Bluray 1080p", "Extended Directors Cut FanEdit"},
		{"Directors middle of title", "A Fake Movie 2035 Directors 2012.mkv", "Directors"},
		{"Director's Cut middle of title", "Movie Director's Cut 2049.mkv", "Director's Cut"},
		{"50th Anniversary Edition year after", "Movie Title 50th Anniversary Edition 2012.mkv", "50th Anniversary Edition"},
		{"2in1 year after", "Movie 2in1 2012.mkv", "2in1"},
		{"IMAX year after", "Movie IMAX 2012.mkv", "IMAX"},
		{"Final Cut year after", "Fake Movie Final Cut 2016", "Final Cut"},
		{"Final Cut year after trailing space", "Fake Movie 2016 Final Cut ", "Final Cut"},
		{"Extended Cut with GERMAN", "My Movie GERMAN Extended Cut 2016", "Extended Cut"},
		{"Extended Cut dots with GERMAN", "My.Movie.GERMAN.Extended.Cut.2016", "Extended Cut"},
		{"Extended Cut dots no year", "My.Movie.GERMAN.Extended.Cut", "Extended Cut"},
		{"Assembly Cut", "My.Movie.Assembly.Cut.1992.REPACK.1080p.BluRay.DD5.1.x264-Group", "Assembly Cut"},
		{"Ultimate Hunter Edition", "Movie.1987.Ultimate.Hunter.Edition.DTS-HD.DTS.MULTISUBS.1080p.BluRay.x264.HQ-TUSAHD", "Ultimate Hunter Edition"},
		{"Diamond Edition", "Movie.1950.Diamond.Edition.1080p.BluRay.x264-nikt0", "Diamond Edition"},
		{"Ultimate Rekall Edition", "Movie.Title.1990.Ultimate.Rekall.Edition.NORDiC.REMUX.1080p.BluRay.AVC.DTS-HD.MA5.1-TWA", "Ultimate Rekall Edition"},
		{"Signature Edition", "Movie.Title.1971.Signature.Edition.1080p.BluRay.FLAC.2.0.x264-TDD", "Signature Edition"},
		{"Imperial Edition", "Movie.1979.The.Imperial.Edition.BluRay.720p.DTS.x264-CtrlHD", "Imperial Edition"},
		{"Open Matte", "Movie.1997.Open.Matte.1080p.BluRay.x264.DTS-FGT", "Open Matte"},

		// Negative cases - should NOT match
		{"No match - Holiday Special in title", "Movie.Holiday.Special.1978.DVD.REMUX.DD.2.0-ViETNAM", ""},
		{"No match - Directors Cut as title", "Directors.Cut.German.2006.COMPLETE.PAL.DVDR-LoD", ""},
		{"No match - Rogue in title", "Movie Impossible: Rogue Movie 2012 Bluray", ""},
		{"No match - FRENCH MD", "Loving.Movie.2018.TS.FRENCH.MD.x264-DROGUERiE", ""},
		{"No match - Uncut as prefix", "Uncut.Movie.2019.720p.BluRay.x264-YOL0W", ""},
		{"No match - Christmas Edition as title", "The.Christmas.Edition.1941.720p.HDTV.x264-CRiMSON", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Parse(tt.title)
			got := result.Release.GetEdition()
			if got != tt.expected {
				t.Errorf("Parse(%q).Release.Edition = %q, want %q",
					tt.title, got, tt.expected)
			}
		})
	}
}
