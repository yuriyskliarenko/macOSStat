package main

import (
	"fmt"
	"sort"
	"strings"
)

var cpuFeatures = []struct{ title, key, label, why string }{
	{"ML і матриці", "FEAT_DotProd", "DotProd", "Скалярні добутки int8: квантовані моделі."},
	{"ML і матриці", "FEAT_I8MM", "I8MM", "Множення матриць int8: швидші квантовані LLM на CPU."},
	{"ML і матриці", "FEAT_BF16", "BF16", "Формат bfloat16 на CPU."},
	{"ML і матриці", "FEAT_FP16", "FP16", "Арифметика половинної точності."},
	{"ML і матриці", "FEAT_SME", "SME", "Матричне розширення Arm (є з M4)."},
	{"ML і матриці", "FEAT_SME2", "SME2", "Друга версія SME: багатовекторні операції."},
	{"Криптографія", "FEAT_AES", "AES", "Апаратне шифрування: TLS, VPN."},
	{"Криптографія", "FEAT_PMULL", "PMULL", "Прискорює AES-GCM."},
	{"Криптографія", "FEAT_SHA256", "SHA-256", "Апаратне гешування."},
	{"Криптографія", "FEAT_SHA512", "SHA-512", "Апаратне гешування."},
	{"Криптографія", "FEAT_SHA3", "SHA-3", "Апаратне гешування."},
	{"Криптографія", "FEAT_CRC32", "CRC32", "Контрольні суми: файлові системи, бази даних."},
	{"Паралельність", "FEAT_LSE", "LSE", "Атомарні операції без циклів повтору."},
	{"Паралельність", "FEAT_LSE2", "LSE2", "Атомарний доступ до невирівняних даних."},
	{"Паралельність", "FEAT_LRCPC", "LRCPC", "Дешевша синхронізація acquire/release."},
	{"Паралельність", "FEAT_WFxT", "WFxT", "Очікування з тайм-аутом без зайвого споживання."},
	{"Мови й емуляція", "FEAT_JSCVT", "JSCVT", "Перетворення чисел для JavaScript."},
	{"Мови й емуляція", "FEAT_FlagM", "FlagM", "Робота з прапорцями, зокрема для емуляції."},
	{"Мови й емуляція", "FEAT_CSSC", "CSSC", "Короткі інструкції: min, max, popcount."},
	{"Захист", "FEAT_PAuth", "PAuth", "Підпис вказівників проти підміни адрес."},
	{"Захист", "FEAT_PAuth2", "PAuth2", "Посилений підпис вказівників."},
	{"Захист", "FEAT_BTI", "BTI", "Обмежує цілі непрямих переходів."},
	{"Захист", "FEAT_MTE", "MTE", "Теги пам’яті: ловлять вихід за межі."},
	{"Захист", "FEAT_DIT", "DIT", "Час виконання не залежить від даних."},
	{"Захист", "FEAT_SSBS", "SSBS", "Захист від Spectre v4."},
}

func passportSections() ([]Section, error) {
	params, err := readAll()
	if err != nil {
		return nil, err
	}
	const prefix = "hw.optional.arm."
	vals, yes, total := map[string]string{}, 0, 0
	var others []string
	known := map[string]bool{}
	for _, f := range cpuFeatures {
		known[f.key] = true
	}
	var res []Section
	for _, p := range params {
		vals[p.Key] = p.Value
		if name, ok := strings.CutPrefix(p.Key, prefix); ok && strings.HasPrefix(name, "FEAT_") {
			total++
			if p.Value == "1" {
				yes++
				if !known[name] {
					others = append(others, strings.TrimPrefix(name, "FEAT_"))
				}
			}
		}
	}
	if total == 0 { // Intel
		s := Section{Title: "Інструкції Intel"}
		for _, k := range []string{"machdep.cpu.features", "machdep.cpu.leaf7_features", "machdep.cpu.extfeatures"} {
			if vals[k] != "" {
				s.Items = append(s.Items, item(strings.TrimPrefix(k, "machdep.cpu."), vals[k], k, nil))
			}
		}
		return []Section{s}, nil
	}
	for _, f := range cpuFeatures {
		v, ok := vals[prefix+f.key]
		if !ok {
			continue
		}
		if len(res) == 0 || res[len(res)-1].Title != f.title {
			res = append(res, Section{Title: f.title})
		}
		it := item(f.label, "є", prefix+f.key, nil)
		if v != "1" {
			it.Value = "немає"
		}
		it.Note = f.why
		res[len(res)-1].Items = append(res[len(res)-1].Items, it)
	}
	sort.Strings(others)
	return append(res, Section{Title: "Разом", Items: []Item{
		item("Підтримується", fmt.Sprintf(tr("%d із %d відомих ядру"), yes, total), prefix+"FEAT_*", nil),
		item("Також є", strings.Join(others, ", "), prefix+"FEAT_*", nil)}}), nil
}
