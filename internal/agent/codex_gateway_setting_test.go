package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/provider"
)

func TestCodexGatewayOverrideDoesNotConnect(t *testing.T) {
	for _, held := range []bool{false, true} {
		for _, model := range []string{"gpt-a", ""} {
			name := map[bool]string{false: "ChatGPT", true: "held ChatGPT"}[held] + "/" + model
			t.Run(name, func(t *testing.T) {
				config := ""
				if model != "" {
					config = "model = \"" + model + "\"\n"
				}
				home, read := codexHome(t, `{"tokens":{"access_token":"x","id_token":"x.e30.x"}}`, config)
				os.WriteFile(filepath.Join(home, ".codex", "models_cache.json"), []byte(`{"models":[{"slug":"gpt-a","display_name":"A","priority":1}]}`), 0o600)
				if err := provider.Delete("fake"); err != nil {
					t.Fatal(err)
				}
				codexUsedUp = func() bool { return held }
				cx := codex(home)
				before := read()
				if err := provider.SetCodexSubagentModel("codex/gpt-a"); err != nil {
					t.Fatal(err)
				}
				if cx.Wired() {
					t.Error("saved gateway override marked an unrouted Codex connected")
				}
				if err := cx.Pick("subagent_model", "codex/gpt-a"); err != nil {
					t.Fatal(err)
				}
				if read() != before || cx.Wired() {
					t.Errorf("gateway override connected Codex without wiring its config: wired=%v\n%s", cx.Wired(), read())
				}
				if d := cx.Drift(); d != nil {
					t.Errorf("gateway override is not a drifted Codex config: %+v", d)
				}
				c, err := cx.ConnectHow()
				if err != nil {
					t.Fatal(err)
				}
				want := "joined"
				if held {
					want = "same"
					if model == "" {
						want = "default"
					}
				}
				if c.How != want || !cx.Wired() || !strings.Contains(read(), "openai_base_url") {
					t.Errorf("connect skipped real wiring: connection=%+v wired=%v\n%s", c, cx.Wired(), read())
				}
				if held && !strings.Contains(read(), `model = "codex/gpt-a"`) {
					t.Errorf("held account did not start on its own model through magpie:\n%s", read())
				}
				if err := cx.Disconnect(); err != nil {
					t.Fatal(err)
				}
				if cx.Wired() || strings.Contains(read(), "openai_base_url") {
					t.Errorf("disconnect did not restore Codex: wired=%v\n%s", cx.Wired(), read())
				}
				if provider.CodexSubagentModel() != "" {
					t.Error("disconnect changed the existing override reset behavior")
				}
			})
		}
	}
}

func TestCodexGatewayOverrideDoesNotDrift(t *testing.T) {
	home, _ := codexHome(t, `{"tokens":{"access_token":"x","id_token":"x.e30.x"}}`, "model = \"gpt-a\"\n")
	cx := codex(home)
	if err := provider.SetCodexSubagentModel("codex/gpt-a"); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	wasStarted, wasLastSeen := started, lastSeen
	started = now.Add(-time.Hour)
	lastSeen = func(string) time.Time { return time.Time{} }
	t.Cleanup(func() { started, lastSeen = wasStarted, wasLastSeen })
	appliedSave(map[string]applied{"codex": {
		At: now.Add(-30 * time.Minute), Fields: map[string]string{"subagent_model": "codex/gpt-a"},
	}})
	cx.LastUsed = func() time.Time { return now.Add(-5 * time.Minute) }
	if d := cx.Drift(); d != nil {
		t.Errorf("direct Codex use was reported as bypassing a gateway-only setting: %+v", d)
	}
	if err := provider.SetCodexSubagentModel(""); err != nil {
		t.Fatal(err)
	}
	if d := cx.Drift(); d != nil {
		t.Errorf("clearing a gateway setting was reported as a changed Codex config: %+v", d)
	}
}
