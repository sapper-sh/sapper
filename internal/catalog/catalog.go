// Package catalog holds the built-in app definitions a blueprint can reference.
package catalog

import "sort"

// Mount is a container path that holds persistent data.
type Mount struct {
	Name          string // volume name used in the blueprint, e.g. "data"
	ContainerPath string
}

// App is a catalogue entry.
type App struct {
	Name    string
	Image   string
	Port    int // container port exposed over HTTP
	Mounts  []Mount
	Env     map[string]string
	Summary string
}

var apps = map[string]App{
	"vaultwarden": {
		Name: "vaultwarden", Image: "vaultwarden/server:1.32.7", Port: 80,
		Mounts:  []Mount{{Name: "data", ContainerPath: "/data"}},
		Env:     map[string]string{"SIGNUPS_ALLOWED": "false"},
		Summary: "password manager (Bitwarden-compatible)",
	},
	"filebrowser": {
		Name: "filebrowser", Image: "filebrowser/filebrowser:v2.31.2", Port: 80,
		Mounts: []Mount{
			{Name: "files", ContainerPath: "/srv"},
			{Name: "database", ContainerPath: "/database"},
		},
		Summary: "web file manager",
	},
	"memos": {
		Name: "memos", Image: "neosmemo/memos:0.23.0", Port: 5230,
		Mounts:  []Mount{{Name: "data", ContainerPath: "/var/opt/memos"}},
		Summary: "notes",
	},
}

// Lookup returns the catalogue entry for name.
func Lookup(name string) (App, bool) {
	a, ok := apps[name]
	return a, ok
}

// Names returns all catalogue app names in sorted order.
func Names() []string {
	out := make([]string, 0, len(apps))
	for n := range apps {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// HasMount reports whether app declares a mount called name.
func (a App) HasMount(name string) bool {
	for _, m := range a.Mounts {
		if m.Name == name {
			return true
		}
	}
	return false
}
