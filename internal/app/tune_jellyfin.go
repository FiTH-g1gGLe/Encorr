package app

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// Jellyfin's Tune-up checks. Library settings live per library in
// Jellyfin: Apply changes the libraries Encorr watches that aren't set yet,
// and Undo puts each of those back exactly as it was.

func (a *App) readJellyfin(ctx context.Context, e *tuneEnv) error {
	jf := a.Jellyfin()
	info, err := jf.Ping(ctx)
	if err != nil {
		return err
	}
	libs, err := jf.Libraries(ctx)
	if err != nil {
		return err
	}
	watched := map[string]bool{}
	for _, l := range a.jfLibraries() {
		watched[l.Key] = true
	}
	names := map[string]string{}
	e.mu.Lock()
	if e.raw["jellyfin"] == nil {
		e.raw["jellyfin"] = map[string]map[string]any{}
	}
	for _, l := range libs {
		if !watched[l.ItemID] || l.LibraryOptions == nil {
			continue
		}
		e.raw["jellyfin"]["lib:"+l.ItemID] = l.LibraryOptions
		names[l.ItemID] = l.Name
	}
	e.jfNames = names
	e.apps["jellyfin"].Connected = true
	e.apps["jellyfin"].Version = info.Version
	e.mu.Unlock()
	if cfg, err := jf.Config(ctx); err == nil {
		if v, ok := cfg["QuickConnectAvailable"].(bool); ok {
			e.set("jellyfin", "cfg.QuickConnectAvailable", v)
		}
	}
	return nil
}

// jfOff lists the watched libraries whose field isn't want yet.
func jfOff(e *tuneEnv, field string, want bool) []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	var ids []string
	for k, opts := range e.raw["jellyfin"] {
		id, ok := strings.CutPrefix(k, "lib:")
		if !ok {
			continue
		}
		if cur, isBool := opts[field].(bool); isBool && cur != want {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

// setJfLibs sets field on the given libraries, sending each library's whole
// options object back (Jellyfin replaces it), then reads them back.
func (a *App) setJfLibs(ctx context.Context, vals map[string]bool, field string) error {
	jf := a.Jellyfin()
	if jf == nil {
		return userErr("Jellyfin isn't connected.")
	}
	libs, err := jf.Libraries(ctx)
	if err != nil {
		return err
	}
	for _, l := range libs {
		want, ok := vals[l.ItemID]
		if !ok || l.LibraryOptions == nil {
			continue
		}
		cp := map[string]any{}
		for k, v := range l.LibraryOptions {
			cp[k] = v
		}
		cp[field] = want
		if err := jf.SetLibraryOptions(ctx, l.ItemID, cp); err != nil {
			return err
		}
	}
	libs, err = jf.Libraries(ctx)
	if err != nil {
		return err
	}
	for _, l := range libs {
		if want, ok := vals[l.ItemID]; ok {
			if got, _ := l.LibraryOptions[field].(bool); got != want {
				return fmt.Errorf("Jellyfin didn't keep the change for %s", l.Name)
			}
		}
	}
	return nil
}

// jfLibRule is a setting Jellyfin keeps per library.
func jfLibRule(id, area, level, field string, want bool, title, why string) tuneRule {
	return tuneRule{id: id, app: "jellyfin", area: area, level: level, title: title, why: why,
		check: func(e *tuneEnv) []TuneFinding {
			off := jfOff(e, field, want)
			e.mu.Lock()
			total := 0
			for k := range e.raw["jellyfin"] {
				if strings.HasPrefix(k, "lib:") {
					total++
				}
			}
			names := e.jfNames
			e.mu.Unlock()
			if total == 0 {
				return nil
			}
			f := TuneFinding{Status: "ok", Current: onOff(want), Recommended: onOff(want)}
			if len(off) > 0 {
				var ns []string
				for _, id := range off {
					if n := names[id]; n != "" {
						ns = append(ns, n)
					}
				}
				f.Status, f.Current = "change", onOff(!want)
				if len(ns) > 0 && len(off) < total {
					f.Current += " in " + strings.Join(ns, ", ")
				}
			}
			return []TuneFinding{f}
		},
		action: func(ctx context.Context, a *App, e *tuneEnv) (map[string]any, string, error) {
			off := jfOff(e, field, want)
			if len(off) == 0 {
				return nil, "", userErr("That's already set.")
			}
			vals, undo := map[string]bool{}, map[string]any{"field": field}
			for _, id := range off {
				vals[id] = want
				undo[id] = !want
			}
			if err := a.setJfLibs(ctx, vals, field); err != nil {
				// Put back anything that did change.
				back := map[string]bool{}
				for _, id := range off {
					back[id] = !want
				}
				_ = a.setJfLibs(ctx, back, field)
				return nil, "", err
			}
			return undo, fmt.Sprintf("%s in %d librar%s", onOff(want), len(off), map[bool]string{true: "y", false: "ies"}[len(off) == 1]), nil
		},
		undo: func(ctx context.Context, a *App, data map[string]any) error {
			f, _ := data["field"].(string)
			if f == "" {
				return userErr("That one can't be undone.")
			}
			vals := map[string]bool{}
			for k, v := range data {
				if b, ok := v.(bool); ok && k != "field" {
					vals[k] = b
				}
			}
			return a.setJfLibs(ctx, vals, f)
		}}
}

func jellyfinRules() []tuneRule {
	return []tuneRule{
		jfLibRule("jf-realtime", "Library", "safe", "EnableRealtimeMonitor", true,
			"Watch library folders for new files",
			"New films and episodes appear in Jellyfin within moments of being imported, instead of waiting for the next scheduled scan."),
		jfLibRule("jf-trickplay", "Housekeeping", "review", "ExtractTrickplayImagesDuringLibraryScan", false,
			"Make scrubbing previews after scans, not during them",
			"Building preview images during a scan is heavy and can hold new arrivals back for a long time. Jellyfin's scheduled task still makes them, just later."),
		jfLibRule("jf-chapters", "Housekeeping", "review", "ExtractChapterImagesDuringLibraryScan", false,
			"Make chapter images after scans, not during them",
			"Same idea: heavy work moves out of the scan, so new things show up sooner."),
		{id: "jf-quickconnect", app: "jellyfin", area: "Library", level: "info",
			title: "Let friends sign in with a Quick Connect code",
			check: func(e *tuneEnv) []TuneFinding {
				v, ok := e.get("jellyfin", "cfg.QuickConnectAvailable")
				if !ok || v == true {
					return nil
				}
				return []TuneFinding{{Status: "notice", Current: "Off", Recommended: "On",
					Why: "With Quick Connect, friends sign in to Encorr by typing a code into a Jellyfin app they already use: no password to remember.",
					Fix: "In Jellyfin, open Dashboard → General and tick \"Enable Quick Connect\". Encorr leaves sign-in settings to you."}}
			}},
	}
}
