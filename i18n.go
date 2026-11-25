package main

/*
#cgo LDFLAGS: -framework CoreFoundation
#include <CoreFoundation/CoreFoundation.h>
static int prefersUkrainian(void) {
    CFArrayRef list = CFLocaleCopyPreferredLanguages();
    int uk = 0;
    if (list && CFArrayGetCount(list) > 0)
        uk = CFStringHasPrefix(CFArrayGetValueAtIndex(list, 0), CFSTR("uk"));
    if (list) CFRelease(list);
    return uk;
}
*/
import "C"

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
)

// Source strings are Ukrainian and double as section and item identifiers.
// Titles, values and notes are translated on output (localize);
// format strings and text fragments are translated in place via tr.
var english atomic.Bool

func tr(s string) string {
	if english.Load() {
		if t, ok := catalog[s]; ok {
			return t
		}
	}
	return s
}

func langCode() string {
	if english.Load() {
		return "en"
	}
	return "uk"
}

func setLang(code string) error {
	switch code {
	case "en", "uk":
		english.Store(code == "en")
		return nil
	}
	return fmt.Errorf(tr("невідома мова %q: en або uk"), code)
}

// initLang uses the saved choice, otherwise the first system language.
func initLang() {
	if dir, err := configPath(); err == nil {
		if data, err := os.ReadFile(filepath.Join(dir, "lang")); err == nil && setLang(strings.TrimSpace(string(data))) == nil {
			return
		}
	}
	english.Store(C.prefersUkrainian() == 0)
}

func saveLang(code string) error {
	if err := setLang(code); err != nil {
		return err
	}
	dir, err := dataDir()
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "lang"), []byte(code+"\n"), 0o600)
}

// Names with these prefixes contain a variable part.
var prefixes = []string{"Том: ", "Ядра ", "Накопичувач ", "Інтерфейс ", "Вентилятор "}

func trName(s string) string {
	if !english.Load() {
		return s
	}
	if t, ok := catalog[s]; ok {
		return t
	}
	for _, p := range prefixes {
		if rest, ok := strings.CutPrefix(s, p); ok {
			return tr(p) + rest
		}
	}
	return s
}

func localize(sections []Section) []Section {
	if !english.Load() {
		return sections
	}
	res := make([]Section, len(sections))
	for i, s := range sections {
		res[i] = Section{Title: trName(s.Title), Items: make([]Item, len(s.Items))}
		for j, it := range s.Items {
			it.Label, it.Value, it.Note = trName(it.Label), tr(it.Value), tr(it.Note)
			res[i].Items[j] = it
		}
	}
	return res
}

func usage() string {
	if english.Load() {
		return usageEN
	}
	return usageUK
}
