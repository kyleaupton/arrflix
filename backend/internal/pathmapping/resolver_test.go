package pathmapping

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/kyleaupton/arrflix/internal/model"
)

func volume(name, arrflixBase string, aliases ...model.VolumeAlias) model.Volume {
	return model.Volume{ID: uuid.New(), Name: name, ArrflixBase: arrflixBase, Aliases: aliases}
}

func alias(dl uuid.UUID, base string) model.VolumeAlias {
	return model.VolumeAlias{DownloaderID: dl, Base: base, Flavor: "posix"}
}

func TestToCanonical(t *testing.T) {
	t.Parallel()
	qbit := uuid.New()
	other := uuid.New()
	r := New([]model.Volume{
		volume("media", "/data/torrents", alias(qbit, "/downloads")),
		volume("tv", "/mnt/tv", alias(qbit, "/downloads/tv")),
		volume("root", "/srv", alias(qbit, "/")),
		volume("other", "/elsewhere", alias(other, "/downloads")),
	})

	cases := []struct {
		name, in, want, volume string
	}{
		{"swaps the alias base for the Arrflix base", "/downloads/X/X.mkv", "/data/torrents/X/X.mkv", "media"},
		{"the base itself maps", "/downloads", "/data/torrents", "media"},
		{"nested volume wins by longest prefix", "/downloads/tv/Y/Y.mkv", "/mnt/tv/Y/Y.mkv", "tv"},
		{"prefix matches whole segments only", "/downloadsextra/Z.mkv", "/srv/downloadsextra/Z.mkv", "root"},
		{"input is cleaned before matching", "/downloads//X/../X/X.mkv", "/data/torrents/X/X.mkv", "media"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := r.ToCanonical(qbit, tc.in)
			if err != nil {
				t.Fatalf("ToCanonical(%q): %v", tc.in, err)
			}
			if got.Path != tc.want {
				t.Errorf("ToCanonical(%q) = %q, want %q", tc.in, got.Path, tc.want)
			}
			if got.Volume == nil || got.Volume.Name != tc.volume {
				t.Errorf("ToCanonical(%q) volume = %v, want %q", tc.in, got.Volume, tc.volume)
			}
		})
	}

	t.Run("segment boundary with no catch-all leaves the path unchanged", func(t *testing.T) {
		t.Parallel()
		r := New([]model.Volume{volume("data", "/mnt/data", alias(qbit, "/data"))})
		got, err := r.ToCanonical(qbit, "/database/x.mkv")
		if err != nil {
			t.Fatal(err)
		}
		if got.Path != "/database/x.mkv" || got.Volume != nil {
			t.Errorf("ToCanonical(/database/x.mkv) = %+v, want unchanged with no volume", got)
		}
	})

	t.Run("another downloader's alias never applies", func(t *testing.T) {
		t.Parallel()
		r := New([]model.Volume{volume("other", "/elsewhere", alias(other, "/downloads"))})
		got, err := r.ToCanonical(qbit, "/downloads/X.mkv")
		if err != nil {
			t.Fatal(err)
		}
		if got.Path != "/downloads/X.mkv" || got.Volume != nil {
			t.Errorf("got %+v, want unchanged", got)
		}
	})

	t.Run("relative path is rejected", func(t *testing.T) {
		t.Parallel()
		if got, err := r.ToCanonical(qbit, "X/X.mkv"); err == nil {
			t.Errorf("ToCanonical(relative) = %+v, want an error", got)
		}
	})
}

func TestFromCanonical(t *testing.T) {
	t.Parallel()
	qbit := uuid.New()
	v := volume("media", "/data/torrents", alias(qbit, "/downloads"))

	got, err := FromCanonical(v, "/data/torrents/X/X.mkv", qbit)
	if err != nil || got != "/downloads/X/X.mkv" {
		t.Errorf("FromCanonical = %q, %v; want /downloads/X/X.mkv", got, err)
	}
	got, err = FromCanonical(v, "/data/torrents/X", uuid.New())
	if err != nil || got != "/data/torrents/X" {
		t.Errorf("FromCanonical without alias = %q, %v; want unchanged", got, err)
	}
	if _, err := FromCanonical(v, "/data/torrentsextra/X", qbit); err == nil {
		t.Errorf("FromCanonical outside the volume: want an error")
	}
	if _, err := FromCanonical(v, "torrents/X", qbit); err == nil {
		t.Errorf("FromCanonical relative: want an error")
	}
}

func TestToDownloader(t *testing.T) {
	t.Parallel()
	qbit := uuid.New()
	r := New([]model.Volume{
		volume("media", "/data/torrents", alias(qbit, "/downloads")),
		volume("4k", "/data/torrents/4k", alias(qbit, "/fast")),
	})

	cases := []struct {
		name, in, want, volume string
	}{
		{"swaps the Arrflix base for the alias", "/data/torrents/hd", "/downloads/hd", "media"},
		{"innermost volume wins", "/data/torrents/4k/x", "/fast/x", "4k"},
		{"outside every volume is unchanged", "/srv/other/", "/srv/other", ""},
		{"prefix matches whole segments only", "/data/torrentsextra", "/data/torrentsextra", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, v, err := r.ToDownloader(qbit, tc.in)
			if err != nil {
				t.Fatalf("ToDownloader(%q): %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("ToDownloader(%q) = %q, want %q", tc.in, got, tc.want)
			}
			name := ""
			if v != nil {
				name = v.Name
			}
			if name != tc.volume {
				t.Errorf("ToDownloader(%q) volume = %q, want %q", tc.in, name, tc.volume)
			}
		})
	}

	t.Run("a relative path is an error", func(t *testing.T) {
		t.Parallel()
		if _, _, err := r.ToDownloader(qbit, "data/torrents"); err == nil {
			t.Errorf("want an error")
		}
	})
}

func TestDeviceAndSameFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	a := filepath.Join(dir, "a.mkv")
	b := filepath.Join(dir, "b.mkv")
	c := filepath.Join(dir, "c.mkv")
	if err := os.WriteFile(a, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(a, b); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(c, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if same, err := SameFile(a, b); err != nil || !same {
		t.Errorf("SameFile(hardlink) = %v, %v; want true", same, err)
	}
	if same, err := SameFile(a, c); err != nil || same {
		t.Errorf("SameFile(copy) = %v, %v; want false", same, err)
	}
	da, err := Device(a)
	if err != nil {
		t.Fatal(err)
	}
	dd, err := Device(dir)
	if err != nil {
		t.Fatal(err)
	}
	if da != dd {
		t.Errorf("Device differs within one directory: %d vs %d", da, dd)
	}
	if _, err := Device(filepath.Join(dir, "missing")); err == nil {
		t.Errorf("Device(missing): want an error")
	}
}
