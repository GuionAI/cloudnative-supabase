package configmaps

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	supabasev1alpha1 "github.com/GuionAI/cloudnative-supabase/api/v1alpha1"
)

const (
	testProjectName = "my-app"
	testNamespace   = "test-ns"
)

type powersyncConfig struct {
	Storage struct {
		Type string `json:"type"`
		URI  string `json:"uri"`
	} `json:"storage"`
	Replication struct {
		Connections []struct {
			Type string `json:"type"`
			URI  string `json:"uri"`
			Tag  string `json:"tag"`
		} `json:"connections"`
	} `json:"replication"`
	ClientAuth struct {
		Supabase bool     `json:"supabase"`
		JWKSURI  string   `json:"jwks_uri"`
		Audience []string `json:"audience"`
	} `json:"client_auth"`
	SyncRules struct {
		Path        string `json:"path"`
		ExitOnError bool   `json:"exit_on_error"`
	} `json:"sync_rules"`
}

func newTestProject(namespace string) *supabasev1alpha1.SupabaseProject {
	return &supabasev1alpha1.SupabaseProject{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testProjectName,
			Namespace: namespace,
		},
		Spec: supabasev1alpha1.SupabaseProjectSpec{
			Powersync: &supabasev1alpha1.PowersyncSpec{
				SyncRules: supabasev1alpha1.SyncRulesSpec{
					Inline: "config:\n  edition: 3\nstreams:\n  notes:\n    auto_subscribe: true\n    query: SELECT id FROM notes WHERE user_id = auth.user_id()",
				},
			},
		},
	}
}

func TestPowersyncConfigMapName(t *testing.T) {
	project := newTestProject("default")
	got := PowersyncConfigMapName(project)
	if got != "my-app-powersync-config" {
		t.Errorf("PowersyncConfigMapName() = %q, want %q", got, "my-app-powersync-config")
	}
}

func TestPowersyncSyncRulesConfigMapName(t *testing.T) {
	project := newTestProject("default")
	got := PowersyncSyncRulesConfigMapName(project)
	if got != "my-app-powersync-sync-rules" {
		t.Errorf("PowersyncSyncRulesConfigMapName() = %q, want %q", got, "my-app-powersync-sync-rules")
	}
}

