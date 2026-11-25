package main

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// Capacity history: one record per 12 hours, only while the app runs.
const recordEvery = 12 * time.Hour

type capacityPoint struct {
	at                      time.Time
	cycles, nominal, design float64
}

var logMu sync.Mutex

func configPath() (string, error) {
	base, err := os.UserConfigDir() // ~/Library/Application Support
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "macOSStat"), nil
}

func dataDir() (string, error) {
	dir, err := configPath()
	if err != nil {
		return "", err
	}
	return dir, os.MkdirAll(dir, 0o700)
}

func loadBatteryLog() []capacityPoint {
	dir, err := dataDir()
	if err != nil {
		return nil
	}
	f, err := os.Open(filepath.Join(dir, "battery.csv"))
	if err != nil {
		return nil
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.FieldsPerRecord = 4
	var pts []capacityPoint
	for {
		rec, err := r.Read()
		if err != nil {
			return pts
		}
		at, err := time.Parse(time.RFC3339, rec[0])
		c, ok1 := number(rec[1])
		n, ok2 := number(rec[2])
		d, ok3 := number(rec[3])
		if err == nil && ok1 && ok2 && ok3 && d > 0 {
			pts = append(pts, capacityPoint{at, c, n, d})
		}
	}
}

func recordBattery(b *battery, now time.Time) {
	logMu.Lock()
	defer logMu.Unlock()
	nominal, design := b.val("NominalChargeCapacity"), b.val("DesignCapacity")
	if nominal <= 0 || design <= 0 {
		return
	}
	if pts := loadBatteryLog(); len(pts) > 0 && now.Sub(pts[len(pts)-1].at) < recordEvery {
		return
	}
	dir, err := dataDir()
	if err != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, "battery.csv"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	ff := func(v float64) string { return strconv.FormatFloat(v, 'f', 0, 64) }
	fmt.Fprintf(f, "%s,%s,%s,%s\n", now.Format(time.RFC3339), ff(b.val("CycleCount")), ff(nominal), ff(design))
}

// slope is the least-squares slope of the (x, y) pairs.
func slope(x, y []float64) float64 {
	var mx, my, num, den float64
	for i := range x {
		mx += x[i] / float64(len(x))
		my += y[i] / float64(len(y))
	}
	for i := range x {
		num += (x[i] - mx) * (y[i] - my)
		den += (x[i] - mx) * (x[i] - mx)
	}
	if den == 0 {
		return 0
	}
	return num / den
}

func forecastSections(b *battery, pts []capacityPoint) []Section {
	const src = "~/Library/Application Support/macOSStat/battery.csv"
	const target = 80.0 // Apple's service threshold
	s := Section{Title: "Прогноз акумулятора"}
	add := func(label, value, note string) {
		it := item(label, value, src, nil)
		it.Note = note
		s.Items = append(s.Items, it)
	}
	now, cycles := 0.0, b.val("CycleCount")
	if d := b.val("DesignCapacity"); d > 0 {
		now = b.val("NominalChargeCapacity") / d * 100
	}
	if lost := 100 - now; now > target && lost > 0 && cycles > 0 {
		add("До 80% лишилось циклів", fmt.Sprintf("≈ %.0f", (now-target)/(lost/cycles)),
			"Груба оцінка: втрата ємності від нового стану рівномірна за циклами.")
	}

	const needDays, needPoints = 14.0, 10
	days := 0.0
	if len(pts) > 1 {
		days = pts[len(pts)-1].at.Sub(pts[0].at).Hours() / 24
	}
	if days < needDays || len(pts) < needPoints {
		add("Тренд за часом", fmt.Sprintf(tr("збираю дані: %.0f із %.0f днів"), days, needDays),
			"Запис раз на 12 годин, поки програма відкрита чи стоїть у menu bar.")
		return []Section{s}
	}
	var x, y, cyc []float64
	for _, p := range pts {
		x = append(x, p.at.Sub(pts[0].at).Hours()/24)
		y = append(y, p.nominal/p.design*100)
		cyc = append(cyc, p.cycles)
	}
	perDay := slope(x, y)
	add("Зміна ємності", fmt.Sprintf(tr("%+.2f%% на місяць"), perDay*30), fmt.Sprintf(tr("Лінійний тренд за %.0f днів."), days))
	add("Циклів на місяць", fmt.Sprintf("%.1f", slope(x, cyc)*30), "")
	switch {
	case now <= target:
		add("80% ємності", "уже досягнуто", "")
	case perDay >= -0.001:
		add("80% ємності", "спаду поки не видно", "")
	default:
		at := time.Now().Add(time.Duration((now - target) / -perDay * 24 * float64(time.Hour)))
		add("80% ємності", tr("орієнтовно ")+at.Format("01.2006"), "Прогноз за трендом; що довша історія, то точніше.")
	}
	return []Section{s}
}
