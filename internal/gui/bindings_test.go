// SPDX-License-Identifier: MIT

package gui

import (
	"os"
	"reflect"
	"regexp"
	"testing"

	"github.com/otfabric/modbus-trace-explorer/internal/service"
)

// Every Service method is bound for the window (Subscribe arrives as
// events), and every binding the frontend's Wails adapter calls exists, so
// a method added on one side only fails here and not at run time.
func TestBindings(t *testing.T) {
	app := reflect.TypeOf(&App{})
	svc := reflect.TypeOf((*service.Service)(nil)).Elem()
	for i := range svc.NumMethod() {
		name := svc.Method(i).Name
		if name == "Subscribe" {
			continue
		}
		if _, ok := app.MethodByName(name); !ok {
			t.Errorf("Service.%s is not bound in gui.App", name)
		}
	}
	src, err := os.ReadFile("../../frontend/src/adapters/wails/wails.ts")
	if err != nil {
		t.Fatal(err)
	}
	calls := regexp.MustCompile(`call<[^>]*>\('(\w+)'`).FindAllStringSubmatch(string(src), -1)
	if len(calls) < 30 {
		t.Fatalf("found %d calls in the adapter", len(calls))
	}
	for _, c := range calls {
		if _, ok := app.MethodByName(c[1]); !ok {
			t.Errorf("the frontend calls %s, which gui.App does not have", c[1])
		}
	}
}
