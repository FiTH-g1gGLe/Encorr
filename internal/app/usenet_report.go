package app

// The Usenet report: how the news servers, blocks and indexers are actually
// doing, from SABnzbd, Prowlarr and Radarr/Sonarr's own records. It answers
// "is this renewal worth it?", "how long will my block last?" and "why is
// it slow?" with real numbers.

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/FiTH-g1gGLe/encorr/internal/arr"
	"github.com/FiTH-g1gGLe/encorr/internal/sab"
	"github.com/FiTH-g1gGLe/encorr/internal/store"
)

// usenetNetworks maps news server addresses to the network (backbone) behind
// them. Servers on the same network carry the same articles, so a second
// one rarely finds what the first missed. Only well-known ones are listed:
// an unknown address says nothing rather than guessing.
var usenetNetworks = []struct{ match, network string }{
	{"newshosting", "Omicron"}, {"easynews", "Omicron"}, {"usenetserver", "Omicron"},
	{"eweka", "Omicron"}, {"tweaknews", "Omicron"}, {"pureusenet", "Omicron"}, {"sunnyusenet", "Omicron"},
	{"usenetexpress", "UsenetExpress"}, {"newsgroupdirect", "UsenetExpress"},
	{"giganews", "Giganews"}, {"supernews", "Giganews"},
	{"blocknews", "Abavia"}, {"bulknews", "Abavia"},
	{"usenet.farm", "Usenet.Farm"},
}

func usenetNetwork(host string) string {
	h := strings.ToLower(host)
	for _, n := range usenetNetworks {
		if strings.Contains(h, n.match) {
			return n.network
		}
	}
	return ""
}

type reportServer struct {
	Name        string   `json:"name"`
	Label       string   `json:"label"`
	Host        string   `json:"host"`
	Priority    int      `json:"priority"`
	Connections int      `json:"connections"`
	Enabled     bool     `json:"enabled"`
	Network     string   `json:"network,omitempty"`
	SameAs      []string `json:"sameAs,omitempty"` // other servers on the same network
	GB7         float64  `json:"gb7"`
	GB30        float64  `json:"gb30"`
	Share30     float64  `json:"share30"` // % of everything downloaded in 30 days
	Block       *struct {
		SizeGB   float64 `json:"sizeGB"`
		UsedGB   float64 `json:"usedGB"`
		PerDayGB float64 `json:"perDayGB"`
		DaysLeft int     `json:"daysLeft,omitempty"` // 0 = not being used
	} `json:"block,omitempty"`
}

type reportIndexer struct {
	Name          string  `json:"name"`
	Priority      int     `json:"priority"`
	Enabled       bool    `json:"enabled"`
	Queries       int     `json:"queries"`
	Grabs         int     `json:"grabs"`
	FailedQueries int     `json:"failedQueries"`
	FailedGrabs   int     `json:"failedGrabs"`
	AvgMs         int     `json:"avgMs"`
	Grabs90       int     `json:"grabs90"`    // grabs Radarr/Sonarr made from it in 90 days
	Imported90    int     `json:"imported90"` // of those, ended up in the library
	Failed90      int     `json:"failed90"`   // of those, failed to download
	Share90       float64 `json:"share90"`    // % of all grabs in 90 days
	Expires       string  `json:"expires,omitempty"`
	Lifetime      bool    `json:"lifetime,omitempty"`
}

type reportSpeed struct {
	RecentMBs float64 `json:"recentMBs"` // average of recent big downloads
	Samples   int     `json:"samples"`
	LineMBs   float64 `json:"lineMBs,omitempty"` // from the last speed test
	Percent   int     `json:"percent,omitempty"`
}

// UsenetReport is the whole report.
type UsenetReport struct {
	Servers  []reportServer  `json:"servers"`
	Indexers []reportIndexer `json:"indexers"`
	Speed    reportSpeed     `json:"speed"`
	Tips     []string        `json:"tips"`
	Built    time.Time       `json:"built"`
	Missing  []string        `json:"missing,omitempty"` // apps not connected
}

