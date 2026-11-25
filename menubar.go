package main

import (
	"fmt"
	"time"
)

var lastRecordCheck time.Time // only used by menuStatus, which is called serially

type menuInfo struct {
	Title string   `json:"title"`
	Lines []string `json:"lines"`
}

// menuStatus uses fast sources only: it runs every few seconds.
func menuStatus() menuInfo {
	m := menuInfo{Title: "macOSStat"}
	title := ""
	sensorsMu.Lock()
	temps, fans := tempSections(readTemps()), fanSection()
	sensorsMu.Unlock()
	for _, it := range temps[0].Items {
		m.Lines = append(m.Lines, trName(it.Label)+": "+tr(it.Value))
		if it.Label == "Кристал SoC" && it.Percent != nil {
			title = fmt.Sprintf("%.0f°", *it.Percent)
		}
	}
	for _, it := range fans.Items {
		m.Lines = append(m.Lines, trName(it.Label)+": "+tr(it.Value))
		if it.Key == "SMC: PSTR" {
			title += " " + it.Value
		}
	}
	params, _ := readKeys([]string{"kern.memorystatus_vm_pressure_level", "vm.swapusage"})
	for _, p := range params {
		switch p.Key {
		case "kern.memorystatus_vm_pressure_level":
			m.Lines = append(m.Lines, tr("Тиск на пам’ять: ")+tr(fPressure(p.Value)))
			if mark := map[string]string{"2": "🟡 ", "4": "🔴 "}[p.Value]; mark != "" {
				title = mark + title
			}
		case "vm.swapusage":
			if s := swapRe.FindStringSubmatch(p.Value); s != nil {
				used, _ := number(s[2])
				m.Lines = append(m.Lines, "Swap: "+humanBytes(used*(1<<20)))
			}
		}
	}
	if b := readBattery(); b != nil {
		if now := time.Now(); now.Sub(lastRecordCheck) > 10*time.Minute {
			lastRecordCheck = now
			recordBattery(b, now)
		}
		m.Lines = append(m.Lines, fmt.Sprintf(tr("Акумулятор: %.0f%%, %s"), b.charge(), b.state()))
	}
	if title != "" {
		m.Title = title
	}
	return m
}
