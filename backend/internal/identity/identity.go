package identity

import (
	"errors"
	"regexp"
	"strconv"

	"github.com/kyleaupton/arrflix/internal/model"
)

type Identity struct {
	TmdbID  *int64
	TvdbID  *int64
	ImdbID  *string
	Season  *int32
	Episode *int32
}

var ErrNoIdentityFound = errors.New("no identity found")

// Returns the id and provider for the given media file path.
// If a tv series, the season and episode number are also returned.
func Resolve(library model.Library, path string) (Identity, error) {
	provider, id, err := getIdFromString(path)
	if err != nil {
		return Identity{}, err
	}

	var payload = Identity{}

	switch provider {
	case "tmdb":
		idInt, err := strconv.ParseInt(id, 10, 64)
		if err != nil {
			return Identity{}, err
		}

		payload.TmdbID = &idInt
	case "tvdb":
		idInt, err := strconv.ParseInt(id, 10, 64)
		if err != nil {
			return Identity{}, err
		}

		payload.TvdbID = &idInt
	case "imdb":
		payload.ImdbID = &id
	}

	if library.Type == "series" {
		// attempt to get season and episode from path
		season, episode, err := getSeasonAndEpisodeFromPath(path)
		if err != nil {
			return Identity{}, err
		}

		payload.Season = &season
		payload.Episode = &episode
	}

	return payload, nil
}

// Returns the first id and provider found in the string.
func getIdFromString(str string) (string, string, error) {
	re := regexp.MustCompile(`(tvdb|tmdb|imdb)-((?:tt)?\d+)`)
	matches := re.FindStringSubmatch(str)
	if len(matches) == 0 {
		return "", "", ErrNoIdentityFound
	}

	return matches[1], matches[2], nil
}

func getSeasonAndEpisodeFromPath(path string) (int32, int32, error) {
	re := regexp.MustCompile(`[sS](\d+)[eE](\d+)`)
	matches := re.FindStringSubmatch(path)
	if len(matches) == 0 {
		return 0, 0, ErrNoIdentityFound
	}

	season, err := strconv.ParseInt(matches[1], 10, 32)
	if err != nil {
		return 0, 0, err
	}

	episode, err := strconv.ParseInt(matches[2], 10, 32)
	if err != nil {
		return 0, 0, err
	}

	return int32(season), int32(episode), nil
}
