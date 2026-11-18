package tmdb

import "testing"

func TestAiredCounts(t *testing.T) {
	seasons := []Season{
		{SeasonNumber: 1, EpisodeCount: 9, AirDate: "2022-02-18"},
		{SeasonNumber: 2, EpisodeCount: 10, AirDate: "2025-01-17"},
		{SeasonNumber: 3, EpisodeCount: 8, AirDate: "2026-11-14"}, // two out so far
		{SeasonNumber: 4, EpisodeCount: 0, AirDate: ""},           // announced only
		{SeasonNumber: 5, EpisodeCount: 6, AirDate: "2027-03-01"}, // dated, future
	}
	airedCounts(seasons, &episodeRef{SeasonNumber: 3, EpisodeNumber: 2}, "2026-11-18")
	want := []int{9, 10, 2, 0, 0}
	for i, s := range seasons {
		if s.Aired != want[i] {
			t.Errorf("season %d: aired %d, want %d", s.SeasonNumber, s.Aired, want[i])
		}
	}
	// No "last episode" info: trust the counts for seasons that have started.
	old := []Season{{SeasonNumber: 1, EpisodeCount: 13}}
	airedCounts(old, nil, "2026-11-18")
	if old[0].Aired != 13 {
		t.Errorf("got %d, want 13", old[0].Aired)
	}
}
