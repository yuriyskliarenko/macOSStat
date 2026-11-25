package main

/*
#include <mach/mach.h>
#include <mach/mach_host.h>
static int vmCounters(uint64_t *o) {
    vm_statistics64_data_t v;
    mach_msg_type_number_t n = HOST_VM_INFO64_COUNT;
    mach_port_t h = mach_host_self();
    kern_return_t r = host_statistics64(h, HOST_VM_INFO64, (host_info64_t)&v, &n);
    mach_port_deallocate(mach_task_self(), h);
    if (r != KERN_SUCCESS) return -1;
    o[0] = v.swapins; o[1] = v.swapouts; o[2] = v.compressions; o[3] = v.decompressions;
    o[4] = v.pageins; o[5] = v.pageouts; o[6] = v.faults;
    for (int i = 0; i < 6; i++) o[i] *= vm_kernel_page_size;
    return 0;
}
*/
import "C"

import (
	"fmt"
	"os/exec"
	"sync"
	"time"
)

var ioErrors = []string{"Errors (Read)", "Errors (Write)"}

// counters are cumulative since boot.
type counters struct {
	at    time.Time
	v     map[string]float64
	bootd float64 // days since boot
}

type rateSet struct {
	per   map[string]float64 // per second
	total counters
	span  time.Duration
}

var sampler struct {
	sync.Mutex
	prev counters
	last rateSet
}

func diskCounters() (map[string]float64, error) {
	out, err := exec.Command("/usr/sbin/ioreg", "-r", "-c", "IOBlockStorageDriver", "-d1", "-w0").Output()
	if err != nil {
		return nil, fmt.Errorf("ioreg: %w", err)
	}
	total := map[string]float64{}
	for _, m := range ioStatRe.FindAllStringSubmatch(string(out), -1) {
		n, _ := number(m[2])
		total[m[1]] += n
	}
	return total, nil
}

func readCounters() (counters, error) {
	v, err := diskCounters()
	if err != nil {
		return counters{}, err
	}
	c := counters{at: time.Now(), v: v}
	var vm [7]C.uint64_t
	if C.vmCounters(&vm[0]) == 0 {
		for i, k := range []string{"swapin", "swapout", "compress", "decompress", "pagein", "pageout", "faults"} {
			v[k] = float64(vm[i])
		}
	}
	for name, n := range readIfCounters() {
		v["in "+name], v["out "+name] = n.in, n.out
	}
	out, _, err := runSysctl("kern.boottime")
	if err != nil {
		return c, err
	}
	if p := parseSysctl(out); len(p) == 1 {
		if t, ok := parseTime(p[0].Value); ok {
			c.bootd = c.at.Sub(t).Hours() / 24
		}
	}
	return c, nil
}

// rates computes speed between two samples; without a recent one it waits a second.
func rates() (rateSet, error) {
	sampler.Lock()
	defer sampler.Unlock()
	if sampler.last.per != nil && time.Since(sampler.last.total.at) < 700*time.Millisecond {
		return sampler.last, nil
	}
	if sampler.prev.v == nil || time.Since(sampler.prev.at) > 15*time.Second {
		c, err := readCounters()
		if err != nil {
			return rateSet{}, err
		}
		sampler.prev = c
		time.Sleep(time.Second)
	}
	c, err := readCounters()
	if err != nil {
		return rateSet{}, err
	}
	r := rateSet{map[string]float64{}, c, c.at.Sub(sampler.prev.at)}
	for k, n := range c.v {
		if old, ok := sampler.prev.v[k]; ok {
			r.per[k] = max(0, n-old) / r.span.Seconds()
		}
	}
	sampler.prev, sampler.last = c, r
	return r, nil
}

func perSec(n float64) string { return humanBytes(n) + tr("/с") }

func rateSections() ([]Section, error) {
	r, err := rates()
	if err != nil {
		return nil, err
	}
	build := func(title, src string, rows [][2]string) Section {
		s := Section{Title: title}
		for _, row := range rows {
			if n, ok := r.per[row[1]]; ok {
				it := item(row[0], perSec(n), src+row[1], nil)
				if row[1] == "faults" || row[1] == "Operations (Read)" || row[1] == "Operations (Write)" {
					it.Value = fmt.Sprintf(tr("%.0f/с"), n)
				}
				if row[1] == "swapin" || row[1] == "swapout" {
					it.Level = "ok"
					if n > 1<<20 {
						it.Level = "warn"
					}
				}
				s.Items = append(s.Items, it)
			}
		}
		return s
	}
	mem := build("Пам’ять зараз", "host_statistics64: ", [][2]string{
		{"Читання зі swap", "swapin"}, {"Запис у swap", "swapout"}, {"Стискання", "compress"},
		{"Розпакування", "decompress"}, {"Підкачка файлів (pagein)", "pagein"},
		{"Скидання (pageout)", "pageout"}, {"Page faults", "faults"}})
	disk := build("Диски зараз", "ioreg: ", [][2]string{
		{"Читання", "Bytes (Read)"}, {"Запис", "Bytes (Write)"},
		{"Операцій читання", "Operations (Read)"}, {"Операцій запису", "Operations (Write)"}})
	wear := Section{Title: "У середньому від завантаження"}
	if d := r.total.bootd; d > 0 {
		for _, row := range [][2]string{{"Записано на диски", "Bytes (Write)"}, {"Прочитано з дисків", "Bytes (Read)"}} {
			wear.Items = append(wear.Items, item(row[0], decimalGB(r.total.v[row[1]]/d)+tr(" на добу"), "ioreg: "+row[1], nil))
		}
		wear.Items = append(wear.Items, item("Записано у swap", decimalGB(r.total.v["swapout"]/d)+tr(" на добу"), "host_statistics64: swapouts", nil))
	}
	return []Section{mem, disk, wear}, nil
}
