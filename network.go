package main

/*
#cgo LDFLAGS: -framework CoreWLAN
#include "wifi.h"
*/
import "C"

import (
	"fmt"
	"net"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

type ifCounter struct{ in, out, speed, errs float64 }

func readIfCounters() map[string]ifCounter {
	var buf [128]C.IfCounters
	n := int(C.readIfCounters(&buf[0], C.int(len(buf))))
	res := map[string]ifCounter{}
	for _, c := range buf[:n] {
		res[C.GoString(&c.name[0])] = ifCounter{float64(c.in), float64(c.out), float64(c.speed), float64(c.inErr + c.outErr)}
	}
	return res
}

// bits formats a rate in bits per second.
func bits(bps float64) string {
	switch {
	case bps >= 1e9:
		return fmt.Sprintf(tr("%.2f Гбіт/с"), bps/1e9)
	case bps >= 1e6:
		return fmt.Sprintf(tr("%.1f Мбіт/с"), bps/1e6)
	}
	return fmt.Sprintf(tr("%.1f Кбіт/с"), bps/1e3)
}

var (
	routeRe = regexp.MustCompile(`(?m)^\s*(gateway|interface): (\S+)`)
	dnsRe   = regexp.MustCompile(`nameserver\[\d+\] : (\S+)`)
	portRe  = regexp.MustCompile(`Hardware Port: (.+)\nDevice: (\S+)`)
)

func run1(name string, args ...string) string {
	out, _ := exec.Command(name, args...).Output() // no network means empty output
	return string(out)
}

func defaultRoute() (iface, gateway string) {
	for _, m := range routeRe.FindAllStringSubmatch(run1("/sbin/route", "-n", "get", "default"), -1) {
		if m[1] == "interface" {
			iface = m[2]
		} else {
			gateway = m[2]
		}
	}
	return
}

func dnsServers() string {
	// scutil then repeats the same servers scoped to interfaces.
	first, _, _ := strings.Cut(run1("/usr/sbin/scutil", "--dns"), "for scoped queries")
	var list []string
	seen := map[string]bool{}
	for _, m := range dnsRe.FindAllStringSubmatch(first, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			list = append(list, m[1])
		}
	}
	return strings.Join(list, ", ")
}

func nameOf(m map[int]string, k int) string {
	if s, ok := m[k]; ok {
		return tr(s)
	}
	return fmt.Sprintf(tr("невідомо (%d)"), k)
}

func wifiSection() (s Section, name string) {
	var w C.WiFiInfo
	C.readWiFi(&w)
	if w.found == 0 {
		return
	}
	name = C.GoString(&w.name[0])
	s.Title = "Wi-Fi"
	add := func(label, value, key string) *Item {
		s.Items = append(s.Items, item(label, value, "CoreWLAN: "+key, nil))
		return &s.Items[len(s.Items)-1]
	}
	if w.power == 0 {
		add("Стан", "вимкнено", "powerOn")
		return
	}
	if w.channel == 0 || w.rate == 0 {
		add("Стан", "увімкнено, не підключено", "powerOn")
		return
	}
	add("Швидкість з’єднання", bits(float64(w.rate)*1e6), "transmitRate").Note =
		"Канальна швидкість передавання до роутера; реальна завжди нижча."
	add("Стандарт", nameOf(map[int]string{1: "802.11a", 2: "802.11b", 3: "802.11g", 4: "Wi-Fi 4 (802.11n)",
		5: "Wi-Fi 5 (802.11ac)", 6: "Wi-Fi 6/6E (802.11ax)", 7: "Wi-Fi 7 (802.11be)"}, int(w.phy)), "activePHYMode")
	ch := fmt.Sprintf("%d, %s", int(w.channel), nameOf(map[int]string{1: "2,4 ГГц", 2: "5 ГГц", 3: "6 ГГц"}, int(w.band)))
	if width, ok := map[int]string{1: "20 МГц", 2: "40 МГц", 3: "80 МГц", 4: "160 МГц"}[int(w.width)]; ok {
		ch += ", " + tr(width)
	}
	add("Канал", ch, "wlanChannel")
	rssi, noise := float64(w.rssi), float64(w.noise)
	if rssi < 0 {
		q := min(100, max(0, 2*(rssi+100)))
		it := add("Сигнал", fmt.Sprintf(tr("%.0f дБм"), rssi), "rssiValue")
		it.Percent, it.Level = &q, "ok"
		if rssi < -80 {
			it.Level = "bad"
		} else if rssi < -67 {
			it.Level = "warn"
		}
		it.Note = "Орієнтир: до −67 добре, нижче −80 погано."
		if noise < 0 {
			add("Шум", fmt.Sprintf(tr("%.0f дБм"), noise), "noiseMeasurement")
			add("Сигнал/шум (SNR)", fmt.Sprintf(tr("%.0f дБ"), rssi-noise), "rssiValue − noiseMeasurement")
		}
	}
	add("Потужність передавача", fmt.Sprintf(tr("%d мВт"), int(w.txPower)), "transmitPower")
	add("Захист", nameOf(map[int]string{0: "відкрита", 1: "WEP", 2: "WPA", 3: "WPA/WPA2", 4: "WPA2", 5: "WPA/WPA2/WPA3",
		6: "Dynamic WEP", 7: "WPA Enterprise", 8: "WPA/WPA2 Enterprise", 9: "WPA2 Enterprise", 10: "Enterprise",
		11: "WPA3", 12: "WPA3 Enterprise", 13: "WPA2/WPA3", 14: "OWE", 15: "OWE (перехідний)"}, int(w.security)), "security")
	if c := C.GoString(&w.country[0]); c != "" {
		add("Код країни", c, "countryCode")
	}
	return
}

