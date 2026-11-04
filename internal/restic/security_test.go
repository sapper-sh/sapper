package restic

import (
	"context"
	"strings"
	"testing"
)

func TestCmdDropsPasswordSourcesFromChildEnv(t *testing.T) {
	t.Setenv("SAPPER_RESTIC_PASSWORD", "from-env")
	t.Setenv("RESTIC_PASSWORD_COMMAND", "sh -c evil")
	c := &Client{Bin: "restic", Repository: "/r", Password: "pw", DropEnv: []string{"SAPPER_RESTIC_PASSWORD"}}
	cmd := c.cmd(context.Background(), "snapshots")
	var pw int
	for _, kv := range cmd.Env {
		switch {
		case strings.HasPrefix(kv, "SAPPER_RESTIC_PASSWORD="), strings.HasPrefix(kv, "RESTIC_PASSWORD_COMMAND="):
			t.Errorf("child env contains %s", kv)
		case strings.HasPrefix(kv, "RESTIC_PASSWORD="):
			pw++
		}
	}
	if pw != 1 {
		t.Errorf("RESTIC_PASSWORD set %d times", pw)
	}
	for _, a := range cmd.Args {
		if strings.Contains(a, "pw") || strings.Contains(a, "from-env") {
			t.Errorf("password in argv: %v", cmd.Args)
		}
	}
}
