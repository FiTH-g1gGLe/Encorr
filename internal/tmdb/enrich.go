package tmdb

import "strings"

// crewJobs are the jobs shown on the title page, in order, with the label
// they get. TMDB has dozens; these are the ones people look for.
var crewJobs = []struct{ label, job string }{
	{"Director", "Director"},
	{"Writer", "Screenplay"},
	{"Writer", "Writer"},
	{"Story", "Story"},
	{"Based on the book by", "Novel"},
	{"Producer", "Producer"},
	{"Executive producer", "Executive Producer"},
	{"Music", "Original Music Composer"},
	{"Cinematography", "Director of Photography"},
}

func (c *Client) enrich(d *Details, r *rawDetails, typ string) {
	for _, g := range r.Genres {
		d.GenreList = append(d.GenreList, Named{ID: g.ID, Name: g.Name})
	}
	d.VoteCount = r.VoteCount
	d.OriginalTitle = r.OriginalTitle
	if typ == "tv" {
		d.OriginalTitle = r.OriginalName
	}
	if d.OriginalTitle == d.Title {
		d.OriginalTitle = ""
	}
	d.Language = strings.ToUpper(r.OriginalLanguage)
	for _, l := range r.SpokenLanguages {
		if l.ISO == r.OriginalLanguage && l.EnglishName != "" {
			d.Language = l.EnglishName
		}
	}
	if n, ok := languageNames[r.OriginalLanguage]; ok {
		d.Language = n
	}
	for _, x := range r.ProductionCountries {
		d.Countries = append(d.Countries, x.Name)
	}
	for i, x := range r.ProductionCompanies {
		if i < 3 {
			d.Studios = append(d.Studios, x.Name)
		}
	}
	d.Budget, d.Revenue, d.Homepage = r.Budget, r.Revenue, r.Homepage
	d.NumSeasons, d.NumEpisodes = r.NumSeasons, r.NumEpisodes
	if typ == "tv" {
		d.FirstAir, d.LastAir = r.FirstAirDate, r.LastAirDate
	}

	// Release dates for this region: cinema (2/3), digital (4), disc (5), TV (6).
	kinds := map[int]string{2: "cinema", 3: "cinema", 4: "digital", 5: "disc", 6: "tv"}
	seen := map[string]bool{}
	for _, rd := range r.ReleaseDates.Results {
		if rd.ISO != c.Region {
			continue
		}
		for _, x := range rd.ReleaseDates {
			k := kinds[x.Type]
			if k == "" || seen[k] || len(x.Date) < 10 {
				continue
			}
			seen[k] = true
			d.Releases = append(d.Releases, Release{Kind: k, Date: x.Date[:10]})
		}
	}

	for i, p := range r.Credits.Cast {
		if i >= 20 {
			break
		}
		d.Cast = append(d.Cast, Person{ID: p.ID, Name: p.Name, Character: p.Character, Photo: p.ProfilePath})
	}
	groups := map[string]*CrewGroup{}
	var order []string
	addCrew := func(label string, p Person) {
		g := groups[label]
		if g == nil {
			g = &CrewGroup{Job: label}
			groups[label] = g
			order = append(order, label)
		}
		for _, x := range g.People {
			if x.ID == p.ID {
				return
			}
		}
		if len(g.People) < 3 {
			g.People = append(g.People, p)
		}
	}
	for _, cb := range r.CreatedBy {
		addCrew("Created by", Person{ID: cb.ID, Name: cb.Name, Photo: cb.ProfilePath})
	}
	for _, j := range crewJobs {
		for _, p := range r.Credits.Crew {
			if p.Job == j.job {
				addCrew(j.label, Person{ID: p.ID, Name: p.Name, Photo: p.ProfilePath})
			}
		}
	}
	for _, label := range order {
		d.Crew = append(d.Crew, *groups[label])
	}

	d.Keywords = r.Keywords.Keywords
	if typ == "tv" {
		d.Keywords = r.Keywords.Results
	}
	if len(d.Keywords) > 12 {
		d.Keywords = d.Keywords[:12]
	}
	if wp, ok := r.WatchProviders.Results[c.Region]; ok {
		d.ProvidersURL = wp.Link
		for _, p := range wp.Flatrate {
			d.Providers = append(d.Providers, Provider{Name: p.Name, Logo: p.Logo})
		}
	}
}

// A few common languages TMDB's spoken_languages doesn't always name.
var languageNames = map[string]string{
	"en": "English", "fr": "French", "es": "Spanish", "de": "German", "it": "Italian",
	"ja": "Japanese", "ko": "Korean", "zh": "Chinese", "cn": "Cantonese", "hi": "Hindi",
	"nl": "Dutch", "sv": "Swedish", "da": "Danish", "no": "Norwegian", "pt": "Portuguese",
	"ru": "Russian", "pl": "Polish", "tr": "Turkish", "ar": "Arabic", "th": "Thai",
}
