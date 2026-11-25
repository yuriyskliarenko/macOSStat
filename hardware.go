package main

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"time"
)

type obj = map[string]any

const hardwareTTL = time.Minute // system_profiler takes about 0.5 s

var hw struct {
	sync.Mutex
	at   time.Time
	data map[string][]obj
}

// profile reads system_profiler; serial numbers and UUIDs are never shown.
func profile() (map[string][]obj, error) {
	hw.Lock()
	defer hw.Unlock()
	if hw.data != nil && time.Since(hw.at) < hardwareTTL {
		return hw.data, nil
	}
	out, err := exec.Command("/usr/sbin/system_profiler", "-json",
		"SPHardwareDataType", "SPMemoryDataType", "SPDisplaysDataType", "SPNVMeDataType",
		"SPStorageDataType", "SPPowerDataType", "SPAudioDataType", "SPCameraDataType",
		"SPUSBHostDataType", "SPThunderboltDataType").Output()
	if err != nil {
		return nil, fmt.Errorf("system_profiler: %w", err)
	}
	var data map[string][]obj
	if err := json.Unmarshal(out, &data); err != nil {
		return nil, fmt.Errorf("system_profiler: %w", err)
	}
	hw.data, hw.at = data, time.Now()
	return data, nil
}

func str(o obj, key string) string {
	if v, ok := o[key]; ok && v != nil {
		return fmt.Sprint(v)
	}
	return ""
}

func children(o obj, key string) []obj {
	list, _ := o[key].([]any)
	var res []obj
	for _, x := range list {
		if c, ok := x.(obj); ok {
			res = append(res, c)
		}
	}
	return res
}

type power struct{ MaxCapacity, Condition, Controller, Charger string }

func powerInfo() (p power) {
	data, err := profile()
	if err != nil {
		return
	}
	for _, o := range data["SPPowerDataType"] {
		if info, ok := o["sppower_battery_health_info"].(obj); ok {
			p.MaxCapacity = str(info, "sppower_battery_health_maximum_capacity")
			p.Condition = str(info, "sppower_battery_health")
		}
		if info, ok := o["sppower_battery_model_info"].(obj); ok {
			p.Controller = str(info, "sppower_battery_device_name")
		}
		if name := str(o, "sppower_ac_charger_name"); name != "" {
			p.Charger = name
		}
	}
	return
}

func hardwareSections() ([]Section, error) {
	data, err := profile()
	if err != nil {
		return nil, err
	}
	var res []Section
	var src string
	section := func(title, dataType string) {
		res = append(res, Section{Title: title})
		src = "system_profiler: " + dataType
	}
	add := func(label, value string) {
		if value != "" {
			s := &res[len(res)-1]
			s.Items = append(s.Items, Item{Label: label, Value: value, Key: src})
		}
	}

	section("Комп’ютер", "SPHardwareDataType")
	for _, o := range data["SPHardwareDataType"] {
		add("Назва", str(o, "machine_name"))
		add("Ідентифікатор моделі", str(o, "machine_model"))
		add("Номер моделі", str(o, "model_number"))
		add("Версія прошивки", str(o, "boot_rom_version"))
	}

	section("Модулі пам’яті", "SPMemoryDataType")
	for _, o := range data["SPMemoryDataType"] {
		add("Тип", str(o, "dimm_type"))
		add("Виробник", str(o, "dimm_manufacturer"))
		for _, d := range children(o, "_items") { // separate modules on Intel
			add(str(d, "_name"), strings.TrimSpace(str(d, "dimm_size")+" "+str(d, "dimm_type")+" "+str(d, "dimm_speed")))
		}
	}

	section("GPU", "SPDisplaysDataType")
	for _, o := range data["SPDisplaysDataType"] {
		add("Модель", str(o, "sppci_model"))
		add("Ядер", str(o, "sppci_cores"))
		add("Metal", strings.TrimPrefix(str(o, "spdisplays_mtlgpufamilysupport"), "spdisplays_metal"))
	}
	section("Дисплеї", "SPDisplaysDataType")
	for _, o := range data["SPDisplaysDataType"] {
		for _, d := range children(o, "spdisplays_ndrvs") {
			name := str(d, "_name")
			add(name+tr(": матриця"), str(d, "_spdisplays_pixels"))
			add(name+tr(": режим"), str(d, "_spdisplays_resolution"))
		}
	}

	section("Аудіо", "SPAudioDataType")
	for _, o := range data["SPAudioDataType"] {
		for _, d := range children(o, "_items") {
			var parts []string
			if n := str(d, "coreaudio_device_input"); n != "" {
				parts = append(parts, tr("входів: ")+n)
			}
			if n := str(d, "coreaudio_device_output"); n != "" {
				parts = append(parts, tr("виходів: ")+n)
			}
			if r := str(d, "coreaudio_device_srate"); r != "" {
				parts = append(parts, r+tr(" Гц"))
			}
			add(str(d, "_name"), strings.Join(parts, ", "))
		}
	}
	section("Камера", "SPCameraDataType")
	for _, o := range data["SPCameraDataType"] {
		add("Модель", str(o, "_name"))
	}

	section("USB", "SPUSBHostDataType")
	var usb func(o obj)
	usb = func(o obj) {
		for _, d := range children(o, "_items") {
			vendor := str(d, "USBDeviceKeyVendorName")
			if vendor == "" {
				vendor = "підключено"
			}
			add(str(d, "_name"), vendor)
			usb(d)
		}
	}
	for _, o := range data["SPUSBHostDataType"] {
		usb(o)
	}
	section("Thunderbolt", "SPThunderboltDataType")
	ports := slices.Clone(data["SPThunderboltDataType"])
	slices.SortFunc(ports, func(a, b obj) int {
		pa, _ := a["receptacle_1_tag"].(obj)
		pb, _ := b["receptacle_1_tag"].(obj)
		return strings.Compare(str(pa, "receptacle_id_key"), str(pb, "receptacle_id_key"))
	})
	for _, o := range ports {
		port, ok := o["receptacle_1_tag"].(obj)
		if !ok {
			continue
		}
		status := "пристрій підключено"
		if str(port, "receptacle_status_key") == "receptacle_no_devices_connected" {
			status = "вільний"
		}
		add(tr("Порт ")+str(port, "receptacle_id_key"), tr(status)+", "+strings.Replace(str(port, "current_speed_key"), "Up to", tr("до"), 1))
		for _, d := range children(o, "_items") {
			add("Пристрій Thunderbolt", str(d, "_name"))
		}
	}

	return res, nil
}

func decimalGB(n float64) string { return fmt.Sprintf(tr("%.1f ГБ"), n/1e9) }
