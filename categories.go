package main

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Category is a sidebar entry and a CLI subcommand.
type Category struct {
	Name     string   `json:"name"`
	Title    string   `json:"title"`
	sections []string // section titles in display order
	sources  []string // which sources to query
}

var categories = []Category{
	{"summary", "Огляд", nil, []string{"sysctl", "hardware", "sensors", "battery", "disks", "network"}},
	{"diagnosis", "Діагноз", nil, []string{"diagnosis"}},
	{"system", "Система", []string{"Комп’ютер", "macOS", "Сон і завантаження"}, []string{"sysctl", "hardware"}},
	{"cpu", "Процесор і GPU", []string{"Процесор", "Ядра Performance", "Ядра Efficiency", "Кеш", "GPU"}, []string{"sysctl", "hardware"}},
	{"passport", "Паспорт CPU", nil, []string{"passport"}},
	{"memory", "Пам’ять", []string{"Використання пам’яті", "Модулі пам’яті"}, []string{"sysctl", "hardware"}},
	{"disks", "Диски", nil, []string{"disks"}},
	{"rates", "Швидкість змін", nil, []string{"rates"}},
	{"battery", "Акумулятор", nil, []string{"battery"}},
	{"sensors", "Температури", nil, []string{"sensors"}},
	{"wakes", "Пробудження", nil, []string{"wakes"}},
	{"devices", "Дисплеї та пристрої", []string{"Дисплеї", "Thunderbolt", "USB", "Аудіо", "Камера"}, []string{"hardware"}},
	{"network", "Мережа", nil, []string{"network", "sysctl"}},
	{"security", "Безпека", []string{"Безпека"}, []string{"sysctl"}},
	{"limits", "Ліміти", []string{"Файли", "Процеси й потоки", "Інші ліміти"}, []string{"sysctl"}},
}

func findCategory(name string) *Category {
	for i := range categories {
		if categories[i].Name == name {
			return &categories[i]
		}
	}
	return nil
}

func fetch(source string) ([]Section, error) {
	switch source {
	case "sysctl":
		params, err := readAll()
		if err != nil {
			return nil, err
		}
		return buildSummary(params, time.Now()), nil
	case "hardware":
		return hardwareSections()
	case "disks":
		return diskSections()
	case "diagnosis":
		return diagnosisSections()
	case "passport":
		return passportSections()
	case "rates":
		return rateSections()
	case "wakes":
		return wakeSections()
	case "network":
		return networkSections()
	case "battery":
		return batterySections(), nil
	}
	return sensorSections(), nil
}

func localizedView(name string) ([]Section, error) {
	s, err := view(name)
	return localize(s), err
}

// view returns the sections of a category; "all" means everything but the overview.
func view(name string) ([]Section, error) {
	if name == "all" {
		var res []Section
		for _, c := range categories[1:] {
			s, err := view(c.Name)
			if err != nil {
				return nil, err
			}
			res = append(res, s...)
		}
		return res, nil
	}
	c := findCategory(name)
	if c == nil {
		return nil, fmt.Errorf(tr("невідома категорія %q"), name)
	}
	var all []Section
	for _, src := range c.sources {
		s, err := fetch(src)
		if err != nil {
			return nil, err
		}
		for _, sec := range s {
			if len(sec.Items) > 0 {
				all = append(all, sec)
			}
		}
	}
	if name == "summary" {
		return overview(all), nil
	}
	if name == "network" {
		// Interfaces vary, and only the network sections of sysctl are needed.
		return slices.DeleteFunc(all, func(s Section) bool {
			return !slices.Contains([]string{"Підключення", "Wi-Fi", "З’єднання", "TCP/IP", "Буфери"}, s.Title) &&
				!strings.HasPrefix(s.Title, "Інтерфейс ")
		}), nil
	}
	if c.sections == nil {
		return all, nil
	}
	var res []Section
	for _, title := range c.sections {
		for _, s := range all {
			if s.Title == title {
				res = append(res, s)
			}
		}
	}
	return res, nil
}

// overview picks the essentials from the other categories.
func overview(all []Section) []Section {
	pick := func(title string, want ...string) Section { // want: "Section/Label"
		res := Section{Title: title}
		for _, w := range want {
			sec, label, _ := strings.Cut(w, "/")
			for _, s := range all {
				for _, it := range s.Items {
					if s.Title == sec && it.Label == label {
						res.Items = append(res.Items, it)
					}
				}
			}
		}
		return res
	}
	disks := Section{Title: "Диски"}
	for _, s := range all {
		if name, ok := strings.CutPrefix(s.Title, "Том: "); ok && len(s.Items) > 0 {
			it := s.Items[0]
			it.Label = name
			disks.Items = append(disks.Items, it)
		}
	}
	res := []Section{
		pick("Система", "Комп’ютер/Назва", "Процесор/Процесор", "macOS/Версія macOS", "Сон і завантаження/Час роботи",
			"Процесор/Навантаження 1/5/15 хв"),
		pick("Пам’ять", "Використання пам’яті/Усього", "Використання пам’яті/Тиск на пам’ять",
			"Використання пам’яті/Доступно за оцінкою ядра", "Використання пам’яті/Swap"),
		disks,
		pick("Мережа", "Підключення/Стан", "Підключення/Інтерфейс", "Підключення/IP-адреса", "Підключення/Швидкість Wi-Fi",
			"Підключення/Швидкість лінка", "Wi-Fi/Сигнал", "Підключення/Завантаження ↓", "Підключення/Вивантаження ↑"),
		pick("Акумулятор", "Стан акумулятора/Цикли заряджання", "Стан акумулятора/Макс. ємність (за даними macOS)",
			"Заряд і живлення/Заряд", "Заряд і живлення/Стан"),
		pick("Температури", "Температури/Кристал SoC", "Температури/SSD (NAND)", "Температури/Акумулятор",
			"Вентилятори й потужність/Вентилятор 1", "Вентилятори й потужність/Вентилятор 2",
			"Вентилятори й потужність/Споживання системи"),
	}
	return slices.DeleteFunc(res, func(s Section) bool { return len(s.Items) == 0 })
}

