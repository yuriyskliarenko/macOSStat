package main

import (
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	ioregRe   = regexp.MustCompile(`"([A-Za-z]+)" ?= ?(\d+|Yes|No)\b`)
	profileRe = regexp.MustCompile(`"MaxCurrent"=(\d+),"MaxVoltage"=(\d+)`)
)

// battery holds raw AppleSmartBattery fields; top-level ones win over nested ones.
type battery struct {
	top, nested map[string]string
	raw         string
}

// readBattery returns nil on a Mac without a battery.
func readBattery() *battery {
	out, err := exec.Command("/usr/sbin/ioreg", "-r", "-n", "AppleSmartBattery", "-d1", "-w0").Output()
	if err != nil {
		return nil
	}
	b := &battery{map[string]string{}, map[string]string{}, string(out)}
	for _, line := range strings.Split(b.raw, "\n") {
		for i, m := range ioregRe.FindAllStringSubmatch(line, -1) {
			dst := b.nested
			if i == 0 && strings.HasPrefix(strings.TrimSpace(line), m[0]) {
				dst = b.top
			}
			if _, dup := dst[m[1]]; !dup {
				dst[m[1]] = m[2]
			}
		}
	}
	if _, ok := b.num("CycleCount"); !ok {
		return nil
	}
	return b
}

func (b *battery) num(key string) (float64, bool) {
	v, ok := b.top[key]
	if !ok {
		v = b.nested[key]
	}
	u, err := strconv.ParseUint(v, 10, 64)
	return float64(int64(u)), err == nil // ioreg prints negatives as uint64
}

func (b *battery) val(key string) float64 { n, _ := b.num(key); return n }

func (b *battery) plugged() bool { return b.top["ExternalConnected"] == "Yes" }

func (b *battery) charge() float64 {
	if m := b.val("MaxCapacity"); m > 0 {
		return min(100, b.val("CurrentCapacity")/m*100)
	}
	return 0
}

// watts is the battery power: negative while discharging.
func (b *battery) watts() float64 { return b.val("Voltage") * b.val("Amperage") / 1e6 }

func (b *battery) state() string {
	switch {
	case b.top["IsCharging"] == "Yes":
		return tr("заряджається")
	case b.top["FullyCharged"] == "Yes":
		return tr("заряджений")
	case b.plugged():
		return tr("від мережі, не заряджається")
	}
	return tr("розряджається")
}

