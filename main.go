package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
)

const usageUK = `macOSStat — корисні параметри macOS (лише читання)

  macOSStat                                  графічне вікно
  macOSStat --cli [КАТЕГОРІЯ] [--json]       огляд або окрема категорія
  macOSStat --cli lang [en|uk]               показати або зберегти мову
  macOSStat --cli list [--json] [ШАБЛОН]     усі параметри, з пошуком у ключі та значенні
  macOSStat --cli get [--json] КЛЮЧ...       окремі ключі або гілки (hw, vm.swapusage)
  macOSStat --cli watch [-n СЕКУНДИ] [КАТЕГОРІЯ]
                                             автооновлення (Ctrl+C — вихід)
  macOSStat --cli snapshot list                       збережені знімки параметрів
  macOSStat --cli snapshot save НАЗВА                 зберегти знімок
  macOSStat --cli snapshot diff [--all] А [Б]         порівняти (без Б — із поточним станом);
                                             --all додає лічильники й стан
  --lang en|uk після --cli                   мова лише для цього запуску

Категорії: summary, diagnosis, system, cpu, passport, memory, disks, rates, battery,
           sensors, wakes, devices, network, security, limits, all
`

const usageEN = `macOSStat — useful macOS parameters (read-only)

  macOSStat                                  graphical window
  macOSStat --cli [CATEGORY] [--json]        overview or a single category
  macOSStat --cli lang [en|uk]               show or save the language
  macOSStat --cli list [--json] [PATTERN]    all parameters, search in key and value
  macOSStat --cli get [--json] KEY...        single keys or subtrees (hw, vm.swapusage)
  macOSStat --cli watch [-n SECONDS] [CATEGORY]
                                             auto-refresh (Ctrl+C to quit)
  macOSStat --cli snapshot list                       saved parameter snapshots
  macOSStat --cli snapshot save NAME                  save a snapshot
  macOSStat --cli snapshot diff [--all] A [B]         compare (without B: to current state);
                                             --all adds counters and state
  --lang en|uk after --cli                   language for this run only

Categories: summary, diagnosis, system, cpu, passport, memory, disks, rates, battery,
            sensors, wakes, devices, network, security, limits, all
`

func main() {
	initLang()
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "macOSStat:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return runGUI()
	}
	switch args[0] {
	case "--cli":
		return runCLI(args[1:])
	case "help", "-h", "--help":
		fmt.Print(usage())
		return nil
	}
	return fmt.Errorf(tr("невідомий аргумент %q (для командного рядка додайте --cli)\n\n%s"), args[0], usage())
}