func (a *App) hUsenetReport(w http.ResponseWriter, r *http.Request, u *store.User) {
	ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
	defer cancel()
	writeJSON(w, 200, a.usenetReport(ctx))
}

func (a *App) usenetReport(ctx context.Context) *UsenetReport {
	rep := &UsenetReport{Servers: []reportServer{}, Indexers: []reportIndexer{}, Tips: []string{}, Built: time.Now()}
	var wg sync.WaitGroup
	var mu sync.Mutex
	if a.SAB() == nil {
		rep.Missing = append(rep.Missing, "SABnzbd")
	} else {
		wg.Add(1)
		go func() {
			defer wg.Done()
			servers, speed := a.reportSAB(ctx)
			mu.Lock()
			rep.Servers, rep.Speed = servers, speed
			mu.Unlock()
		}()
	}
	a.autoDetectProwlarr(ctx)
	if a.Prowlarr() == nil {
		rep.Missing = append(rep.Missing, "Prowlarr")
	} else {
		wg.Add(1)
		go func() {
			defer wg.Done()
			idx := a.reportIndexers(ctx)
			mu.Lock()
			rep.Indexers = idx
			mu.Unlock()
		}()
	}
	wg.Wait()
	a.Store.Read(func(d *store.Data) {
		if n := len(d.SpeedTests); n > 0 {
			rep.Speed.LineMBs = d.SpeedTests[n-1].Internet
		}
	})
	if rep.Speed.LineMBs > 0 && rep.Speed.RecentMBs > 0 {
		rep.Speed.Percent = int(math.Round(rep.Speed.RecentMBs / rep.Speed.LineMBs * 100))
	}
	rep.Tips = usenetTips(rep)
	return rep
}