var (
	containerRe = regexp.MustCompile(`^disk\d+`)
	ioStatRe    = regexp.MustCompile(`"((?:Bytes|Operations|Errors) \((?:Read|Write)\))"=(\d+)`)
)

func diskSections() ([]Section, error) {
	data, err := profile()
	if err != nil {
		return nil, err
	}
	var res []Section
	for _, c := range data["SPNVMeDataType"] {
		for _, d := range children(c, "_items") {
			s := Section{Title: "Накопичувач " + str(d, "bsd_name")}
			add := func(label, value string) {
				if value != "" {
					s.Items = append(s.Items, Item{Label: label, Value: value, Key: "system_profiler: SPNVMeDataType"})
				}
			}
			add("Модель", str(d, "device_model"))
			if n, ok := d["size_in_bytes"].(float64); ok {
				add("Розмір", decimalGB(n))
			}
			add("S.M.A.R.T.", translate(str(d, "smart_status")))
			add("TRIM", translate(str(d, "spnvme_trim_support")))
			add("Знімний", translate(str(d, "detachable_drive")))
			add("Прошивка", strings.TrimSpace(str(d, "device_revision")))
			res = append(res, s)
		}
	}

	// Volumes of one APFS container share space, so show them together.
	vols := map[string]int{}
	for _, v := range data["SPStorageDataType"] {
		size, _ := v["size_in_bytes"].(float64)
		free, _ := v["free_space_in_bytes"].(float64)
		if size <= 0 {
			continue
		}
		name, id := str(v, "_name"), str(v, "bsd_name")
		if str(v, "file_system") == "APFS" {
			id = containerRe.FindString(id)
		}
		if i, ok := vols[id]; ok && id != "" {
			res[i].Title += ", " + name
			res[i].Items = append(res[i].Items, Item{Label: "Точка монтування", Value: str(v, "mount_point"), Key: "system_profiler: SPStorageDataType"})
			continue
		}
		vols[id] = len(res)
		s := Section{Title: "Том: " + name}
		pct := min(100, max(0, (size-free)/size*100))
		add := func(label, value string, p *float64) {
			if value != "" {
				s.Items = append(s.Items, item(label, value, "system_profiler: SPStorageDataType", p))
			}
		}
		add("Зайнято", fmt.Sprintf("%s / %s (%.0f%%)", decimalGB(size-free), decimalGB(size), pct), &pct)
		add("Вільно", decimalGB(free), nil)
		add("Файлова система", str(v, "file_system"), nil)
		if p, ok := v["physical_drive"].(obj); ok {
			add("Диск", str(p, "device_name"), nil)
			add("Тип носія", strings.ToUpper(str(p, "medium_type")), nil)
			add("Інтерфейс", str(p, "protocol"), nil)
			add("Внутрішній", translate(str(p, "is_internal_disk")), nil)
		}
		add("Точка монтування", str(v, "mount_point"), nil)
		res = append(res, s)
	}

	total, err := diskCounters()
	if err != nil {
		return nil, err
	}
	io := Section{Title: "Активність усіх дисків"}
	for _, r := range []struct{ label, key string }{
		{"Прочитано", "Bytes (Read)"}, {"Записано", "Bytes (Write)"},
		{"Операцій читання", "Operations (Read)"}, {"Операцій запису", "Operations (Write)"},
		{"Помилок читання", "Errors (Read)"}, {"Помилок запису", "Errors (Write)"},
	} {
		n, ok := total[r.key]
		if !ok {
			continue
		}
		text := fmt.Sprintf("%.0f", n)
		if strings.HasPrefix(r.key, "Bytes") {
			text = decimalGB(n)
		}
		io.Items = append(io.Items, Item{Label: r.label, Value: text, Key: "ioreg: IOBlockStorageDriver " + r.key})
	}
	return append(res, io), nil
}

func translate(s string) string {
	switch strings.ToLower(s) {
	case "yes", "true":
		return "так"
	case "no", "false":
		return "ні"
	case "verified":
		return "справний"
	case "good":
		return "добрий"
	}
	return s
}

func localizedCategories() []Category {
	res := slices.Clone(categories)
	for i := range res {
		res[i].Title = tr(res[i].Title)
	}
	return res
}
