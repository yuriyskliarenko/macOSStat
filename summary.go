package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Item is one row. Percent (0–100) is set only for values with a gauge.
type Item struct {
	Label   string   `json:"label"`
	Value   string   `json:"value"`
	Key     string   `json:"key"` // where the value comes from
	Percent *float64 `json:"percent,omitempty"`
	Note    string   `json:"note,omitempty"`  // explanation below the row
	Level   string   `json:"level,omitempty"` // ok, warn or bad
}

func item(label, value, key string, pct *float64) Item {
	return Item{Label: label, Value: value, Key: key, Percent: pct}
}

type Section struct {
	Title string `json:"title"`
	Items []Item `json:"items"`
}

type formatter func(string) string

// spec describes a parameter shown almost as is.
type spec struct {
	label, key string
	format     formatter
}

func number(s string) (float64, bool) {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return f, err == nil
}

func humanBytes(b float64) string {
	units := []string{"Б", "КіБ", "МіБ", "ГіБ", "ТіБ", "ПіБ"}
	i := 0
	for b >= 1024 && i < len(units)-1 {
		b /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%.0f %s", b, tr(units[i]))
	}
	return fmt.Sprintf("%.1f %s", b, tr(units[i]))
}

func numeric(f func(float64) string) formatter {
	return func(s string) string {
		if n, ok := number(s); ok {
			return f(n)
		}
		return s
	}
}

var (
	fBytes = numeric(humanBytes)
	fMHz   = numeric(func(n float64) string { return fmt.Sprintf(tr("%.0f МГц"), n/1e6) })
	fMs    = numeric(func(n float64) string { return humanDuration(time.Duration(n) * time.Millisecond) })
	fBool  = numeric(func(n float64) string {
		if n != 0 {
			return "так"
		}
		return "ні"
	})
)

func humanDuration(d time.Duration) string {
	d = d.Round(time.Second)
	days, d := d/(24*time.Hour), d%(24*time.Hour)
	h, m, s := d/time.Hour, d%time.Hour/time.Minute, d%time.Minute/time.Second
	switch {
	case days > 0:
		return fmt.Sprintf(tr("%d д %d год %d хв"), days, h, m)
	case h > 0:
		return fmt.Sprintf(tr("%d год %d хв"), h, m)
	case m > 0:
		return fmt.Sprintf(tr("%d хв %d с"), m, s)
	}
	return fmt.Sprintf(tr("%d с"), s)
}

var secRe = regexp.MustCompile(`sec = (\d+)`)

func parseTime(s string) (time.Time, bool) {
	m := secRe.FindStringSubmatch(s)
	if m == nil {
		return time.Time{}, false
	}
	n, err := strconv.ParseInt(m[1], 10, 64)
	return time.Unix(n, 0), err == nil && n > 0
}

func fTime(s string) string {
	if t, ok := parseTime(s); ok {
		return t.Format("02.01.2006 15:04:05")
	}
	return "—"
}

func fPressure(s string) string {
	switch s {
	case "1":
		return "нормальний"
	case "2":
		return "підвищений"
	case "4":
		return "критичний"
	}
	return s
}

func fBraces(s string) string { return strings.Trim(s, "{} ") }

var swapRe = regexp.MustCompile(`total = ([\d.]+)M\s+used = ([\d.]+)M`)

