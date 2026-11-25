package main

import (
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type powerEvent struct {
	at     time.Time
	kind   string // Sleep, Wake, DarkWake, Start (after a reboot)
	reason string
	onBatt bool
	charge int // -1 if unknown
	by     string
}

var (
	pmLineRe  = regexp.MustCompile(`^(\d{4}-\d\d-\d\d \d\d:\d\d:\d\d [+-]\d{4}) (Sleep|Wake|DarkWake|Wake Requests|Start)\s*\t(.*)$`)
	pmCauseRe = regexp.MustCompile(`due to '?(.+?)'?(?::TCPKeepAlive\S*)? Using (\S+?) ?\(Charge: ?(\d+)%\)`)
	pmNoiseRe = regexp.MustCompile(`NUB\.\S+ |nub-spmi\S* |smc\.sysState\.Wake\(\w+\) |SMC\.OutboxNotEmpty/?`)
	pmReqRe   = regexp.MustCompile(`\[\*process=(\S+) request=(\S+)[^\]]*?(?:info="([^"]*)")?\]`)
	pmUserRe  = regexp.MustCompile(`:\d+:|,\d+$`)
)

func parsePowerLog(log string) []powerEvent {
	var events []powerEvent
	pending := ""
	for _, line := range strings.Split(log, "\n") {
		m := pmLineRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		if m[2] == "Wake Requests" {
			// The asterisk marks the request the timer is set for.
			pending = ""
			if r := pmReqRe.FindStringSubmatch(m[3]); r != nil {
				pending = r[1] + " / " + r[2]
				if r[3] != "" {
					pending += ": " + strings.TrimSpace(pmUserRe.ReplaceAllString(r[3], " "))
				}
			}
			continue
		}
		at, err := time.Parse("2006-01-02 15:04:05 -0700", m[1])
		if err != nil {
			continue
		}
		e := powerEvent{at: at, kind: m[2], charge: -1}
		if c := pmCauseRe.FindStringSubmatch(m[3]); c != nil {
			e.reason = strings.TrimSpace(pmNoiseRe.ReplaceAllString(c[1], ""))
			e.onBatt = strings.EqualFold(c[2], "batt")
			e.charge, _ = strconv.Atoi(c[3])
		}
		if e.kind != "Sleep" && e.kind != "Start" && strings.Contains(e.reason, "rtc/") {
			e.by, pending = pending, ""
		}
		events = append(events, e)
	}
	return events
}

var pmCache struct {
	sync.Mutex
	at     time.Time
	events []powerEvent
}

// powerEvents caches the log: pmset takes about two seconds to read it.
func powerEvents() ([]powerEvent, error) {
	pmCache.Lock()
	defer pmCache.Unlock()
	if !pmCache.at.IsZero() && time.Since(pmCache.at) < 5*time.Minute {
		return pmCache.events, nil
	}
	out, err := exec.Command("/usr/bin/pmset", "-g", "log").Output()
	if err != nil {
		return nil, fmt.Errorf("pmset -g log: %w", err)
	}
	pmCache.events, pmCache.at = parsePowerLog(string(out)), time.Now()
	return pmCache.events, nil
}

type counted struct {
	name string
	n    int
}

func topCounts(m map[string]int, limit int) []counted {
	var res []counted
	for k, n := range m {
		res = append(res, counted{k, n})
	}
	sort.Slice(res, func(i, j int) bool {
		return res[i].n > res[j].n || res[i].n == res[j].n && res[i].name < res[j].name
	})
	return res[:min(limit, len(res))]
}

func wakeSections() ([]Section, error) {
	events, err := powerEvents()
	if err != nil {
		return nil, err
	}
	const src = "pmset -g log"
	var res []Section
	now := time.Now()
	for _, p := range []struct {
		title string
		span  time.Duration
	}{{"За добу", 24 * time.Hour}, {"За 7 днів", 7 * 24 * time.Hour}} {
		var dark, full, lost int
		var asleep time.Duration
		for i, e := range events {
			if now.Sub(e.at) > p.span {
				continue
			}
			switch e.kind {
			case "DarkWake":
				dark++
			case "Wake":
				full++
			}
			if i == 0 || e.kind == "Start" || events[i-1].kind == "Wake" || events[i-1].kind == "Start" {
				continue // the Mac was awake before this
			}
			prev := events[i-1]
			if prev.kind == "Sleep" {
				asleep += e.at.Sub(prev.at)
			}
			if prev.onBatt && e.onBatt && prev.charge >= e.charge && e.charge >= 0 {
				lost += prev.charge - e.charge
			}
		}
		s := Section{Title: p.title, Items: []Item{
			item("Повних пробуджень", strconv.Itoa(full), src, nil),
			item("Темних пробуджень", strconv.Itoa(dark), src, nil),
			item("Час у сні", humanDuration(asleep), src, nil),
			item("Втрачено заряду уві сні", fmt.Sprintf("%d%%", lost), src, nil)}}
		s.Items[1].Note = "Екран вимкнений: система прокидається для фонових справ."
		if h := asleep.Hours(); h >= 1 {
			it := item("Темних на годину сну", fmt.Sprintf("%.1f", float64(dark)/h), src, nil)
			s.Items = append(s.Items, it)
		}
		res = append(res, s)
	}

	reasons, who := map[string]int{}, map[string]int{}
	for _, e := range events {
		if e.kind == "Sleep" || e.kind == "Start" || now.Sub(e.at) > 7*24*time.Hour {
			continue
		}
		if e.reason != "" {
			reasons[e.reason]++
		}
		if e.by != "" {
			who[e.by]++
		}
	}
	for _, g := range []struct {
		title string
		m     map[string]int
	}{{"Хто будив за таймером (7 днів)", who}, {"Причини (7 днів)", reasons}} {
		s := Section{Title: g.title}
		for _, c := range topCounts(g.m, 8) {
			s.Items = append(s.Items, item(c.name, strconv.Itoa(c.n), src, nil))
		}
		res = append(res, s)
	}
	last := Section{Title: "Останні повні пробудження"}
	for i := len(events) - 1; i >= 0 && len(last.Items) < 8; i-- {
		if e := events[i]; e.kind == "Wake" {
			v := e.reason
			if e.charge >= 0 {
				v += fmt.Sprintf(" (%d%%)", e.charge)
			}
			last.Items = append(last.Items, item(e.at.Local().Format("02.01 15:04"), v, src, nil))
		}
	}
	return append(res, last), nil
}