// reportSAB reads servers, usage and recent download speeds from SABnzbd.
func (a *App) reportSAB(ctx context.Context) ([]reportServer, reportSpeed) {
	c := a.SAB()
	out := []reportServer{}
	var speed reportSpeed
	cfg, err := c.GetConfig(ctx)
	if err != nil {
		return out, speed
	}
	var stats struct {
		Servers map[string]struct {
			Total float64            `json:"total"`
			Daily map[string]float64 `json:"daily"`
		} `json:"servers"`
	}
	_ = c.CallRaw(ctx, "server_stats", &stats)
	since7 := time.Now().AddDate(0, 0, -7).Format("2006-01-02")
	since30 := time.Now().AddDate(0, 0, -30).Format("2006-01-02")
	var all30 float64
	for _, s := range cfg.Servers {
		if isStraySABServer(s) {
			continue
		}
		name := canon(s["name"])
		if name == "" {
			continue
		}
		label := canon(s["displayname"])
		if label == "" {
			label = name
		}
		rs := reportServer{Name: name, Label: label, Host: canon(s["host"]), Priority: int(num(s["priority"])),
			Connections: int(num(s["connections"])), Enabled: canon(s["enable"]) != "0"}
		rs.Network = usenetNetwork(rs.Host)
		st := stats.Servers[name]
		days30 := map[string]bool{}
		for d, b := range st.Daily {
			if d >= since30 {
				rs.GB30 += b / 1e9
				days30[d] = true
			}
			if d >= since7 {
				rs.GB7 += b / 1e9
			}
		}
		all30 += rs.GB30
		if q := sizeBytes(s["quota"]); q > 0 {
			used := st.Total - num(s["usage_at_start"])
			if used < 0 {
				used = 0
			}
			b := &struct {
				SizeGB   float64 `json:"sizeGB"`
				UsedGB   float64 `json:"usedGB"`
				PerDayGB float64 `json:"perDayGB"`
				DaysLeft int     `json:"daysLeft,omitempty"`
			}{SizeGB: q / 1e9, UsedGB: used / 1e9}
			b.PerDayGB = rs.GB30 / 30
			if b.PerDayGB > 0.001 {
				b.DaysLeft = int((b.SizeGB - b.UsedGB) / b.PerDayGB)
			}
			rs.Block = b
		}
		out = append(out, rs)
	}
	byNet := map[string][]string{}
	for _, s := range out {
		if s.Network != "" && s.Enabled {
			byNet[s.Network] = append(byNet[s.Network], s.Label)
		}
	}
	for i := range out {
		if all30 > 0 {
			out[i].Share30 = math.Round(out[i].GB30/all30*1000) / 10
		}
		for _, other := range byNet[out[i].Network] {
			if other != out[i].Label && out[i].Enabled {
				out[i].SameAs = append(out[i].SameAs, other)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Priority < out[j].Priority })

	speed.RecentMBs, speed.Samples = sabRecentSpeed(ctx, c)
	return out, speed
}

// reportIndexers combines Prowlarr's numbers with what Radarr/Sonarr did
// with each indexer's finds over the last 90 days.
func (a *App) reportIndexers(ctx context.Context) []reportIndexer {
	c := a.Prowlarr()
	var idx []map[string]any
	if c.GetJSON(ctx, "/indexer", &idx) != nil {
		return []reportIndexer{}
	}
	var stats struct {
		Indexers []struct {
			Name          string  `json:"indexerName"`
			Queries       int     `json:"numberOfQueries"`
			Grabs         int     `json:"numberOfGrabs"`
			FailedQueries int     `json:"numberOfFailedQueries"`
			FailedGrabs   int     `json:"numberOfFailedGrabs"`
			AvgMs         float64 `json:"averageResponseTime"`
		} `json:"indexers"`
	}
	_ = c.GetJSON(ctx, "/indexerstats", &stats)
	byName := map[string]*reportIndexer{}
	var out []*reportIndexer
	for _, ix := range idx {
		if canon(ix["protocol"]) == "torrent" {
			continue
		}
		name := canon(ix["name"])
		ri := &reportIndexer{Name: name, Priority: int(num(ix["priority"])), Enabled: canon(ix["enable"]) == "1"}
		if vip, ok := fieldValue(ix, "vipExpiration"); ok {
			if d, err := time.Parse("2006-01-02", canon(vip)); err == nil {
				ri.Expires = d.Format("2006-01-02")
				ri.Lifetime = d.Year() > 2100
			}
		}
		byName[strings.ToLower(name)] = ri
		out = append(out, ri)
	}
	for _, s := range stats.Indexers {
		if ri := byName[strings.ToLower(s.Name)]; ri != nil {
			ri.Queries, ri.Grabs, ri.FailedQueries, ri.FailedGrabs, ri.AvgMs = s.Queries, s.Grabs, s.FailedQueries, s.FailedGrabs, int(s.AvgMs)
		}
	}
	// What happened to each grab: imported, failed, or still waiting.
	since := time.Now().AddDate(0, 0, -90).UTC().Format(time.RFC3339)
	total := 0
	for _, cl := range []*arr.Client{a.Radarr(), a.Sonarr()} {
		if cl == nil {
			continue
		}
		var hist []arr.HistoryItem
		if cl.GetJSONQuery(ctx, "/history/since", url.Values{"date": {since}}, &hist) != nil {
			continue
		}
		grabbedBy := map[string]string{} // downloadId -> indexer
		for _, h := range hist {
			if h.EventType == "grabbed" && h.DownloadID != "" {
				grabbedBy[h.DownloadID] = strings.TrimSuffix(h.Data["indexer"], " (Prowlarr)")
			}
		}
		seen := map[string]bool{}
		for _, ix := range grabbedBy {
			if ri := byName[strings.ToLower(ix)]; ri != nil {
				ri.Grabs90++
				total++
			}
		}
		for _, h := range hist {
			ix := grabbedBy[h.DownloadID]
			ri := byName[strings.ToLower(ix)]
			if ri == nil || seen[h.DownloadID+h.EventType] {
				continue
			}
			switch h.EventType {
			case "downloadFolderImported":
				seen[h.DownloadID+h.EventType] = true
				ri.Imported90++
			case "downloadFailed":
				seen[h.DownloadID+h.EventType] = true
				ri.Failed90++
			}
		}
	}
	res := make([]reportIndexer, 0, len(out))
	for _, ri := range out {
		if total > 0 {
			ri.Share90 = math.Round(float64(ri.Grabs90)/float64(total)*1000) / 10
		}
		res = append(res, *ri)
	}
	sort.SliceStable(res, func(i, j int) bool { return res[i].Grabs90 > res[j].Grabs90 })
	return res
}

// usenetTips turns the numbers into a few plain suggestions.
func usenetTips(rep *UsenetReport) []string {
	var tips []string
	sp := rep.Speed
	if sp.Samples >= 3 && sp.Percent > 0 && sp.Percent < 40 {
		tips = append(tips, fmt.Sprintf("Downloads average %.0f MB/s, about %d%% of your line (%.0f MB/s). The limit is between you and your news server: try its address for your region (for Newshosting, news-eu.newshosting.com in Europe), or a few more connections.", sp.RecentMBs, sp.Percent, sp.LineMBs))
	}
	nets := map[string][]string{}
	var blocksFirst []string
	minUnlimited := math.MaxInt
	for _, s := range rep.Servers {
		if s.Enabled && s.Block == nil && s.Priority < minUnlimited {
			minUnlimited = s.Priority
		}
	}
	for _, s := range rep.Servers {
		if !s.Enabled {
			continue
		}
		if s.Network != "" {
			nets[s.Network] = append(nets[s.Network], s.Label)
		}
		if s.Block != nil && minUnlimited != math.MaxInt && s.Priority <= minUnlimited {
			blocksFirst = append(blocksFirst, s.Label)
		}
		if s.Block != nil && s.Block.DaysLeft > 0 && s.Block.DaysLeft < 60 {
			tips = append(tips, fmt.Sprintf("%s will run out in about %d days at the current rate.", s.Label, s.Block.DaysLeft))
		}
	}
	for net, list := range nets {
		if len(list) > 1 {
			tips = append(tips, fmt.Sprintf("%s are all on the %s network, so they carry the same articles: if one is missing something, the others usually are too. Real backup comes from a server on a different network.", joinAnd(list), net))
		}
	}
	if len(blocksFirst) > 0 {
		tips = append(tips, joinAnd(blocksFirst)+" (paid by the gigabyte) is tried before your unlimited server. Give unlimited servers the lowest priority number so blocks only fill gaps.")
	}
	for _, ix := range rep.Indexers {
		if ix.Grabs90 >= 10 && ix.Failed90*100/ix.Grabs90 >= 25 {
			tips = append(tips, fmt.Sprintf("%d of %d downloads from %s failed in the last 90 days. It may be worth a lower priority.", ix.Failed90, ix.Grabs90, ix.Name))
		}
		if ix.Enabled && ix.Queries > 200 && ix.FailedQueries*100/ix.Queries >= 20 {
			tips = append(tips, fmt.Sprintf("%s fails %d%% of searches. Check its API key and status in Prowlarr.", ix.Name, ix.FailedQueries*100/ix.Queries))
		}
	}
	return tips
}

func joinAnd(list []string) string {
	switch len(list) {
	case 0:
		return ""
	case 1:
		return list[0]
	}
	return strings.Join(list[:len(list)-1], ", ") + " and " + list[len(list)-1]
}

// sabRecentSpeed averages SABnzbd's recent big finished downloads (small
// ones never get up to speed). It returns MB/s and how many were counted.
func sabRecentSpeed(ctx context.Context, c *sab.Client) (float64, int) {
	var hist struct {
		History struct {
			Slots []struct {
				Bytes        float64 `json:"bytes"`
				DownloadTime float64 `json:"download_time"`
				Status       string  `json:"status"`
			} `json:"slots"`
		} `json:"history"`
	}
	if c.CallRawQuery(ctx, "history", url.Values{"limit": {"100"}}, &hist) != nil {
		return 0, 0
	}
	var bytes, secs float64
	n := 0
	for _, h := range hist.History.Slots {
		if h.Status == "Completed" && h.Bytes > 500e6 && h.DownloadTime > 5 && n < 30 {
			bytes += h.Bytes
			secs += h.DownloadTime
			n++
		}
	}
	if secs == 0 {
		return 0, 0
	}
	return math.Round(bytes/secs/1048576*10) / 10, n
}
