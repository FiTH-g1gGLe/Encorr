package app

import (
	"strings"
	"testing"

	"github.com/FiTH-g1gGLe/encorr/internal/store"
)

func TestUsenetNetwork(t *testing.T) {
	for host, want := range map[string]string{
		"news-eu.newshosting.com": "Omicron", "newshosting.tweaknews.eu": "Omicron", "secure.news.easynews.com": "Omicron",
		"news.usenetexpress.com": "UsenetExpress", "eu.usenetprime.com": "", "news.example.com": "",
	} {
		if got := usenetNetwork(host); got != want {
			t.Fatalf("%s: got %q, want %q", host, got, want)
		}
	}
}

func TestUsenetTips(t *testing.T) {
	block := func(days int) *struct {
		SizeGB   float64 `json:"sizeGB"`
		UsedGB   float64 `json:"usedGB"`
		PerDayGB float64 `json:"perDayGB"`
		DaysLeft int     `json:"daysLeft,omitempty"`
	} {
		return &struct {
			SizeGB   float64 `json:"sizeGB"`
			UsedGB   float64 `json:"usedGB"`
			PerDayGB float64 `json:"perDayGB"`
			DaysLeft int     `json:"daysLeft,omitempty"`
		}{SizeGB: 2000, DaysLeft: days}
	}
	rep := &UsenetReport{
		Speed: reportSpeed{RecentMBs: 30, LineMBs: 245, Percent: 12, Samples: 10},
		Servers: []reportServer{
			{Label: "Newshosting", Priority: 1, Enabled: true, Network: "Omicron"},
			{Label: "Tweaknews", Priority: 2, Enabled: true, Network: "Omicron"},
			{Label: "Prime Block", Priority: 0, Enabled: true, Block: block(40)},
		},
		Indexers: []reportIndexer{{Name: "FlakyNZB", Grabs90: 20, Failed90: 8, Enabled: true}},
	}
	all := strings.Join(usenetTips(rep), "\n")
	for _, want := range []string{"about 12% of your line", "Newshosting and Tweaknews are all on the Omicron network", "Prime Block (paid by the gigabyte) is tried before", "run out in about 40 days", "8 of 20 downloads from FlakyNZB failed"} {
		if !strings.Contains(all, want) {
			t.Fatalf("missing tip %q in:\n%s", want, all)
		}
	}
	rep2 := &UsenetReport{Speed: reportSpeed{RecentMBs: 200, LineMBs: 245, Percent: 82, Samples: 10},
		Servers: []reportServer{{Label: "Main", Enabled: true}, {Label: "Block", Priority: 3, Enabled: true, Block: block(900)}}}
	if tips := usenetTips(rep2); len(tips) != 0 {
		t.Fatalf("a healthy setup should have no tips: %v", tips)
	}
}

func TestTuneBlockOrderAndSpeed(t *testing.T) {
	e := &tuneEnv{sabSrv: []map[string]any{
		{"name": "main", "displayname": "Main", "enable": 1, "priority": 1, "quota": ""},
		{"name": "blk", "displayname": "Prime Block", "enable": 1, "priority": 0, "quota": "2T"},
		{"name": "late", "displayname": "Late Block", "enable": 1, "priority": 3, "quota": "1T"},
	}, speed: &store.SpeedTest{Internet: 245}, sabRecentMBs: 30}
	ids := map[string]TuneFinding{}
	for _, f := range checkSABServers(e) {
		ids[f.ID] = f
	}
	if f, ok := ids["sab-block-order:blk"]; !ok || !strings.Contains(f.Fix, "higher than 1") {
		t.Fatalf("block tried first should be flagged: %+v", ids)
	}
	if _, ok := ids["sab-block-order:late"]; ok {
		t.Fatal("a block after the unlimited server is fine")
	}
	if f, ok := ids["sab-slow-real"]; !ok || !strings.Contains(f.Title, "12%") {
		t.Fatalf("slow downloads should be flagged: %+v", f)
	}
	e.sabRecentMBs = 200
	for _, f := range checkSABServers(e) {
		if f.ID == "sab-slow-real" {
			t.Fatal("fast downloads flagged as slow")
		}
	}
}
