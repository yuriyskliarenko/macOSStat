package main

import (
	"fmt"
	"strings"
)

// diagnosisSections applies simple rules; each item's Note explains its threshold.
func diagnosisSections() ([]Section, error) {
	params, err := readAll()
	if err != nil {
		return nil, err
	}
	vals := map[string]string{}
	for _, p := range params {
		vals[p.Key] = p.Value
	}
	num := func(k string) float64 { n, _ := number(vals[k]); return n }
	data, err := profile()
	if err != nil {
		return nil, err
	}
	r, err := rates()
	if err != nil {
		return nil, err
	}

	var items []Item
	add := func(level, label, value, note, key string) {
		items = append(items, Item{Label: label, Value: value, Key: key, Note: note, Level: level})
	}
	level := func(v, warn, bad float64) string {
		switch {
		case v >= bad:
			return "bad"
		case v >= warn:
			return "warn"
		}
		return "ok"
	}

	// Memory: kernel pressure plus whether swap is active right now.
	lv, text := "ok", "пам’яті вистачає"
	switch vals["kern.memorystatus_vm_pressure_level"] {
	case "2":
		lv, text = "warn", "пам’яті бракує, ядро стискає сторінки"
	case "4":
		lv, text = "bad", "критична нестача пам’яті"
	}
	comp := humanBytes(num("vm.compressor_bytes_used"))
	add(lv, "Пам’ять", fmt.Sprintf(tr("%s; компресор займає %s"), tr(text), comp),
		"Рівень тиску ядра: 1 — норма, 2 — підвищений, 4 — критичний.", "kern.memorystatus_vm_pressure_level")
	swap := r.per["swapin"] + r.per["swapout"]
	if m := swapRe.FindStringSubmatch(vals["vm.swapusage"]); m != nil {
		used, _ := number(m[2])
		switch {
		case swap >= 1<<20:
			lv, text = level(swap, 1<<20, 50<<20), tr("активний зараз: ")+perSec(swap)+tr(", це гальмує")
		case used > 0:
			lv, text = "ok", fmt.Sprintf(tr("зайнято %s, але зараз майже не активний"), humanBytes(used*(1<<20)))
		default:
			lv, text = "ok", "не використовується"
		}
		add(lv, "Swap", text, "Важлива швидкість, а не обсяг: увага від 1 МіБ/с, погано від 50 МіБ/с.", "host_statistics64: swapins, swapouts")
	}

	if cpus := num("hw.logicalcpu"); cpus > 0 {
		if f := strings.Fields(fBraces(vals["vm.loadavg"])); len(f) > 0 {
			load, _ := number(f[0])
			lv = level(load/cpus, 1, 2)
			text = map[string]string{"ok": "запас є", "warn": "черга довша за кількість ядер", "bad": "перевантажений"}[lv]
			add(lv, "Процесор", fmt.Sprintf(tr("%s: навантаження %.2f на %.0f ядер"), tr(text), load, cpus),
				"Середнє за 1 хв на ядро: увага від 1, погано від 2.", "vm.loadavg / hw.logicalcpu")
		}
	}

	sensorsMu.Lock()
	temps := readTemps()
	sensorsMu.Unlock()
	for _, t := range []struct {
		group     string
		warn, bad float64
	}{{"Кристал SoC", 85, 100}, {"SSD (NAND)", 70, 80}, {"Акумулятор", 40, 45}} {
		for _, g := range tempGroups {
			if v := g.values(temps); g.title == t.group && len(v) > 0 {
				_, top := stats(v)
				add(level(top, t.warn, t.bad), tr("Температура: ")+tr(g.title), celsius(top),
					fmt.Sprintf(tr("Орієнтири, не межі Apple: увага від %.0f °C, погано від %.0f °C."), t.warn, t.bad), "HID: "+g.names)
			}
		}
	}

	seen := map[string]bool{}
	for _, v := range data["SPStorageDataType"] {
		size, _ := v["size_in_bytes"].(float64)
		free, _ := v["free_space_in_bytes"].(float64)
		id := str(v, "bsd_name")
		if str(v, "file_system") == "APFS" {
			id = containerRe.FindString(id)
		}
		if size <= 0 || seen[id] || str(v, "writable") == "no" {
			continue
		}
		seen[id] = true
		used := (size - free) / size * 100
		add(level(used, 85, 95), tr("Місце: ")+str(v, "_name"), fmt.Sprintf(tr("зайнято %.0f%%, вільно %s"), used, decimalGB(free)),
			"Увага від 85%, погано від 95%.", "system_profiler: SPStorageDataType")
	}
	for _, c := range data["SPNVMeDataType"] {
		for _, d := range children(c, "_items") {
			if st := str(d, "smart_status"); st != "" {
				lv = "bad"
				if st == "Verified" {
					lv = "ok"
				}
				add(lv, "S.M.A.R.T. "+str(d, "bsd_name"), translate(st), "Погано, якщо стан не Verified.", "system_profiler: SPNVMeDataType")
			}
		}
	}
	errs := 0.0
	for _, k := range ioErrors {
		errs += r.total.v[k]
	}
	lv = "ok"
	if errs > 0 {
		lv = "warn"
	}
	add(lv, "Помилки дисків", fmt.Sprintf(tr("%.0f від завантаження"), errs), "Увага, якщо є хоч одна.", "ioreg: IOBlockStorageDriver")

	if b := readBattery(); b != nil {
		c := b.val("CycleCount")
		add(level(c, 800, 1000), "Цикли акумулятора", fmt.Sprintf(tr("%.0f із 1000"), c),
			"Розрахунковий ресурс сучасних MacBook — 1000 циклів.", "ioreg: CycleCount")
		if h, ok := number(strings.TrimSuffix(powerInfo().MaxCapacity, "%")); ok {
			add(level(100-h, 15, 20), "Ємність акумулятора", fmt.Sprintf(tr("%.0f%% від початкової"), h),
				"Увага від 85% і нижче, погано від 80% і нижче.", "system_profiler: SPPowerDataType")
		}
		if b.plugged() {
			lv, text = "ok", "потужності вистачає"
			if b.watts() < -1 {
				lv, text = "warn", fmt.Sprintf(tr("від мережі, а акумулятор віддає %.1f Вт: зарядка заслабка"), -b.watts())
			}
			add(lv, "Зарядка", text, "Увага, якщо акумулятор розряджається понад 1 Вт, хоча живлення підключене.", "ioreg: Voltage × Amperage")
		}
	}
	if m := num("kern.maxfiles"); m > 0 {
		u := num("kern.num_files") / m * 100
		add(level(u, 70, 90), "Відкриті файли", fmt.Sprintf(tr("%.0f%% ліміту"), u), "Увага від 70%, погано від 90%.", "kern.num_files / kern.maxfiles")
	}

	res := []Section{{Title: "Погано"}, {Title: "Варто уваги"}, {Title: "Усе гаразд"}}
	for _, it := range items {
		i := map[string]int{"bad": 0, "warn": 1, "ok": 2}[it.Level]
		res[i].Items = append(res[i].Items, it)
	}
	return res, nil
}