func batterySections() []Section {
	b := readBattery()
	if b == nil {
		return nil
	}
	p := powerInfo()
	var res []Section
	var s Section
	add := func(label, value, key string, pct *float64) {
		s.Items = append(s.Items, item(label, value, "ioreg: "+key, pct))
	}
	info := func(label, value string) {
		if value != "" {
			s.Items = append(s.Items, item(label, value, "system_profiler: SPPowerDataType", nil))
		}
	}

	s = Section{Title: "Стан акумулятора"}
	add("Цикли заряджання", fmt.Sprintf("%.0f", b.val("CycleCount")), "CycleCount", nil)
	info("Макс. ємність (за даними macOS)", p.MaxCapacity)
	info("Стан", translate(p.Condition))
	info("Контролер", p.Controller)
	if design := b.val("DesignCapacity"); design > 0 {
		for _, f := range []struct{ label, key string }{
			{"Номінальна ємність", "NominalChargeCapacity"},
			{"Виміряна ємність", "AppleRawMaxCapacity"},
		} {
			if n, ok := b.num(f.key); ok {
				pct := min(100, n/design*100)
				add(f.label, fmt.Sprintf(tr("%.0f / %.0f мА·год (%.0f%%)"), n, design, n/design*100), f.key, &pct)
			}
		}
	}
	res = append(res, s)

	s = Section{Title: "Заряд і живлення"}
	pct := b.charge()
	add("Заряд", fmt.Sprintf("%.0f%%", pct), "CurrentCapacity", &pct)
	add("Стан", b.state(), "IsCharging", nil)
	if n, ok := b.num("TimeRemaining"); ok && n > 0 && n < 65535 {
		add("Залишилось часу", humanDuration(time.Duration(n)*time.Minute), "TimeRemaining", nil)
	}
	add("Напруга", fmt.Sprintf(tr("%.2f В"), b.val("Voltage")/1000), "Voltage", nil)
	add("Струм", fmt.Sprintf(tr("%.0f мА"), b.val("Amperage")), "Amperage", nil)
	add("Потужність (− розряд)", fmt.Sprintf(tr("%.1f Вт"), b.watts()), "Voltage × Amperage", nil)
	res = append(res, s)

	if b.plugged() {
		s = Section{Title: "Чесність зарядки"}
		info("Зарядний пристрій", p.Charger)
		negotiated, volts, amps := b.val("Watts"), b.val("AdapterVoltage")/1000, b.val("Current")/1000
		add("Домовлено", fmt.Sprintf(tr("%.0f Вт (%.0f В × %.2f А)"), negotiated, volts, amps), "AdapterDetails", nil)
		if in, ok := b.num("SystemPowerIn"); ok {
			in /= 1000
			it := item("Надходить на вхід", fmt.Sprintf(tr("%.1f Вт (%.2f В × %.2f А)"), in,
				b.val("SystemVoltageIn")/1000, b.val("SystemCurrentIn")/1000), "ioreg: PowerTelemetryData.SystemPowerIn", nil)
			if negotiated > 0 {
				u := min(100, max(0, in/negotiated*100))
				it.Percent = &u
			}
			it.Note = "Mac бере стільки, скільки йому треба; менше за домовлене — нормально."
			s.Items = append(s.Items, it)
			add("Споживає система", fmt.Sprintf(tr("%.1f Вт"), b.val("SystemLoad")/1000), "PowerTelemetryData.SystemLoad", nil)
			add("В акумулятор", fmt.Sprintf(tr("%.1f Вт"), b.watts()), "Voltage × Amperage", nil)
			if v := b.val("SystemVoltageIn") / 1000; volts > 0 && v > 0 {
				d := item("Просідання напруги", fmt.Sprintf(tr("%.2f В"), max(0, volts-v)),
					"ioreg: AdapterVoltage − SystemVoltageIn", nil)
				d.Level, d.Note = "ok", "Понад 5% від домовленої напруги під навантаженням натякає на слабкий кабель чи зарядку."
				if v < volts*.95 {
					d.Level = "warn"
				}
				s.Items = append(s.Items, d)
			}
		}
		var modes []string
		for _, m := range profileRe.FindAllStringSubmatch(b.raw, -1) {
			a, _ := number(m[1])
			v, _ := number(m[2])
			mode := fmt.Sprintf(tr("%.0f В × %.2f А"), v/1000, a/1000)
			if !strings.Contains(strings.Join(modes, "\n"), mode) {
				modes = append(modes, mode)
			}
		}
		add("Профілі зарядки", strings.Join(modes, "; "), "AdapterDetails.UsbHvcMenu", nil)
		if t := b.val("TimeChargingThermallyLimited"); t > 0 {
			add("Обмежено нагрівом", humanDuration(time.Duration(t)*time.Second), "ChargerData.TimeChargingThermallyLimited", nil)
		}
		for _, f := range []struct{ label, key string }{
			{"Код «не заряджається»", "NotChargingReason"}, {"Код «заряджається повільно»", "SlowChargingReason"},
		} {
			if n := b.val(f.key); n > 0 {
				it := item(f.label, fmt.Sprintf("0x%X", int64(n)), "ioreg: ChargerData."+f.key, nil)
				it.Note = "Apple не документує значення кодів."
				s.Items = append(s.Items, it)
			}
		}
		if t := b.val("UpdateTime"); t > 0 {
			add("Дані контролера від", time.Unix(int64(t), 0).Format("15:04:05"), "UpdateTime", nil)
		}
		res = append(res, s)
	}
	recordBattery(b, time.Now())
	return append(res, forecastSections(b, loadBatteryLog())...)
}
