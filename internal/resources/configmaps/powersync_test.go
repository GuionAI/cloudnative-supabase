package configmaps

import (
	"encoding/json"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	supabasev1alpha1 "github.com/GuionAI/cloudnative-supabase/api/v1alpha1"
)

const (
	testProjectName = "my-app"
	testNamespace   = "test-ns"
)

func newTestProject(namespace string) *supabasev1alpha1.SupabaseProject {
	return &supabasev1alpha1.SupabaseProject{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testProjectName,
			Namespace: namespace,
		},
		Spec: supabasev1alpha1.SupabaseProjectSpec{
			Powersync: &supabasev1alpha1.PowersyncSpec{},
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
	dbHost := "my-app-rw"

	cm := BuildPowersyncConfigMap(project, dbHost)

	if cm.Name != "my-app-powersync-config" {
		t.Errorf("Name = %q, want %q", cm.Name, "my-app-powersync-config")
	}
	if cm.Namespace != testNamespace {
		t.Errorf("Namespace = %q, want %q", cm.Namespace, testNamespace)
	}

	configJSON, ok := cm.Data["config.json"]
	if !ok {
		t.Fatal("config.json key not found")
	}

	// Parse the JSON to validate structure
	var config powersyncConfig
	if err := json.Unmarshal([]byte(configJSON), &config); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	// Storage
	if config.Storage.Type != "postgresql" {
		t.Errorf("storage type = %q, want postgresql", config.Storage.Type)
	}
	if !strings.Contains(config.Storage.URI, dbHost) {
		t.Errorf("storage URI %q should contain %q", config.Storage.URI, dbHost)
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

	// Client auth
	if !config.ClientAuth.Supabase {
		t.Error("expected supabase auth = true")
	}
	if config.ClientAuth.SupabaseJWTSecret != "{{ env.PS_JWT_SECRET }}" {
		t.Errorf("JWT secret = %q, want env template", config.ClientAuth.SupabaseJWTSecret)
	}

	// Sync rules path
	if config.SyncRules.Path != "/powersync/sync_rules/sync_rules.yaml" {
		t.Errorf("sync rules path = %q", config.SyncRules.Path)
	}
}

func TestBuildPowersyncSyncRulesConfigMap_Default(t *testing.T) {
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
	if !strings.Contains(syncRules, "bucket_definitions") {
		t.Error("default sync rules should contain bucket_definitions")
	}
}

func TestBuildPowersyncSyncRulesConfigMap_Inline(t *testing.T) {
	project := newTestProject("default")
	project.Spec.Powersync.SyncRules.Inline = "bucket_definitions:\n  custom:\n    data:\n      - SELECT * FROM users"

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
