package deployments

import (
	"testing"

	supabasev1alpha1 "github.com/GuionAI/cloudnative-supabase/api/v1alpha1"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/defaults"
)

func TestPowersyncNames(t *testing.T) {
	project := newTestProject("my-app", "default")

	tests := []struct {
		name string
		fn   func(*supabasev1alpha1.SupabaseProject) string
		want string
	}{
		{"API", PowersyncAPIDeploymentName, "my-app-powersync-api"},
		{"Replication", PowersyncReplicationDeploymentName, "my-app-powersync-replication"},
		{"Compact", PowersyncCompactCronJobName, "my-app-powersync-compact"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.fn(project)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildPowersyncAPIDeployment(t *testing.T) {
	project := newTestProject("my-app", "test-ns")
	secretNames := newTestSecretNames()

	dep := BuildPowersyncAPIDeployment(project, secretNames)

	if dep.Name != "my-app-powersync-api" {
		t.Errorf("Name = %q, want %q", dep.Name, "my-app-powersync-api")
	}
	if dep.Namespace != "test-ns" {
		t.Errorf("Namespace = %q, want %q", dep.Namespace, "test-ns")
	}

	// Default replicas = 1 (NormalizeReplicas(0) = 1)
	if *dep.Spec.Replicas != 1 {
		t.Errorf("Replicas = %d, want 1", *dep.Spec.Replicas)
	}

	c := dep.Spec.Template.Spec.Containers[0]

	// Image
	expectedImage := defaults.PowersyncImage + ":" + defaults.PowersyncTag
	if c.Image != expectedImage {
		t.Errorf("image = %q, want %q", c.Image, expectedImage)
	}

	// Command: entry-api.js
	if len(c.Command) != 2 || c.Command[1] != "entry-api.js" {
		t.Errorf("Command = %v, want [node entry-api.js]", c.Command)
	}

	// Ports: HTTP + metrics
	if len(c.Ports) != 2 {
		t.Fatalf("expected 2 ports, got %d", len(c.Ports))
	}
	if c.Ports[0].ContainerPort != PowersyncHTTPPort {
		t.Errorf("HTTP port = %d, want %d", c.Ports[0].ContainerPort, PowersyncHTTPPort)
	}
	if c.Ports[1].ContainerPort != PowersyncMetricsPort {
		t.Errorf("metrics port = %d, want %d", c.Ports[1].ContainerPort, PowersyncMetricsPort)
	}

	// Probes
	if c.LivenessProbe == nil || c.LivenessProbe.HTTPGet.Path != "/api/status" {
		t.Error("expected liveness probe on /api/status")
	}
	if c.ReadinessProbe == nil || c.ReadinessProbe.HTTPGet.Path != "/api/status" {
		t.Error("expected readiness probe on /api/status")
	}

	// Volume mounts
	if len(c.VolumeMounts) != 2 {
		t.Fatalf("expected 2 volume mounts, got %d", len(c.VolumeMounts))
	}

	// Volumes
	volumes := dep.Spec.Template.Spec.Volumes
	if len(volumes) != 2 {
		t.Fatalf("expected 2 volumes, got %d", len(volumes))
	}
	if volumes[0].Name != "config" {
		t.Errorf("volume[0] name = %q, want %q", volumes[0].Name, "config")
	}
	if volumes[1].Name != "sync-rules" {
		t.Errorf("volume[1] name = %q, want %q", volumes[1].Name, "sync-rules")
	}
}

func TestBuildPowersyncAPIDeployment_CustomReplicas(t *testing.T) {
	project := newTestProject("my-app", "default")
	project.Spec.Powersync.API.Replicas = 3
	secretNames := newTestSecretNames()

	dep := BuildPowersyncAPIDeployment(project, secretNames)
	if *dep.Spec.Replicas != 3 {
		t.Errorf("Replicas = %d, want 3", *dep.Spec.Replicas)
	}
}

func TestBuildPowersyncAPIDeployment_CustomNodeOptions(t *testing.T) {
	project := newTestProject("my-app", "default")
	project.Spec.Powersync.API.NodeOptions = "--max-old-space-size=512"
	secretNames := newTestSecretNames()

	dep := BuildPowersyncAPIDeployment(project, secretNames)
	env := dep.Spec.Template.Spec.Containers[0].Env

	for _, e := range env {
		if e.Name == "NODE_OPTIONS" {
			if e.Value != "--max-old-space-size=512" {
				t.Errorf("NODE_OPTIONS = %q, want --max-old-space-size=512", e.Value)
			}
			return
		}
	}
	t.Error("NODE_OPTIONS env var not found")
}

func TestBuildPowersyncReplicationDeployment(t *testing.T) {
	project := newTestProject("my-app", "test-ns")
	secretNames := newTestSecretNames()

	dep := BuildPowersyncReplicationDeployment(project, secretNames)

	if dep.Name != "my-app-powersync-replication" {
		t.Errorf("Name = %q, want %q", dep.Name, "my-app-powersync-replication")
	}

	// Replication is always single instance
	if *dep.Spec.Replicas != 1 {
		t.Errorf("Replicas = %d, want 1 (replication must be single instance)", *dep.Spec.Replicas)
	}

	c := dep.Spec.Template.Spec.Containers[0]

	// Command: entry-replication.js
	if len(c.Command) != 2 || c.Command[1] != "entry-replication.js" {
		t.Errorf("Command = %v, want [node entry-replication.js]", c.Command)
	}

	// Only metrics port (no HTTP)
	if len(c.Ports) != 1 || c.Ports[0].ContainerPort != PowersyncMetricsPort {
		t.Errorf("expected only metrics port %d", PowersyncMetricsPort)
	}

	// Default NODE_OPTIONS for replication
	for _, e := range c.Env {
		if e.Name == "NODE_OPTIONS" {
			if e.Value != "--max-old-space-size=482" {
				t.Errorf("NODE_OPTIONS = %q, want --max-old-space-size=482", e.Value)
			}
			return
		}
	}
	t.Error("NODE_OPTIONS env var not found")
}

func TestBuildPowersyncCompactCronJob(t *testing.T) {
	project := newTestProject("my-app", "test-ns")
	secretNames := newTestSecretNames()

	cj := BuildPowersyncCompactCronJob(project, secretNames)

	if cj.Name != "my-app-powersync-compact" {
		t.Errorf("Name = %q, want %q", cj.Name, "my-app-powersync-compact")
	}
	if cj.Namespace != "test-ns" {
		t.Errorf("Namespace = %q, want %q", cj.Namespace, "test-ns")
	}

	// Default schedule
	if cj.Spec.Schedule != "0 3 * * *" {
		t.Errorf("Schedule = %q, want %q", cj.Spec.Schedule, "0 3 * * *")
	}

	// Command: entry-compact.js
	c := cj.Spec.JobTemplate.Spec.Template.Spec.Containers[0]
	if len(c.Command) != 2 || c.Command[1] != "entry-compact.js" {
		t.Errorf("Command = %v, want [node entry-compact.js]", c.Command)
	}
}

func TestBuildPowersyncCompactCronJob_CustomSchedule(t *testing.T) {
	project := newTestProject("my-app", "default")
	project.Spec.Powersync.Compact.Schedule = "0 2 * * *"
	secretNames := newTestSecretNames()

	cj := BuildPowersyncCompactCronJob(project, secretNames)
	if cj.Spec.Schedule != "0 2 * * *" {
		t.Errorf("Schedule = %q, want %q", cj.Spec.Schedule, "0 2 * * *")
	}
}

func TestBuildPowersyncCompactCronJob_Disabled(t *testing.T) {
	project := newTestProject("my-app", "default")
	project.Spec.Powersync.Compact.Enabled = false
	secretNames := newTestSecretNames()

	cj := BuildPowersyncCompactCronJob(project, secretNames)
	if cj != nil {
		t.Error("expected nil CronJob when compact is disabled")
	}
}

func TestBuildPowersyncEnvVars(t *testing.T) {
	project := newTestProject("my-app", "default")
	secretNames := newTestSecretNames()

	dep := BuildPowersyncAPIDeployment(project, secretNames)
	env := dep.Spec.Template.Spec.Containers[0].Env

	envMap := make(map[string]struct{})
	for _, e := range env {
		envMap[e.Name] = struct{}{}
	}

	required := []string{"POWERSYNC_CONFIG_PATH", "NODE_OPTIONS", "PS_PG_PASSWORD", "PS_POWERSYNC_STORAGE_URI", "PS_POWERSYNC_REPLICATION_URI", "PS_JWT_SECRET"}
	for _, name := range required {
		if _, ok := envMap[name]; !ok {
			t.Errorf("missing required env var: %s", name)
		}
	}
}