func buildSummary(params []Param, now time.Time) []Section {
	vals := make(map[string]string, len(params))
	for _, p := range params {
		vals[p.Key] = p.Value
	}
	num := func(key string) (float64, bool) { return number(vals[key]) }

	var sections []Section
	var cur *Section
	section := func(title string) {
		sections = append(sections, Section{Title: title})
		cur = &sections[len(sections)-1]
	}
	add := func(specs ...spec) {
		for _, s := range specs {
			v := vals[s.key]
			if v == "" {
				continue // absent on this machine or macOS version
			}
			if s.format != nil {
				v = s.format(v)
			}
			cur.Items = append(cur.Items, Item{Label: s.label, Value: v, Key: s.key})
		}
	}
	// gauge adds "used / total" with a bar.
	gauge := func(label, key string, used, total float64, format func(float64) string) {
		if total <= 0 {
			return
		}
		pct := min(100, max(0, used/total*100))
		cur.Items = append(cur.Items, item(label,
			fmt.Sprintf("%s / %s (%.0f%%)", format(used), format(total), pct), key, &pct))
	}
	count := func(n float64) string { return strconv.FormatFloat(n, 'f', 0, 64) }

	section("macOS")
	add(spec{"Версія macOS", "kern.osproductversion", nil},
		spec{"Збірка", "kern.osversion", nil},
		spec{"Ядро Darwin", "kern.osrelease", nil},
		spec{"Ім’я хоста", "kern.hostname", nil},
		spec{"Віртуальна машина", "kern.hv_vmm_present", fBool})

	section("Сон і завантаження")
	add(spec{"Завантажено", "kern.boottime", fTime})
	if t, ok := parseTime(vals["kern.boottime"]); ok {
		cur.Items = append(cur.Items, Item{Label: "Час роботи", Value: humanDuration(now.Sub(t)), Key: "kern.boottime"})
	}
	add(spec{"Останній сон", "kern.sleeptime", fTime},
		spec{"Останнє пробудження", "kern.waketime", fTime},
		spec{"Причина пробудження", "kern.wakereason", nil},
		spec{"Причина вимкнення", "kern.shutdownreason", strings.TrimSpace})

	section("Процесор")
	add(spec{"Процесор", "machdep.cpu.brand_string", nil},
		spec{"Фізичних ядер", "hw.physicalcpu", nil},
		spec{"Логічних ядер", "hw.logicalcpu", nil},
		spec{"Частота", "hw.cpufrequency", fMHz},
		spec{"Навантаження 1/5/15 хв", "vm.loadavg", fBraces},
		spec{"Планувальник", "kern.sched", nil})
	levels, _ := num("hw.nperflevels")
	for i := 0; i < int(levels); i++ {
		p := fmt.Sprintf("hw.perflevel%d.", i)
		name := vals[p+"name"]
		section("Ядра " + name)
		add(spec{"Кількість", p + "logicalcpu", nil},
			spec{"L1i", p + "l1icachesize", fBytes},
			spec{"L1d", p + "l1dcachesize", fBytes},
			spec{"L2 (кластер)", p + "l2cachesize", fBytes})
	}
	section("Кеш")
	if levels == 0 {
		add(spec{"L1i", "hw.l1icachesize", fBytes}, spec{"L1d", "hw.l1dcachesize", fBytes},
			spec{"L2", "hw.l2cachesize", fBytes}, spec{"L3", "hw.l3cachesize", fBytes})
	}
	add(spec{"Рядок кешу", "hw.cachelinesize", fBytes})

	section("Використання пам’яті")
	add(spec{"Усього", "hw.memsize", fBytes},
		spec{"Тиск на пам’ять", "kern.memorystatus_vm_pressure_level", fPressure})
	if n, ok := num("kern.memorystatus_level"); ok {
		cur.Items = append(cur.Items, item("Доступно за оцінкою ядра", fmt.Sprintf("%.0f%%", n), "kern.memorystatus_level", &n))
	}
	mem, _ := num("hw.memsize")
	page, _ := num("vm.pagesize")
	for _, g := range []struct {
		label, key string
		mult       float64
	}{
		{"Анонімна (програми)", "vm.page_pageable_internal_count", page},
		{"Закріплена (wired)", "vm.page_wired_count", page},
		{"Компресор", "vm.compressor_bytes_used", 1},
		{"Файловий кеш", "vm.page_pageable_external_count", page},
		{"Вільна", "vm.page_free_count", page},
	} {
		if n, ok := num(g.key); ok {
			gauge(g.label, g.key, n*g.mult, mem, humanBytes)
		}
	}
	if m := swapRe.FindStringSubmatch(vals["vm.swapusage"]); m != nil {
		total, _ := number(m[1])
		used, _ := number(m[2])
		if total > 0 {
			gauge("Swap", "vm.swapusage", used*(1<<20), total*(1<<20), humanBytes)
		} else {
			cur.Items = append(cur.Items, Item{Label: "Swap", Value: "не використовується", Key: "vm.swapusage"})
		}
	}
	add(spec{"Розмір сторінки", "vm.pagesize", fBytes},
		spec{"Ліміт wired для GPU, МіБ (0 — типовий)", "iogpu.wired_limit_mb", nil})

	section("Файли")
	files, okF := num("kern.num_files")
	maxFiles, _ := num("kern.maxfiles")
	if okF {
		gauge("Відкриті файли", "kern.num_files", files, maxFiles, count)
	}
	add(spec{"Файлів на процес", "kern.maxfilesperproc", nil},
		spec{"Vnodes виділено", "kern.num_vnodes", nil},
		spec{"Vnodes вільних", "kern.free_vnodes", nil})

	section("Процеси й потоки")
	add(spec{"Макс. процесів", "kern.maxproc", nil},
		spec{"Макс. процесів на користувача", "kern.maxprocperuid", nil},
		spec{"Макс. потоків", "kern.num_threads", nil},
		spec{"Макс. потоків на процес", "kern.num_taskthreads", nil})

	section("Інші ліміти")
	add(spec{"Макс. розмір аргументів", "kern.argmax", fBytes},
		spec{"Макс. псевдотерміналів", "kern.tty.ptmx_max", nil},
		spec{"Макс. сегмент shm", "kern.sysv.shmmax", fBytes})

	section("З’єднання")
	add(spec{"TCP-з’єднань (PCB)", "net.inet.tcp.pcbcount", nil},
		spec{"UDP-сокетів (PCB)", "net.inet.udp.pcbcount", nil},
		spec{"Черга listen (somaxconn)", "kern.ipc.somaxconn", nil})

	section("TCP/IP")
	add(spec{"IPv4 forwarding", "net.inet.ip.forwarding", fBool},
		spec{"IPv6 forwarding", "net.inet6.ip6.forwarding", fBool},
		spec{"TCP keepalive idle", "net.inet.tcp.keepidle", fMs},
		spec{"TCP keepalive інтервал", "net.inet.tcp.keepintvl", fMs},
		spec{"Типовий MSS", "net.inet.tcp.mssdflt", nil},
		spec{"TTL", "net.inet.ip.ttl", nil},
		spec{"Перший ефемерний порт", "net.inet.ip.portrange.first", nil},
		spec{"Останній ефемерний порт", "net.inet.ip.portrange.last", nil})

	section("Буфери")
	add(spec{"Макс. буфер сокета", "kern.ipc.maxsockbuf", fBytes},
		spec{"TCP буфер надсилання", "net.inet.tcp.sendspace", fBytes},
		spec{"TCP буфер приймання", "net.inet.tcp.recvspace", fBytes})

	section("Безпека")
	add(spec{"Secure level", "kern.securelevel", nil},
		spec{"Lockdown Mode", "security.mac.lockdown_mode_state", fBool},
		spec{"Режим розробника", "security.mac.amfi.developer_mode_status", fBool},
		spec{"Gatekeeper (політика ASP)", "security.mac.asp.policy.gatekeeper_enabled", fBool},
		spec{"Launch constraints", "security.mac.amfi.launch_constraints_enforced", fBool},
		spec{"Pointer authentication", "machdep.ptrauth_enabled", fBool},
		spec{"KASLR (slide)", "kern.slide", fBool},
		spec{"Безпечний режим", "kern.safeboot", fBool},
		spec{"Гіпервізор підтримується", "kern.hv_support", fBool})

	return sections
}