func runCLI(args []string) error {
	// --lang works with any command, so strip it up front.
	for i, a := range args {
		if a != "--lang" {
			continue
		}
		if i+1 >= len(args) {
			return setLang("")
		}
		if err := setLang(args[i+1]); err != nil {
			return err
		}
		args = append(args[:i:i], args[i+2:]...)
		break
	}
	cmd := "summary"
	if len(args) > 0 && (!strings.HasPrefix(args[0], "-") || args[0] == "-h" || args[0] == "--help") {
		cmd, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage()) }
	var asJSON, allKeys bool
	interval := 5.0
	switch cmd {
	case "lang":
	case "list", "get":
		fs.BoolVar(&asJSON, "json", false, "")
	case "snapshot":
		fs.BoolVar(&asJSON, "json", false, "")
		fs.BoolVar(&allKeys, "all", false, "")
	case "watch":
		fs.Float64Var(&interval, "n", interval, "")
	case "help", "-h", "--help":
		fmt.Print(usage())
		return nil
	default:
		if cmd != "all" && findCategory(cmd) == nil {
			return fmt.Errorf(tr("невідома команда %q\n\n%s"), cmd, usage())
		}
		fs.BoolVar(&asJSON, "json", false, "")
	}

	// flag stops at the first argument, so parse the flags after it ourselves.
	var rest []string
	for {
		fs.Parse(args)
		if fs.NArg() == 0 {
			break
		}
		rest = append(rest, fs.Arg(0))
		args = fs.Args()[1:]
	}
	switch {
	case cmd == "get" && len(rest) == 0:
		return fmt.Errorf(tr("вкажіть хоча б один ключ\n\n%s"), usage())
	case (cmd == "list" || cmd == "watch" || cmd == "lang") && len(rest) > 1,
		cmd != "get" && cmd != "list" && cmd != "watch" && cmd != "snapshot" && cmd != "lang" && len(rest) > 0:
		return fmt.Errorf(tr("зайві аргументи: %s"), strings.Join(rest, " "))
	}

	switch cmd {
	case "lang":
		if len(rest) == 1 {
			return saveLang(rest[0])
		}
		fmt.Println(langCode())
		return nil
	case "snapshot":
		return runSnapshot(rest, allKeys, asJSON)
	case "get":
		params, err := readKeys(rest)
		if len(params) > 0 || err == nil {
			if perr := printParams(params, asJSON); perr != nil {
				return perr
			}
		}
		return err
	case "list":
		params, err := readAll()
		if err != nil {
			return err
		}
		if len(rest) == 1 {
			params = filterParams(params, rest[0])
		}
		return printParams(params, asJSON)
	case "watch":
		if interval < 0.1 {
			return errors.New(tr("інтервал має бути не менше 0.1 с"))
		}
		name := "summary"
		if len(rest) == 1 {
			name = rest[0]
		}
		for {
			sections, err := localizedView(name)
			if err != nil {
				return err
			}
			fmt.Print("\033[H\033[2J" + renderSummary(sections))
			time.Sleep(time.Duration(interval * float64(time.Second)))
		}
	}
	sections, err := localizedView(cmd)
	if err != nil {
		return err
	}
	if asJSON {
		return printJSON(sections)
	}
	fmt.Print(renderSummary(sections))
	return nil
}

func filterParams(params []Param, pattern string) []Param {
	pattern = strings.ToLower(pattern)
	found := []Param{}
	for _, p := range params {
		if strings.Contains(strings.ToLower(p.Key+"\n"+p.Value), pattern) {
			found = append(found, p)
		}
	}
	return found
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

func printParams(params []Param, asJSON bool) error {
	if asJSON {
		return printJSON(params)
	}
	for _, p := range params {
		fmt.Printf("%s: %s\n", p.Key, p.Value)
	}
	return nil
}

func renderSummary(sections []Section) string {
	var b strings.Builder
	for i, s := range sections {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "%s\n", s.Title)
		for _, it := range s.Items {
			mark := map[string]string{"ok": "✓ ", "warn": "! ", "bad": "✗ "}[it.Level]
			fmt.Fprintf(&b, "  %-40s %s", mark+it.Label, it.Value) // width is counted in runes
			if it.Percent != nil {
				n := int(*it.Percent/5 + 0.5)
				fmt.Fprintf(&b, "  %s%s", strings.Repeat("█", n), strings.Repeat("░", 20-n))
			}
			b.WriteByte('\n')
			if it.Note != "" {
				fmt.Fprintf(&b, "      %s\n", it.Note)
			}
		}
	}
	return b.String()
}

func runSnapshot(args []string, all, asJSON bool) error {
	switch {
	case len(args) == 1 && args[0] == "list":
		list, err := listSnapshots()
		if err != nil || asJSON {
			if err == nil {
				err = printJSON(list)
			}
			return err
		}
		for _, s := range list {
			fmt.Printf("%-24s %s  macOS %s\n", s.Name, s.At, s.OS)
		}
		return nil
	case len(args) == 2 && args[0] == "save":
		return saveSnapshot(args[1])
	case (len(args) == 2 || len(args) == 3) && args[0] == "diff":
		changes, err := diffByName(args[1], strings.Join(args[2:], ""), all)
		if err != nil {
			return err
		}
		if asJSON {
			return printJSON(changes)
		}
		for _, c := range changes {
			fmt.Printf("%s\n  - %s\n  + %s\n", c.Key, c.Old, c.New)
		}
		fmt.Printf(tr("Змін: %d\n"), len(changes))
		return nil
	}
	return fmt.Errorf(tr("некоректна команда snapshot\n\n%s"), usage())
}
