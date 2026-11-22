//go:build desktop && darwin

package ui

/*
#include <stdlib.h>
void GSShowWindow(const char *name);
void GSUpdateWindows(const char *json);
int GSConfirm(const char *title, const char *message);
void GSShowMessage(const char *title, const char *message, int failure);
*/
import "C"

import (
	"encoding/json"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/TypeTerrors/go_sync/internal/configfile"
	"github.com/TypeTerrors/go_sync/internal/desktop/status"
	"github.com/ncruces/zenity"
)

var windowApp atomic.Pointer[App]

//export GSDispatchWindowAction
func GSDispatchWindowAction(value *C.char) {
	if a := windowApp.Load(); a != nil {
		select {
		case a.actions <- C.GoString(value):
		case <-a.session.Context.Done():
		default: // Do not block AppKit while a service action is in progress.
		}
	}
}

func (a *App) nativeQuestion(title, message string) (bool, error) {
	t, m := C.CString(title), C.CString(message)
	defer C.free(unsafe.Pointer(t))
	defer C.free(unsafe.Pointer(m))
	if C.GSConfirm(t, m) == 0 {
		return true, zenity.ErrCanceled
	}
	return true, nil
}

func (a *App) showMessage(title, message string, failure bool) error {
	t, m := C.CString(title), C.CString(message)
	defer C.free(unsafe.Pointer(t))
	defer C.free(unsafe.Pointer(m))
	f := C.int(0)
	if failure {
		f = 1
	}
	C.GSShowMessage(t, m, f)
	return nil
}

func (a *App) handlePresentationAction(action string) bool {
	switch action {
	case "settings", "details":
		name := C.CString(action)
		C.GSShowWindow(name)
		C.free(unsafe.Pointer(name))
		return true
	}
	return a.handleWindowAction(action)
}

func (a *App) updateWindows(data map[string]any, path string, cfg configfile.Description, m status.Model, service status.Service, now time.Time) {
	a.fillWindowData(data, path, cfg, m, service, now)
	if bytes, err := json.Marshal(data); err == nil {
		str := C.CString(string(bytes))
		C.GSUpdateWindows(str)
		C.free(unsafe.Pointer(str))
	}
}
