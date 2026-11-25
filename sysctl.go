package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

// Param is a single sysctl parameter.
type Param struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// These keys are missing from `sysctl -a` but can be read by name.
var hiddenKeys = []string{"hw.model", "hw.cpufrequency", "kern.memorystatus_vm_pressure_level"}

var lineRe = regexp.MustCompile(`^([A-Za-z0-9_]+(?:\.[^\s:]+)+):(?: (.*))?$`)

// runSysctl returns stdout and stderr. A non-zero exit code is not an error:
// sysctl does that for a single unknown key and still prints the rest.
func runSysctl(args ...string) (string, string, error) {
	cmd := exec.Command("/usr/sbin/sysctl", args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		err = nil
	}
	return out.String(), errOut.String(), err
}

// parseSysctl parses `key: value` lines; other lines continue the value.
func parseSysctl(out string) []Param {
	var params []Param
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if m := lineRe.FindStringSubmatch(line); m != nil {
			params = append(params, Param{m[1], m[2]})
		} else if n := len(params); n > 0 {
			params[n-1].Value += "\n" + line
		}
	}
	return params
}

func readAll() ([]Param, error) {
	out, _, err := runSysctl("-a")
	if err != nil {
		return nil, err
	}
	params := parseSysctl(out)
	if len(params) == 0 {
		return nil, errors.New(tr("sysctl -a не повернув даних"))
	}
	seen := make(map[string]bool, len(params))
	for _, p := range params {
		seen[p.Key] = true
	}
	out, _, err = runSysctl(hiddenKeys...)
	if err != nil {
		return nil, err
	}
	for _, p := range parseSysctl(out) {
		if !seen[p.Key] {
			params = append(params, p)
		}
	}
	return params, nil
}

func readKeys(keys []string) ([]Param, error) {
	for _, k := range keys {
		// Read-only tool: no flags and no key=value writes.
		if strings.HasPrefix(k, "-") || strings.Contains(k, "=") {
			return nil, fmt.Errorf(tr("некоректний ключ: %q"), k)
		}
	}
	out, errOut, err := runSysctl(keys...)
	if err != nil {
		return nil, err
	}
	params := parseSysctl(out)
	if errOut != "" {
		return params, errors.New(strings.TrimSpace(errOut))
	}
	return params, nil
}