// hardwarePorts is cached: port names hardly ever change.
var portCache struct {
	at    time.Time
	names map[string]string
}

func hardwarePorts() map[string]string { // call with netMu held
	if portCache.names == nil || time.Since(portCache.at) > time.Minute {
		portCache.names = map[string]string{}
		for _, m := range portRe.FindAllStringSubmatch(run1("/usr/sbin/networksetup", "-listallhardwareports"), -1) {
			portCache.names[m[2]] = m[1]
		}
		portCache.at = time.Now()
	}
	return portCache.names
}

var netMu sync.Mutex

func networkSections() ([]Section, error) {
	r, err := rates()
	if err != nil {
		return nil, err
	}
	netMu.Lock()
	defer netMu.Unlock()
	ports := hardwarePorts()
	def, gateway := defaultRoute()
	counts := readIfCounters()
	wifi, wifiName := wifiSection()

	title := func(name string) string {
		switch {
		case ports[name] != "":
			return ports[name] + " (" + name + ")"
		case strings.HasPrefix(name, "utun"), strings.HasPrefix(name, "ipsec"), strings.HasPrefix(name, "ppp"):
			return tr("Тунель ") + name
		}
		return name
	}
	main := Section{Title: "Підключення"}
	var others []Section
	if def == "" {
		main.Items = append(main.Items, item("Стан", "немає маршруту в інтернет", "route get default", nil))
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	for _, i := range ifaces {
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagLoopback != 0 {
			continue
		}
		var v4, v6 []string
		addrs, _ := i.Addrs()
		for _, a := range addrs {
			n, ok := a.(*net.IPNet)
			switch {
			case !ok || n.IP.IsLinkLocalUnicast():
			case n.IP.To4() != nil:
				v4 = append(v4, n.String())
			default:
				v6 = append(v6, n.IP.String())
			}
		}
		if len(v4)+len(v6) == 0 && i.Name != def {
			continue
		}
		s := Section{Title: "Інтерфейс " + title(i.Name)}
		add := func(label, value, key string) {
			if value != "" {
				s.Items = append(s.Items, item(label, value, key, nil))
			}
		}
		if i.Name == def {
			add("Інтерфейс", title(i.Name), "route get default")
		}
		add("IP-адреса", strings.Join(v4, ", "), "getifaddrs "+i.Name)
		if i.Name == def {
			if i.Name == wifiName && len(wifi.Items) > 0 && wifi.Items[0].Label == "Швидкість з’єднання" {
				add("Швидкість Wi-Fi", wifi.Items[0].Value, wifi.Items[0].Key)
			} else if sp := counts[i.Name].speed; sp > 0 {
				add("Швидкість лінка", bits(sp), "if_data64 "+i.Name)
			}
		}
		if n, ok := r.per["in "+i.Name]; ok {
			add("Завантаження ↓", bits(n*8), "if_data64 "+i.Name)
			add("Вивантаження ↑", bits(r.per["out "+i.Name]*8), "if_data64 "+i.Name)
		}
		if i.Name == def {
			add("Роутер", gateway, "route get default")
			add("DNS", dnsServers(), "scutil --dns")
		}
		for _, a := range v6 {
			add("IPv6", a, "getifaddrs "+i.Name)
		}
		add("MTU", fmt.Sprint(i.MTU), "getifaddrs "+i.Name)
		if c, ok := counts[i.Name]; ok {
			add("Отримано", decimalGB(c.in), "if_data64 "+i.Name)
			add("Надіслано", decimalGB(c.out), "if_data64 "+i.Name)
			add("Помилок", fmt.Sprintf("%.0f", c.errs), "if_data64 "+i.Name)
		}
		if i.Name == def {
			main.Items = append(main.Items, s.Items...)
		} else {
			others = append(others, s)
		}
	}
	return append([]Section{main, wifi}, others...), nil
}
