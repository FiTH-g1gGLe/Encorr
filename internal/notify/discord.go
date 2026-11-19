// Package notify sends Encorr's notifications. Version 0.1 supports Discord
// webhooks; the event model is shared so more channels can be added later.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Event string

const (
	EventRequested Event = "requested" // waiting for approval
	EventApproved  Event = "approved"
	EventAuto      Event = "autoapproved"
	EventAvailable Event = "available"
	EventDeclined  Event = "declined"
	EventFailed    Event = "failed"
	EventTest      Event = "test"
	EventFixed     Event = "fixed"    // Encorr sorted a download out by itself
	EventAttention Event = "warning"  // something needs the owner
	EventReported  Event = "reported" // someone reported a problem with a title
	EventResolved  Event = "resolved" // a reported problem was sorted
)

type Message struct {
	Event       Event
	Title       string // film or show title
	Year        int
	Poster      string // full image URL
	URL         string // link back to Encorr
	RequestedBy string
	MentionID   string // Discord user id to @mention
	Seasons     []int
	Detail      string // error text or note
	Library     string // where things appear: "Plex", "Jellyfin" or "your library"
}

func (m Message) library() string {
	if m.Library == "" {
		return "Plex"
	}
	return m.Library
}

var httpClient = &http.Client{Timeout: 10 * time.Second}

const (
	colorPending   = 0xF59E0B
	colorApproved  = 0x8B5CF6
	colorAvailable = 0x22C55E
	colorBad       = 0xEF4444
)

func heading(e Event) (string, int) {
	switch e {
	case EventRequested:
		return "Encore requested", colorPending
	case EventApproved:
		return "Request approved", colorApproved
	case EventAuto:
		return "Encore requested", colorApproved
	case EventAvailable:
		return "Your encore is ready", colorAvailable
	case EventDeclined:
		return "Request declined", colorBad
	case EventFailed:
		return "Request failed", colorBad
	case EventFixed:
		return "Encorr fixed a download", 0x38BDF8
	case EventAttention:
		return "Needs a hand", colorPending
	case EventReported:
		return "Problem reported", colorBad
	case EventResolved:
		return "Problem sorted", colorAvailable
	default:
		return "Encorr test message", colorApproved
	}
}

func seasonText(s []int) string {
	if len(s) == 0 {
		return ""
	}
	parts := make([]string, len(s))
	for i, n := range s {
		parts[i] = fmt.Sprint(n)
	}
	if len(s) == 1 {
		return "Season " + parts[0]
	}
	return "Seasons " + strings.Join(parts, ", ")
}

// Discord posts an embed to a webhook.
func Discord(ctx context.Context, webhook string, m Message) error {
	head, color := heading(m.Event)
	title := m.Title
	if m.Year > 0 {
		title = fmt.Sprintf("%s (%d)", m.Title, m.Year)
	}
	desc := ""
	switch m.Event {
	case EventRequested:
		desc = "Waiting for an admin to approve."
	case EventAuto, EventApproved:
		desc = "On its way: it'll appear in " + m.library() + " once it's downloaded."
	case EventAvailable:
		desc = "It's in " + m.library() + " now. Enjoy the show."
	case EventDeclined:
		desc = "This request was declined."
	case EventFailed:
		desc = "Encorr couldn't send this to Radarr/Sonarr."
	case EventTest:
		desc = "If you can read this, Discord notifications are working."
	case EventFixed, EventAttention, EventReported, EventResolved:
		desc, m.Detail = m.Detail, ""
	}
	fields := []map[string]any{}
	if m.RequestedBy != "" {
		fields = append(fields, map[string]any{"name": "Requested by", "value": m.RequestedBy, "inline": true})
	}
	if st := seasonText(m.Seasons); st != "" {
		fields = append(fields, map[string]any{"name": "Seasons", "value": st, "inline": true})
	}
	if m.Detail != "" {
		d := m.Detail
		if len(d) > 900 {
			d = d[:900] + "…"
		}
		fields = append(fields, map[string]any{"name": "Details", "value": d})
	}
	embed := map[string]any{
		"author":      map[string]any{"name": head},
		"title":       title,
		"description": desc,
		"color":       color,
		"fields":      fields,
		"footer":      map[string]any{"text": "Encorr · Request the encore."},
		"timestamp":   time.Now().UTC().Format(time.RFC3339),
	}
	if m.Event == EventTest {
		embed["title"] = head
		delete(embed, "author")
	}
	if m.URL != "" {
		embed["url"] = m.URL
	}
	if m.Poster != "" {
		embed["thumbnail"] = map[string]any{"url": m.Poster}
	}
	payload := map[string]any{"username": "Encorr", "embeds": []any{embed}}
	if m.MentionID != "" {
		payload["content"] = "<@" + m.MentionID + ">"
		payload["allowed_mentions"] = map[string]any{"users": []string{m.MentionID}}
	}
	b, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhook, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("couldn't reach Discord: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 500))
		if resp.StatusCode == 401 || resp.StatusCode == 404 {
			return fmt.Errorf("Discord says that webhook doesn't exist: copy it again from the channel's Integrations settings")
		}
		return fmt.Errorf("Discord answered %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}
