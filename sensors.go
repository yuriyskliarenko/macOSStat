package main

/*
#cgo LDFLAGS: -framework Foundation -framework IOKit
#include <stdlib.h>
#include "sensors.h"
*/
import "C"

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"unsafe"
)

var sensorsMu sync.Mutex // the SMC connection is shared

type tempGroup struct {
	title, names string
	match        func(string) bool
}

// Apple does not document individual PMU sensors, so group by name only.
var tempGroups = []tempGroup{
	{"Кристал SoC", "PMU tdie*, PMU TP*", func(n string) bool {
		return strings.HasPrefix(n, "PMU tdie") || strings.HasPrefix(n, "PMU TP")
	}},
	{"Навколо PMU", "PMU tdev*", func(n string) bool { return strings.HasPrefix(n, "PMU tdev") }},
	{"SSD (NAND)", "NAND*", func(n string) bool { return strings.HasPrefix(n, "NAND") }},
	{"Акумулятор", "gas gauge battery", func(n string) bool { return n == "gas gauge battery" }},
}

func (g tempGroup) values(temps map[string][]float64) (vals []float64) {
	for name, v := range temps {
		if g.match(name) {
			vals = append(vals, v...)
		}
	}
	return
}

func celsius(v float64) string { return fmt.Sprintf("%.1f °C", v) }

func readTemps() map[string][]float64 {
	var buf [256]C.TempReading
	n := int(C.readTemps(&buf[0], C.int(len(buf))))
	temps := map[string][]float64{}
	for _, t := range buf[:n] {
		name, v := C.GoString(&t.name[0]), float64(t.value)
		// tcal is a calibration constant; out-of-range values are absent sensors.
		if strings.HasSuffix(name, "tcal") || v <= 0 || v > 150 {
			continue
		}
		temps[name] = append(temps[name], v)
	}
	return temps
}

func stats(v []float64) (avg, top float64) {
	for _, x := range v {
		avg += x
		top = max(top, x)
	}
	return avg / float64(len(v)), top
}

func tempSections(temps map[string][]float64) []Section {
	sum := Section{Title: "Температури"}
	all := Section{Title: "Усі датчики температури"}
	for _, g := range tempGroups {
		vals := g.values(temps)
		if len(vals) == 0 {
			continue
		}
		avg, top := stats(vals)
		text := celsius(top)
		if len(vals) > 1 {
			text = fmt.Sprintf(tr("%s (сер. %.1f)"), celsius(top), avg)
		}
		pct := min(100, top)
		sum.Items = append(sum.Items, item(g.title, text, "HID: "+g.names, &pct))
	}
	names := make([]string, 0, len(temps))
	for n := range temps {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		v := temps[n]
		sort.Float64s(v)
		parts := make([]string, len(v))
		for i, x := range v {
			parts[i] = fmt.Sprintf("%.1f", x)
		}
		all.Items = append(all.Items, Item{Label: n, Value: strings.Join(parts, " / ") + " °C", Key: "HID: " + n})
	}
	return []Section{sum, all}
}

func smc(key string) (float64, bool) {
	k := C.CString(key)
	defer C.free(unsafe.Pointer(k))
	var v C.double
	ok := C.smcRead(k, &v) == 0
	return float64(v), ok
}

func fanSection() Section {
	s := Section{Title: "Вентилятори й потужність"}
	if C.smcOpen() != 0 {
		return s
	}
	defer C.smcClose()
	n, _ := smc("FNum")
	for i := 0; i < int(n) && i < 10; i++ {
		p := fmt.Sprintf("F%d", i)
		rpm, ok := smc(p + "Ac")
		if !ok {
			continue
		}
		it := Item{Label: fmt.Sprintf("Вентилятор %d", i+1), Value: fmt.Sprintf(tr("%.0f об/хв"), rpm), Key: "SMC: " + p + "Ac"}
		if rpm < 1 {
			it.Value = tr("зупинено")
		}
		if top, ok := smc(p + "Mx"); ok && top > 0 {
			it.Value += fmt.Sprintf(tr(" (макс. %.0f)"), top)
			pct := min(100, max(0, rpm/top*100))
			it.Percent = &pct
		}
		s.Items = append(s.Items, it)
	}
	if w, ok := smc("PSTR"); ok {
		s.Items = append(s.Items, Item{Label: "Споживання системи", Value: fmt.Sprintf(tr("%.1f Вт"), w), Key: "SMC: PSTR"})
	}
	return s
}

func sensorSections() []Section {
	sensorsMu.Lock()
	defer sensorsMu.Unlock()
	temps := tempSections(readTemps())
	return []Section{temps[0], fanSection(), temps[1]}
}
