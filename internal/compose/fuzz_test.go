package compose

import (
	"runtime"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/sapper-unbuilt/sapper/internal/blueprint"
)

// FuzzRender checks that any blueprint passing validation renders files whose structure
// cannot be altered by blueprint values: one mount per compose volume entry, one line per
// backup path, a single RESTIC_REPOSITORY line and a single cron line.
func FuzzRender(f *testing.F) {
	f.Add([]byte(blueprint.Example))
	f.Add([]byte("version: 1\nfortress: f\ndata_root: /srv/f\napps:\n  memos:\n    port: 1\n    volumes:\n      data: \"a\\nb\"\nbackup:\n  repository: \"/r\\nX=1\"\n  password_env: P\ndrill:\n  schedule: \"0 3 * * *\"\n"))
	f.Add([]byte("version: 1\nfortress: f\ndata_root: /srv/f\napps:\n  memos:\n    port: 1\n    volumes:\n      data: \"/x:/etc\"\nbackup:\n  repository: /r\n  password_env: P\n"))
	f.Add([]byte("a: &a [1,1]\nb: &b [*a,*a]\nc: [*b,*b]\n"))
	f.Fuzz(func(t *testing.T, raw []byte) {
		bp, err := blueprint.Parse(raw)
		if err != nil {
			return
		}
		bp.Dir = "/srv/fortress"
		if bp.Validate() != nil {
			return
		}
		a, err := Render(bp)
		if err != nil {
			t.Fatalf("validated blueprint failed to render: %v", err)
		}
		var cf File
		if err := yaml.Unmarshal(a["docker-compose.yml"], &cf); err != nil {
			t.Fatalf("compose does not parse: %v", err)
		}
		mounts := 0
		for _, svc := range cf.Services {
			for _, v := range svc.Volumes {
				mounts++
				if strings.Count(v, ":") != 1 || strings.ContainsAny(v, "\n\r,") {
					t.Fatalf("volume %q is not exactly src:dst", v)
				}
			}
		}
		paths := strings.Split(strings.TrimSuffix(string(a["backup-paths.txt"]), "\n"), "\n")
		if len(paths) != mounts || len(bp.VolumePaths()) != mounts {
			t.Fatalf("%d backup paths for %d mounts", len(paths), mounts)
		}
		env := string(a["restic.env"])
		if strings.Count(env, "\n") != 3 || strings.Count(env, "=") != strings.Count(bp.RepoLocation(), "=")+1 {
			t.Fatalf("restic.env has injected lines:\n%s", env)
		}
		if runtime.GOOS != "windows" && strings.ContainsAny(bp.RepoLocation(), " \t$`\"'\\;&|<>(){}*?[]!") {
			t.Fatalf("restic.env value is unsafe to source: %q", bp.RepoLocation())
		}
		if c, ok := a["drill.cron"]; ok && (strings.Count(string(c), "\n") != 2 || strings.Contains(string(c), "%")) {
			t.Fatalf("drill.cron has injected lines:\n%s", c)
		}
	})
}
