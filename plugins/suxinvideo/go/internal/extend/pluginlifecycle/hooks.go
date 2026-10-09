// Package pluginlifecycle stays in the host when code plugins are uninstalled.
package pluginlifecycle

import "sync"

var stops = struct {
	sync.Mutex
	items map[string]func()
}{items: map[string]func(){}}

func RegisterStop(name string, stop func()) {
	stops.Lock()
	defer stops.Unlock()
	stops.items[name] = stop
}
func Stop(name string) {
	stops.Lock()
	stop := stops.items[name]
	delete(stops.items, name)
	stops.Unlock()
	if stop != nil {
		stop()
	}
}
