package fleet

import (
	"strings"
	"testing"
)

func TestManifestIsExplicitAndStrict(t *testing.T) {
	manifest, err := Decode(strings.NewReader(`version: 1
agents:
  - agent_id: quote
    identity_file: agents/quote.identity.yaml
    worker_config: agents/quote.worker.yaml
    enabled: true
`))
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Session != SessionName || len(manifest.Agents) != 1 || !manifest.Agents[0].Enabled {
		t.Fatalf("unexpected manifest: %+v", manifest)
	}
	for name, input := range map[string]string{
		"empty":     "version: 1\nagents: []\n",
		"duplicate": "version: 1\nagents:\n- {agent_id: quote, identity_file: a, worker_config: b}\n- {agent_id: quote, identity_file: c, worker_config: d}\n",
		"unknown":   "version: 1\nagents:\n- {agent_id: quote, identity_file: a, worker_config: b, surprise: true}\n",
		"session":   "version: 1\nsession: other\nagents:\n- {agent_id: quote, identity_file: a, worker_config: b}\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Decode(strings.NewReader(input)); err == nil {
				t.Fatal("invalid Fleet manifest was accepted")
			}
		})
	}
}
