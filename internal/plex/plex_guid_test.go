package plex

import (
	"encoding/json"
	"testing"
)

func TestRealPlexGuidFields(t *testing.T) {
	var c container
	body := `{"MediaContainer":{"Metadata":[
		{"title":"New agent","guid":"plex://movie/5d77","Guid":[{"id":"tmdb://603"},{"id":"imdb://tt0133093"}]},
		{"title":"Legacy","guid":"com.plexapp.agents.themoviedb://550?lang=en"},
		{"title":"Legacy imdb","guid":"com.plexapp.agents.imdb://tt0137523?lang=en"},
		{"title":"Legacy tv","guid":"com.plexapp.agents.thetvdb://81189/1/1?lang=en"}]}}`
	if err := json.Unmarshal([]byte(body), &c); err != nil {
		t.Fatal(err)
	}
	m := c.MediaContainer.Metadata
	if id, imdb, _ := m[0].IDs(); id != 603 || imdb != "tt0133093" {
		t.Fatalf("new agent: %d %s", id, imdb)
	}
	if id, _, _ := m[1].IDs(); id != 550 {
		t.Fatalf("legacy tmdb: %d", id)
	}
	if _, imdb, _ := m[2].IDs(); imdb != "tt0137523" {
		t.Fatalf("legacy imdb: %s", imdb)
	}
	if _, _, tv := m[3].IDs(); tv != 81189 {
		t.Fatalf("legacy tvdb: %d", tv)
	}
}
