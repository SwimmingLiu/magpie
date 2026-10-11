package agent

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/provider"
)

// Two ChatGPT accounts need an explicit gateway choice even when Codex is
// disconnected and uses a CUSTOM provider; reading choices must not wire it.
func TestCodexMultiAccountGatewayChoice(t *testing.T) {
	for _, tc := range []struct {
		name                                                string
		spareOn, paused, lapsed, providerOff, external, api bool
		want                                                bool
	}{
		{name: "two accounts", spareOn: true, want: true},
		{name: "external supplier too", spareOn: true, external: true, want: true},
		{name: "one account"},
		{name: "active account paused", spareOn: true, paused: true},
		{name: "spare sign-in lapsed", spareOn: true, lapsed: true},
		{name: "provider off", spareOn: true, providerOff: true},
		{name: "single account in API mode", api: true, want: true},
		{name: "API already offers gateway model", spareOn: true, api: true, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			claims := func(v map[string]any) string {
				b, err := json.Marshal(v)
				if err != nil {
					t.Fatal(err)
				}
				return "h." + base64.RawURLEncoding.EncodeToString(b) + ".s"
			}
			auth := func(user, id string) map[string]any {
				return map[string]any{"auth_mode": "chatgpt", "tokens": map[string]any{
					"id_token":      claims(map[string]any{"email": user}),
					"access_token":  claims(map[string]any{"exp": time.Now().Add(time.Hour).Unix()}),
					"refresh_token": "fixture-" + id, "account_id": id}}
			}
			me, err := json.Marshal(auth("account-a@example.invalid", "fixture-a"))
			if err != nil {
				t.Fatal(err)
			}
			const config = "model = \"gpt-6.1-sol\"\nmodel_provider = \"custom\"\n\n[model_providers.custom]\nname = \"CUSTOM\"\nwire_api = \"responses\"\n\n[agents]\nmax_threads = 6\n"
			home, read := codexHome(t, string(me), config)
			if !tc.external {
				if err := provider.Delete("fake"); err != nil {
					t.Fatal(err)
				}
			}
			lapsed := ""
			if tc.lapsed {
				lapsed = "fixture sign-in expired"
			}
			logins, err := json.Marshal([]map[string]any{
				{"agent": "codex", "user": "account-a@example.invalid", "paused": tc.paused, "auth": auth("account-a@example.invalid", "fixture-a")},
				{"agent": "codex", "user": "account-b@example.invalid", "on": tc.spareOn, "lapsed": lapsed, "auth": auth("account-b@example.invalid", "fixture-b")},
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(filepath.Dir(provider.Path()), "logins.json"), logins, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(home, ".codex", "models_cache.json"), []byte(`{"models":[{"slug":"gpt-6.1-sol","display_name":"GPT-6.1-Sol","priority":1}]}`), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := provider.Save(provider.Provider{ID: "codex", Models: []string{"gpt-6.1-sol"}, Off: tc.providerOff}); err != nil {
				t.Fatal(err)
			}
			stash(map[string]string{"codex" + disconnectedKey: "1"})
			cx := codex(home)
			if tc.api {
				if err := cx.Field("login").Set("api"); err != nil {
					t.Fatal(err)
				}
				if err := cx.Field("model").Set("codex/gpt-6.1-sol"); err != nil {
					t.Fatal(err)
				}
			}
			before := read()
			var direct, via int
			for _, o := range cx.Field("model").Options(nil) {
				if o.Value == "gpt-6.1-sol" && o.Direct == "custom" {
					direct++
				}
				if o.Value == "codex/gpt-6.1-sol" && o.Ref == o.Value {
					via++
				}
			}
			if read() != before {
				t.Fatal("listing choices changed Codex config")
			}
			if !tc.api && direct != 1 {
				t.Fatalf("CUSTOM direct choice count = %d, want 1", direct)
			}
			want := 0
			if tc.want {
				want = 1
			}
			if via != want {
				t.Fatalf("Codex gateway choice count = %d, want %d", via, want)
			}
			// A stopped WSL distro uses the shared picker without opening
			// its files; it must not retain the old blanket exclusion.
			if !tc.api {
				for _, k := range wslKinds {
					if k.id != "codex" {
						continue
					}
					var n int
					for _, o := range k.asleep("model")(nil) {
						if o.Value == "codex/gpt-6.1-sol" {
							n++
						}
					}
					if n != want {
						t.Fatalf("stopped WSL Codex gateway choices = %d, want %d", n, want)
					}
				}
			}
			if !tc.want || tc.api {
				return
			}
			if err := cx.Pick("model", "gpt-6.1-sol"); err != nil {
				t.Fatal(err)
			}
			if cx.Wired() || !strings.Contains(read(), `model_provider = "custom"`) {
				t.Fatal("native model pick took over CUSTOM wiring")
			}
			subagent := provider.CodexSubagentModel()
			if err := cx.Pick("model", "codex/gpt-6.1-sol"); err != nil {
				t.Fatal(err)
			}
			if provider.CodexSubagentModel() != subagent {
				t.Fatal("main model pick changed the gateway subagent override")
			}
			if got := cx.Field("model").Get(); got != "codex/gpt-6.1-sol" || !cx.Wired() {
				t.Fatalf("gateway pick = %q, wired = %v", got, cx.Wired())
			}
			if cfg := read(); !strings.Contains(cfg, "openai_base_url") || !strings.Contains(cfg, "max_threads = 6") || strings.Contains(cfg, "default_subagent_model") {
				t.Fatalf("gateway pick wrote wrong config:\n%s", cfg)
			}
			if err := cx.Disconnect(); err != nil {
				t.Fatal(err)
			}
			if cfg := read(); !strings.Contains(cfg, `model_provider = "custom"`) || !strings.Contains(cfg, "[model_providers.custom]") || strings.Contains(cfg, "openai_base_url") {
				t.Fatalf("CUSTOM not restored:\n%s", cfg)
			}
		})
	}
}
