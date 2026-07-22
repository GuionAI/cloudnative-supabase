package deployments

import (
	"slices"
	"testing"

	supabasev1alpha1 "github.com/GuionAI/cloudnative-supabase/api/v1alpha1"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/defaults"
	corev1 "k8s.io/api/core/v1"
)

func TestPowersyncCompactUsesImagePullSecrets(t *testing.T) {
	project := newTestProject("default")
	project.Spec.ImagePullSecrets = []corev1.LocalObjectReference{{Name: "registry-auth"}}

	cronJob := BuildPowersyncCompactCronJob(project, newTestSecretNames())
	pullSecrets := cronJob.Spec.JobTemplate.Spec.Template.Spec.ImagePullSecrets
	if len(pullSecrets) != 1 || pullSecrets[0].Name != "registry-auth" {
		t.Errorf("ImagePullSecrets = %v", pullSecrets)
	}
}

func TestPowersyncNames(t *testing.T) {
	project := newTestProject("default")

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
	project := newTestProject(testNamespace)
	secretNames := newTestSecretNames()

	dep := BuildPowersyncAPIDeployment(project, secretNames)

	if dep.Name != "my-app-powersync-api" {
		t.Errorf("Name = %q, want %q", dep.Name, "my-app-powersync-api")
	}
	if dep.Namespace != testNamespace {
		t.Errorf("Namespace = %q, want %q", dep.Namespace, testNamespace)
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

	// The image entrypoint is node service/lib/entry.js.
	if len(c.Command) != 0 {
		t.Errorf("Command = %v, want image entrypoint", c.Command)
	}
	if len(c.Args) != 3 || c.Args[0] != "start" || c.Args[1] != "-r" || c.Args[2] != "api" {
		t.Errorf("Args = %v, want [start -r api]", c.Args)
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

	// PowerSync 1.20 filesystem probes.
	assertFreshPowersyncLivenessProbe(t, c.LivenessProbe)
	if c.ReadinessProbe == nil || c.ReadinessProbe.Exec == nil || c.ReadinessProbe.Exec.Command[1] != "/app/.probes/ready" {
		t.Error("expected filesystem readiness probe")
	}
	if c.StartupProbe == nil || c.StartupProbe.Exec == nil || c.StartupProbe.Exec.Command[1] != "/app/.probes/startup" {
		t.Error("expected filesystem startup probe")
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
	project := newTestProject("default")
	project.Spec.Powersync.API.Replicas = 3
	secretNames := newTestSecretNames()

	dep := BuildPowersyncAPIDeployment(project, secretNames)
	if *dep.Spec.Replicas != 3 {
		t.Errorf("Replicas = %d, want 3", *dep.Spec.Replicas)
	}
}

func TestBuildPowersyncAPIDeployment_CustomNodeOptions(t *testing.T) {
	const (
		nodeOptionsName  = "NODE_OPTIONS"
		nodeOptionsValue = "--max-old-space-size=512"
	)
	project := newTestProject("default")
	project.Spec.Powersync.API.NodeOptions = nodeOptionsValue
	secretNames := newTestSecretNames()

	dep := BuildPowersyncAPIDeployment(project, secretNames)
	env := dep.Spec.Template.Spec.Containers[0].Env

	for _, e := range env {
		if e.Name == nodeOptionsName {
			if e.Value != nodeOptionsValue {
				t.Errorf("NODE_OPTIONS = %q, want %s", e.Value, nodeOptionsValue)
			}
			return
		}
	}
	t.Error("NODE_OPTIONS env var not found")
}

func TestBuildPowersyncReplicationDeployment(t *testing.T) {
	project := newTestProject(testNamespace)
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

	if len(c.Args) != 3 || c.Args[0] != "start" || c.Args[1] != "-r" || c.Args[2] != "sync" {
		t.Errorf("Args = %v, want [start -r sync]", c.Args)
	}

	// Only metrics port (no HTTP)
	if len(c.Ports) != 1 || c.Ports[0].ContainerPort != PowersyncMetricsPort {
		t.Errorf("expected only metrics port %d", PowersyncMetricsPort)
	}
	assertFreshPowersyncLivenessProbe(t, c.LivenessProbe)

	// Default NODE_OPTIONS for replication
	for _, e := range c.Env {
		if e.Name == "NODE_OPTIONS" {
			if e.Value != "--max-old-space-size=230" {
				t.Errorf("NODE_OPTIONS = %q, want --max-old-space-size=230", e.Value)
			}
			return
		}
	}
	t.Error("NODE_OPTIONS env var not found")
}

func assertFreshPowersyncLivenessProbe(t *testing.T, probe *corev1.Probe) {
	t.Helper()
	want := []string{
		"sh",
		"-ec",
		`age=$(( $(date +%s) - $(stat -c %Y /app/.probes/poll) )); [ "$age" -lt 10 ]`,
	}
	if probe == nil || probe.Exec == nil {
		t.Fatal("expected exec liveness probe")
	}
	if !slices.Equal(probe.Exec.Command, want) {
		t.Errorf("liveness command = %v, want %v", probe.Exec.Command, want)
	}
}

func TestBuildPowersyncCompactCronJob(t *testing.T) {
	project := newTestProject(testNamespace)
	secretNames := newTestSecretNames()

	cj := BuildPowersyncCompactCronJob(project, secretNames)

	if cj.Name != "my-app-powersync-compact" {
		t.Errorf("Name = %q, want %q", cj.Name, "my-app-powersync-compact")
	}
	if cj.Namespace != testNamespace {
		t.Errorf("Namespace = %q, want %q", cj.Namespace, testNamespace)
	}

	// Default schedule
	if cj.Spec.Schedule != "0 3 * * *" {
		t.Errorf("Schedule = %q, want %q", cj.Spec.Schedule, "0 3 * * *")
	}

	// Compact uses the image entrypoint.
	c := cj.Spec.JobTemplate.Spec.Template.Spec.Containers[0]
	if len(c.Args) != 1 || c.Args[0] != "compact" {
		t.Errorf("Args = %v, want [compact]", c.Args)
	}
	if cj.Spec.ConcurrencyPolicy != "Forbid" {
		t.Errorf("ConcurrencyPolicy = %q, want Forbid", cj.Spec.ConcurrencyPolicy)
	}

	resources := c.Resources
	if got := resources.Requests.Memory().String(); got != "256Mi" {
		t.Errorf("memory request = %q, want 256Mi", got)
	}
	if got := resources.Requests.Cpu().String(); got != "100m" {
		t.Errorf("CPU request = %q, want 100m", got)
	}
	if got := resources.Limits.Memory().String(); got != "1Gi" {
		t.Errorf("memory limit = %q, want 1Gi", got)
	}
	if got := resources.Limits.Cpu().String(); got != "1" {
		t.Errorf("CPU limit = %q, want 1", got)
	}

	for _, env := range c.Env {
		if env.Name == "NODE_OPTIONS" {
			if env.Value != "--max-old-space-size=512" {
				t.Errorf("NODE_OPTIONS = %q, want --max-old-space-size=512", env.Value)
			}
			return
		}
	}
	t.Error("NODE_OPTIONS env var not found")
}

func TestBuildPowersyncCompactCronJob_CustomSchedule(t *testing.T) {
	project := newTestProject("default")
	project.Spec.Powersync.Compact.Schedule = "0 2 * * *"
	secretNames := newTestSecretNames()

	cj := BuildPowersyncCompactCronJob(project, secretNames)
	if cj.Spec.Schedule != "0 2 * * *" {
		t.Errorf("Schedule = %q, want %q", cj.Spec.Schedule, "0 2 * * *")
	}
}

func TestBuildPowersyncCompactCronJob_Disabled(t *testing.T) {
	project := newTestProject("default")
	project.Spec.Powersync.Compact.Enabled = false
	secretNames := newTestSecretNames()

	cj := BuildPowersyncCompactCronJob(project, secretNames)
	if cj != nil {
		t.Error("expected nil CronJob when compact is disabled")
	}
}

func TestBuildPowersyncEnvVars(t *testing.T) {
	project := newTestProject("default")
	secretNames := newTestSecretNames()

	dep := BuildPowersyncAPIDeployment(project, secretNames)
	env := dep.Spec.Template.Spec.Containers[0].Env

	envMap := make(map[string]struct{})
	for _, e := range env {
		envMap[e.Name] = struct{}{}
	}

	required := []string{"POWERSYNC_CONFIG_PATH", "NODE_OPTIONS", "LOG_FORMAT", "METRICS_PORT", "MICRO_PROBE_TYPE", "PS_STORAGE_PASSWORD", "PS_REPLICATION_PASSWORD", "PS_POWERSYNC_STORAGE_URI", "PS_POWERSYNC_REPLICATION_URI", "PS_JWT_SECRET"}
	for _, name := range required {
		if _, ok := envMap[name]; !ok {
			t.Errorf("missing required env var: %s", name)
		}
	}
}
