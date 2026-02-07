package deployments

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	supabasev1alpha1 "github.com/GuionAI/cloudnative-supabase/api/v1alpha1"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/defaults"
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
			Sequin: &supabasev1alpha1.SequinSpec{},
			Powersync: &supabasev1alpha1.PowersyncSpec{
				Compact: supabasev1alpha1.PowersyncCompactSpec{Enabled: true},
			},
			Meilisearch: &supabasev1alpha1.MeilisearchSpec{},
		},
	}
}

func newTestSecretNames() *supabasev1alpha1.SecretNamesStatus {
	return &supabasev1alpha1.SecretNamesStatus{
		JWT:                       "test-jwt",
		Sequin:                    "test-sequin",
		SequinPassword:            "test-sequin-password",
		SequinReplicationPassword: "test-sequin-replication-password",
		PowersyncStoragePassword:  "test-powersync-storage-password",
		MeilisearchMasterKey:      "test-meilisearch-master-key",
	}
}

func TestSequinDeploymentName(t *testing.T) {
	project := newTestProject("default")
	got := SequinDeploymentName(project)
	want := "my-app-sequin"
	if got != want {
		t.Errorf("SequinDeploymentName() = %q, want %q", got, want)
	}
}

func TestResolveImage(t *testing.T) {
	tests := []struct {
		name         string
		spec         supabasev1alpha1.ImageSpec
		defaultImage string
		defaultTag   string
		want         string
	}{
		{
			name:         "all defaults",
			spec:         supabasev1alpha1.ImageSpec{},
			defaultImage: "sequin/sequin",
			defaultTag:   "v0.13.25",
			want:         "sequin/sequin:v0.13.25",
		},
		{
			name:         "custom tag",
			spec:         supabasev1alpha1.ImageSpec{Tag: "v1.0.0"},
			defaultImage: "sequin/sequin",
			defaultTag:   "v0.13.25",
			want:         "sequin/sequin:v1.0.0",
		},
		{
			name:         "custom repository",
			spec:         supabasev1alpha1.ImageSpec{Repository: "myorg/sequin"},
			defaultImage: "sequin/sequin",
			defaultTag:   "v0.13.25",
			want:         "myorg/sequin:v0.13.25",
		},
		{
			name:         "custom registry",
			spec:         supabasev1alpha1.ImageSpec{Registry: "ghcr.io"},
			defaultImage: "sequin/sequin",
			defaultTag:   "v0.13.25",
			want:         "ghcr.io/sequin/sequin:v0.13.25",
		},
		{
			name: "full override",
			spec: supabasev1alpha1.ImageSpec{
				Registry:   "ghcr.io",
				Repository: "guionai/sequin",
				Tag:        "flicknote",
			},
			defaultImage: "sequin/sequin",
			defaultTag:   "v0.13.25",
			want:         "ghcr.io/guionai/sequin:flicknote",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveImage(tt.spec, tt.defaultImage, tt.defaultTag)
			if got != tt.want {
				t.Errorf("ResolveImage() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolvePullPolicy(t *testing.T) {
	tests := []struct {
		name string
		spec supabasev1alpha1.ImageSpec
		want corev1.PullPolicy
	}{
		{
			name: "default",
			spec: supabasev1alpha1.ImageSpec{},
			want: corev1.PullIfNotPresent,
		},
		{
			name: "always",
			spec: supabasev1alpha1.ImageSpec{PullPolicy: corev1.PullAlways},
			want: corev1.PullAlways,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolvePullPolicy(tt.spec)
			if got != tt.want {
				t.Errorf("ResolvePullPolicy() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildSequinDeployment(t *testing.T) {
	project := newTestProject(testNamespace)
	secretNames := newTestSecretNames()

	dep := BuildSequinDeployment(project, secretNames)

	// Metadata
	if dep.Name != "my-app-sequin" {
		t.Errorf("Name = %q, want %q", dep.Name, "my-app-sequin")
	}
	if dep.Namespace != "test-ns" {
		t.Errorf("Namespace = %q, want %q", dep.Namespace, "test-ns")
	}

	// Replicas default to 1
	if *dep.Spec.Replicas != 1 {
		t.Errorf("Replicas = %d, want 1", *dep.Spec.Replicas)
	}

	// Container
	containers := dep.Spec.Template.Spec.Containers
	if len(containers) != 1 {
		t.Fatalf("expected 1 container, got %d", len(containers))
	}
	c := containers[0]

	if c.Name != SequinComponentName {
		t.Errorf("container name = %q, want %q", c.Name, SequinComponentName)
	}

	expectedImage := defaults.SequinImage + ":" + defaults.SequinTag
	if c.Image != expectedImage {
		t.Errorf("image = %q, want %q", c.Image, expectedImage)
	}

	// Ports
	if len(c.Ports) != 2 {
		t.Fatalf("expected 2 ports, got %d", len(c.Ports))
	}
	if c.Ports[0].ContainerPort != SequinHTTPPort {
		t.Errorf("HTTP port = %d, want %d", c.Ports[0].ContainerPort, SequinHTTPPort)
	}
	if c.Ports[1].ContainerPort != SequinMetricsPort {
		t.Errorf("metrics port = %d, want %d", c.Ports[1].ContainerPort, SequinMetricsPort)
	}

	// Probes
	if c.LivenessProbe == nil {
		t.Error("expected liveness probe")
	}
	if c.ReadinessProbe == nil {
		t.Error("expected readiness probe")
	}

	// Default resources applied
	if c.Resources.Requests.Memory().Cmp(resource.MustParse("256Mi")) != 0 {
		t.Errorf("memory request = %s, want 256Mi", c.Resources.Requests.Memory())
	}
}

func TestBuildSequinDeployment_CustomReplicas(t *testing.T) {
	project := newTestProject("default")
	project.Spec.Sequin.Replicas = 3
	secretNames := newTestSecretNames()

	dep := BuildSequinDeployment(project, secretNames)
	if *dep.Spec.Replicas != 3 {
		t.Errorf("Replicas = %d, want 3", *dep.Spec.Replicas)
	}
}

func TestBuildSequinDeployment_ExternalRedis(t *testing.T) {
	project := newTestProject("default")
	project.Spec.Sequin.Redis.External = &supabasev1alpha1.ExternalRedisSpec{
		Host: "redis.infra.svc",
		Port: 6380,
	}
	secretNames := newTestSecretNames()

	dep := BuildSequinDeployment(project, secretNames)
	env := dep.Spec.Template.Spec.Containers[0].Env

	var redisURL string
	for _, e := range env {
		if e.Name == "REDIS_URL" {
			redisURL = e.Value
		}
	}

	want := "redis://redis.infra.svc:6380"
	if redisURL != want {
		t.Errorf("REDIS_URL = %q, want %q", redisURL, want)
	}
}

func TestBuildSequinDeployment_BundledRedis(t *testing.T) {
	project := newTestProject("default")
	// External is nil by default - should use bundled Redis URL
	secretNames := newTestSecretNames()

	dep := BuildSequinDeployment(project, secretNames)
	env := dep.Spec.Template.Spec.Containers[0].Env

	var redisURL string
	for _, e := range env {
		if e.Name == "REDIS_URL" {
			redisURL = e.Value
		}
	}

	want := "redis://my-app-sequin-redis:6379"
	if redisURL != want {
		t.Errorf("REDIS_URL = %q, want %q", redisURL, want)
	}
}

func TestBuildSequinDeployment_EnvVars(t *testing.T) {
	project := newTestProject("default")
	secretNames := newTestSecretNames()

	dep := BuildSequinDeployment(project, secretNames)
	env := dep.Spec.Template.Spec.Containers[0].Env

	envMap := make(map[string]corev1.EnvVar)
	for _, e := range env {
		envMap[e.Name] = e
	}

	// Check required env vars exist
	requiredEnvs := []string{"PG_HOSTNAME", "PG_PORT", "PG_DATABASE", "PG_USERNAME", "PG_PASSWORD", "REDIS_URL", "SEQUIN_ENV", "SECRET_KEY_BASE", "VAULT_KEY"}
	for _, name := range requiredEnvs {
		if _, ok := envMap[name]; !ok {
			t.Errorf("missing required env var: %s", name)
		}
	}

	// Check PG_DATABASE is "sequin"
	if envMap["PG_DATABASE"].Value != "sequin" {
		t.Errorf("PG_DATABASE = %q, want %q", envMap["PG_DATABASE"].Value, "sequin")
	}

	// Check secret refs
	if envMap["PG_USERNAME"].ValueFrom.SecretKeyRef.Name != secretNames.SequinPassword {
		t.Errorf("PG_USERNAME secret ref = %q, want %q", envMap["PG_USERNAME"].ValueFrom.SecretKeyRef.Name, secretNames.SequinPassword)
	}
	if envMap["SECRET_KEY_BASE"].ValueFrom.SecretKeyRef.Name != secretNames.Sequin {
		t.Errorf("SECRET_KEY_BASE secret ref = %q, want %q", envMap["SECRET_KEY_BASE"].ValueFrom.SecretKeyRef.Name, secretNames.Sequin)
	}
}

func TestNormalizeSequinResources(t *testing.T) {
	tests := []struct {
		name      string
		resources corev1.ResourceRequirements
		isDefault bool
	}{
		{
			name:      "empty uses defaults",
			resources: corev1.ResourceRequirements{},
			isDefault: true,
		},
		{
			name: "custom preserved",
			resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("1Gi"),
				},
			},
			isDefault: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeSequinResources(tt.resources)
			defaultRes := DefaultSequinResources()
			isDefault := got.Requests.Memory().Cmp(*defaultRes.Requests.Memory()) == 0

			if tt.isDefault != isDefault {
				t.Errorf("isDefault = %v, want %v", isDefault, tt.isDefault)
			}
		})
	}
}
