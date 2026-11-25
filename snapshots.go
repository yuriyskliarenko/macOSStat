package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// snapshot holds all parameters at a point in time.
type snapshot struct {
	Name     string            `json:"name"`
	At       time.Time         `json:"at"`
	OS       string            `json:"os"`
	Params   map[string]string `json:"params"`
	Writable []string          `json:"writable"` // settings (sysctl -aW)
}

type snapshotInfo struct {
	Name string `json:"name"`
	At   string `json:"at"`
	OS   string `json:"os"`
}

type change struct {
	Key string `json:"key"`
	Old string `json:"old"`
	New string `json:"new"`
}

const nowName = "@now" // "@" is not allowed in snapshot names

var snapNameRe = regexp.MustCompile(`^[\p{L}\p{N}_][\p{L}\p{N}_. -]{0,63}$`)

func snapshotPath(name string) (string, error) {
	if !snapNameRe.MatchString(name) || name == nowName {
		return "", fmt.Errorf(tr("некоректна назва знімка %q: літери, цифри, пробіл, крапка, _ і -, до 64 символів"), name)
	}
	dir, err := dataDir()
	if err != nil {
		return "", err
	}
	dir = filepath.Join(dir, "snapshots")
	return filepath.Join(dir, name+".json"), os.MkdirAll(dir, 0o700)
}

func takeSnapshot(name string) (snapshot, error) {
	params, err := readAll()
	if err != nil {
		return snapshot{}, err
	}
	out, _, err := runSysctl("-aW")
	if err != nil {
		return snapshot{}, err
	}
	s := snapshot{Name: name, At: time.Now(), Params: map[string]string{}}
	for _, p := range params {
		s.Params[p.Key] = p.Value
	}
	for _, p := range parseSysctl(out) {
		s.Writable = append(s.Writable, p.Key)
	}
	s.OS = s.Params["kern.osproductversion"] + " (" + s.Params["kern.osversion"] + ")"
	return s, nil
}

func saveSnapshot(name string) error {
	path, err := snapshotPath(name)
	if err != nil {
		return err
	}
	s, err := takeSnapshot(name)
	if err != nil {
		return err
	}
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if os.IsExist(err) {
		return fmt.Errorf(tr("знімок %q уже є"), name)
	}
	if err != nil {
		return err
	}
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func loadSnapshot(name string) (snapshot, error) {
	if name == nowName || name == "" {
		return takeSnapshot(nowName)
	}
	path, err := snapshotPath(name)
	if err != nil {
		return snapshot{}, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return snapshot{}, fmt.Errorf(tr("немає знімка %q"), name)
	}
	if err != nil {
		return snapshot{}, err
	}
	var s snapshot
	if err := json.Unmarshal(data, &s); err != nil {
		return s, fmt.Errorf(tr("знімок %q пошкоджено: %w"), name, err)
	}
	s.Name = name
	return s, nil
}

func listSnapshots() ([]snapshotInfo, error) {
	dir, err := dataDir()
	if err != nil {
		return nil, err
	}
	paths, _ := filepath.Glob(filepath.Join(dir, "snapshots", "*.json"))
	list := []snapshotInfo{}
	var all []snapshot
	for _, p := range paths {
		if s, err := loadSnapshot(strings.TrimSuffix(filepath.Base(p), ".json")); err == nil {
			all = append(all, s)
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].At.After(all[j].At) })
	for _, s := range all {
		list = append(list, snapshotInfo{s.Name, s.At.Local().Format("02.01.2006 15:04"), s.OS})
	}
	return list, nil
}

// diffSnapshots reports changed settings; with all, also counters and state.
func diffSnapshots(a, b snapshot, all bool) []change {
	writable := map[string]bool{}
	for _, list := range [][]string{a.Writable, b.Writable} {
		for _, k := range list {
			writable[k] = true
		}
	}
	res := []change{}
	for k, old := range a.Params {
		v, ok := b.Params[k]
		if !ok {
			v = "—"
		}
		if v != old && (all || writable[k] || !ok) {
			res = append(res, change{k, old, v})
		}
	}
	for k, v := range b.Params {
		if _, ok := a.Params[k]; !ok {
			res = append(res, change{k, "—", v})
		}
	}
	if !all && a.OS != b.OS {
		res = append(res, change{"macOS", a.OS, b.OS})
	}
	sort.Slice(res, func(i, j int) bool { return res[i].Key < res[j].Key })
	return res
}

func diffByName(a, b string, all bool) ([]change, error) {
	sa, err := loadSnapshot(a)
	if err != nil {
		return nil, err
	}
	sb, err := loadSnapshot(b)
	if err != nil {
		return nil, err
	}
	return diffSnapshots(sa, sb, all), nil
}
