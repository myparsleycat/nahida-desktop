package actions

import (
	"encoding/json"
	"testing"

	"nahida.live/desktop/internal/setting"
)

func TestRegistryPrepareSettingRisk(t *testing.T) {
	t.Parallel()
	registry := NewRegistry(Dependencies{Settings: setting.New(nil)})
	tests := []struct {
		name  string
		key   string
		value any
		risk  Risk
	}{
		{name: "ordinary setting", key: setting.KeyGeneralLanguage, value: "ko", risk: RiskWrite},
		{name: "startup setting", key: setting.KeyGeneralRunOnStartup, value: true, risk: RiskConfirm},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			raw, _ := json.Marshal(map[string]any{"key": test.key, "value": test.value})
			plan, err := registry.Prepare("global", nil, Call{ActionID: "settings.set", Arguments: raw})
			if err != nil || plan.Risk != test.risk || (plan.Proposal != nil) != (plan.Risk == RiskConfirm) {
				t.Fatalf("prepare = risk %q proposal %#v, %v", plan.Risk, plan.Proposal, err)
			}
		})
	}
}

func TestRegistryRejectsWrongSettingType(t *testing.T) {
	t.Parallel()
	registry := NewRegistry(Dependencies{Settings: setting.New(nil)})
	_, err := registry.Prepare("global", nil, Call{
		ActionID:  "settings.set",
		Arguments: json.RawMessage(`{"key":"general.runInBackground","value":"true"}`),
	})
	if err == nil {
		t.Fatal("wrong setting value type unexpectedly accepted")
	}
}