func TestSyncRulesConfigMapName(t *testing.T) {
	tests := []struct {
		name         string
		configMapRef string
		want         string
	}{
		{
			name:         "auto-generated",
			configMapRef: "",
			want:         "my-app-powersync-sync-rules",
		},
		{
			name:         "external ref",
			configMapRef: "my-custom-rules",
			want:         "my-custom-rules",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			project := newTestProject("default")
			project.Spec.Powersync.SyncRules.ConfigMapRef = tt.configMapRef

			got := SyncRulesConfigMapName(project)
			if got != tt.want {
				t.Errorf("SyncRulesConfigMapName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildPowersyncConfigMap(t *testing.T) {
	project := newTestProject(testNamespace)
	cm := BuildPowersyncConfigMap(project)

	if cm.Name != "my-app-powersync-config" {
		t.Errorf("Name = %q, want %q", cm.Name, "my-app-powersync-config")
	}
	if cm.Namespace != testNamespace {
		t.Errorf("Namespace = %q, want %q", cm.Namespace, testNamespace)
	}

	configYAML, ok := cm.Data["config.yaml"]
	if !ok {
		t.Fatal("config.yaml key not found")
	}

	// Parse the YAML to validate structure. The raw assertions below preserve
	// the !env tags that PowerSync resolves before decoding the values.
	var config powersyncConfig
	if err := yaml.Unmarshal([]byte(configYAML), &config); err != nil {
		t.Fatalf("invalid YAML: %v", err)
	}

	// Storage
	if config.Storage.Type != "postgresql" {
		t.Errorf("storage type = %q, want postgresql", config.Storage.Type)
	}
	if config.Storage.URI != "PS_POWERSYNC_STORAGE_URI" {
		t.Errorf("storage URI = %q, want environment template", config.Storage.URI)
	}
	if !strings.Contains(configYAML, "uri: !env PS_POWERSYNC_STORAGE_URI") {
		t.Error("storage URI must use PowerSync's !env tag")
	}

	// Replication
	if len(config.Replication.Connections) != 1 {
		t.Fatalf("expected 1 replication connection, got %d", len(config.Replication.Connections))
	}
	conn := config.Replication.Connections[0]
	if conn.Type != "postgresql" {
		t.Errorf("connection type = %q, want postgresql", conn.Type)
	}
	if conn.Tag != "default" {
		t.Errorf("connection tag = %q, want default", conn.Tag)
	}
	if conn.URI != "PS_POWERSYNC_REPLICATION_URI" {
		t.Errorf("replication URI = %q, want environment template", conn.URI)
	}
	if !strings.Contains(configYAML, "uri: !env PS_POWERSYNC_REPLICATION_URI") {
		t.Error("replication URI must use PowerSync's !env tag")
	}

	// Client auth
	if config.ClientAuth.Supabase {
		t.Error("legacy Supabase HMAC mode must be disabled")
	}
	if config.ClientAuth.JWKSURI == "" {
		t.Error("JWKS URI must be configured")
	}
	if len(config.ClientAuth.Audience) != 1 || config.ClientAuth.Audience[0] != "authenticated" {
		t.Errorf("audience = %v, want authenticated", config.ClientAuth.Audience)
	}

	// Sync rules path
	if config.SyncRules.Path != "/powersync/sync_rules/sync_rules.yaml" {
		t.Errorf("sync rules path = %q", config.SyncRules.Path)
	}
	if !config.SyncRules.ExitOnError {
		t.Error("sync rules must fail startup when invalid")
	}
}

func TestBuildPowersyncSyncRulesConfigMap_UsesSyncStreams(t *testing.T) {
	project := newTestProject(testNamespace)

	cm := BuildPowersyncSyncRulesConfigMap(project)
	if cm == nil {
		t.Fatal("expected non-nil ConfigMap")
		return
	}
	if cm.Name != "my-app-powersync-sync-rules" {
		t.Errorf("Name = %q, want %q", cm.Name, "my-app-powersync-sync-rules")
	}

	syncRules, ok := cm.Data["sync_rules.yaml"]
	if !ok {
		t.Fatal("sync_rules.yaml key not found")
	}
	if !strings.Contains(syncRules, "edition: 3") || !strings.Contains(syncRules, "streams:") {
		t.Error("sync config should contain edition 3 streams")
	}
}

func TestBuildPowersyncSyncRulesConfigMap_RequiresConfiguration(t *testing.T) {
	project := newTestProject(testNamespace)
	project.Spec.Powersync.SyncRules.Inline = ""

	cm := BuildPowersyncSyncRulesConfigMap(project)
	if cm != nil {
		t.Error("expected nil ConfigMap when no sync config is provided")
	}
}

func TestBuildPowersyncSyncRulesConfigMap_Inline(t *testing.T) {
	project := newTestProject("default")
	project.Spec.Powersync.SyncRules.Inline = "config:\n  edition: 3\nstreams:\n  custom:\n    query: SELECT id FROM users WHERE id = auth.user_id()"

	cm := BuildPowersyncSyncRulesConfigMap(project)
	if cm == nil {
		t.Fatal("expected non-nil ConfigMap")
		return
	}
	if !strings.Contains(cm.Data["sync_rules.yaml"], "custom") {
		t.Error("expected inline sync rules to be used")
	}
}

func TestBuildPowersyncSyncRulesConfigMap_ExternalRef(t *testing.T) {
	project := newTestProject("default")
	project.Spec.Powersync.SyncRules.ConfigMapRef = "my-external-rules"

	cm := BuildPowersyncSyncRulesConfigMap(project)

	if cm != nil {
		t.Error("expected nil ConfigMap when external ConfigMapRef is set")
	}
}
