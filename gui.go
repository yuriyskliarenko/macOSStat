package main

/*
#cgo CFLAGS: -fobjc-arc
#cgo LDFLAGS: -framework Cocoa -framework WebKit
#include <stdlib.h>
void runWindow(const char *html, const void *icon, int iconLen);
*/
import "C"

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"unsafe"
)

//go:embed ui.html
var uiHTML string

//go:embed icon.png
var iconPNG []byte // Dock icon: a bare binary has no bundle resources

// Cocoa requires the window to live on the main thread.
func init() { runtime.LockOSThread() }

func runGUI() error {
	html := C.CString(strings.Replace(uiHTML, "{{LANG}}", langCode(), 1))
	defer C.free(unsafe.Pointer(html))
	C.runWindow(html, unsafe.Pointer(&iconPNG[0]), C.int(len(iconPNG)))
	return nil
}

//export goEnglish
func goEnglish() C.int {
	if english.Load() {
		return 1
	}
	return 0
}

//export goCall
func goCall(request *C.char) *C.char {
	res, err := handle([]byte(C.GoString(request)))
	var data []byte
	if err == nil {
		data, err = json.Marshal(res)
	}
	if err != nil {
		data, _ = json.Marshal(map[string]string{"error": err.Error()})
	}
	return C.CString(string(data)) // freed by gui.m
}

func handle(raw []byte) (any, error) {
	var q struct {
		Op, Name, A, B string
		All            bool
	}
	if err := json.Unmarshal(raw, &q); err != nil {
		return nil, err
	}
	switch q.Op {
	case "view":
		sections, err := localizedView(q.Name)
		return map[string]any{"sections": sections, "categories": localizedCategories()}, err
	case "categories":
		return map[string]any{"categories": localizedCategories()}, nil
	case "lang":
		return map[string]any{}, saveLang(q.Name)
	case "menu":
		return menuStatus(), nil
	case "snapshots":
		list, err := listSnapshots()
		return map[string]any{"snapshots": list, "now": nowName}, err
	case "save":
		return map[string]any{}, saveSnapshot(q.Name)
	case "diff":
		changes, err := diffByName(q.A, q.B, q.All)
		return map[string]any{"changes": changes}, err
	}
	return nil, fmt.Errorf(tr("невідома операція %q"), q.Op)
}
