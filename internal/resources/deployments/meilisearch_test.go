package deployments

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	supabasev1alpha1 "github.com/GuionAI/cloudnative-supabase/api/v1alpha1"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/defaults"
)

func TestMeilisearchStatefulSetName(t *testing.T) {
	project := newTestProject("default")
	got := MeilisearchStatefulSetName(project)
	if got != testProjectName+"-meilisearch" {
		t.Errorf("MeilisearchStatefulSetName() = %q, want %q", got, testProjectName+"-meilisearch")
	}
}

func TestBuildMeilisearchStatefulSet(t *testing.T) {
	project := newTestProject(testNamespace)
	secretNames := newTestSecretNames()

	sts := BuildMeilisearchStatefulSet(project, secretNames)

	// Metadata
	if sts.Name != testProjectName+"-meilisearch" {
		t.Errorf("Name = %q, want %q", sts.Name, testProjectName+"-meilisearch")
	}
	if sts.Namespace != testNamespace {
		t.Errorf("Namespace = %q, want %q", sts.Namespace, testNamespace)
	}

	// Default replica = 1
	if *sts.Spec.Replicas != 1 {
		t.Errorf("Replicas = %d, want 1", *sts.Spec.Replicas)
	}

	// ServiceName
	if sts.Spec.ServiceName != testProjectName+"-meilisearch" {
		t.Errorf("ServiceName = %q, want %q", sts.Spec.ServiceName, testProjectName+"-meilisearch")
	}

	// Container
	c := sts.Spec.Template.Spec.Containers[0]

	expectedImage := defaults.MeilisearchImage + ":" + defaults.MeilisearchTag
	if c.Image != expectedImage {
		t.Errorf("image = %q, want %q", c.Image, expectedImage)
	}

	// Port
	if len(c.Ports) != 1 || c.Ports[0].ContainerPort != MeilisearchHTTPPort {
		t.Errorf("expected port %d", MeilisearchHTTPPort)
	}

	// Env vars
	envMap := make(map[string]corev1.EnvVar)
	for _, e := range c.Env {
		envMap[e.Name] = e
	}

	if envMap["MEILI_ENV"].Value != "production" {
		t.Errorf("MEILI_ENV = %q, want %q", envMap["MEILI_ENV"].Value, "production")
	}
	if envMap["MEILI_NO_ANALYTICS"].Value != "true" {
		t.Errorf("MEILI_NO_ANALYTICS = %q, want %q", envMap["MEILI_NO_ANALYTICS"].Value, "true")
	}
	if envMap["MEILI_MASTER_KEY"].ValueFrom == nil || envMap["MEILI_MASTER_KEY"].ValueFrom.SecretKeyRef.Key != "masterKey" {
		t.Error("MEILI_MASTER_KEY should reference secret masterKey")
	}

	// Probes
	if c.LivenessProbe == nil || c.LivenessProbe.HTTPGet.Path != "/health" {
		t.Error("expected liveness probe on /health")
	}
	if c.ReadinessProbe == nil || c.ReadinessProbe.HTTPGet.Path != "/health" {
		t.Error("expected readiness probe on /health")
	}

	// Volume mount
	if len(c.VolumeMounts) != 1 || c.VolumeMounts[0].MountPath != "/meili_data" {
		t.Error("expected /meili_data volume mount")
	}

	// VolumeClaimTemplates
	if len(sts.Spec.VolumeClaimTemplates) != 1 {
		t.Fatalf("expected 1 VolumeClaimTemplate, got %d", len(sts.Spec.VolumeClaimTemplates))
	}
	pvc := sts.Spec.VolumeClaimTemplates[0]
	storageReq := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
	if storageReq.Cmp(resource.MustParse("10Gi")) != 0 {
		t.Errorf("storage = %s, want 10Gi", storageReq.String())
	}

	// Default: no storage class
	if pvc.Spec.StorageClassName != nil {
		t.Errorf("expected nil StorageClassName, got %q", *pvc.Spec.StorageClassName)
	}
}

func TestBuildMeilisearchStatefulSet_CustomStorage(t *testing.T) {
	project := newTestProject("default")
	project.Spec.Meilisearch.Persistence.Size = "50Gi"
	project.Spec.Meilisearch.Persistence.StorageClass = "longhorn"
	secretNames := newTestSecretNames()

	sts := BuildMeilisearchStatefulSet(project, secretNames)

	pvc := sts.Spec.VolumeClaimTemplates[0]
	storageReq := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
	if storageReq.Cmp(resource.MustParse("50Gi")) != 0 {
		t.Errorf("storage = %s, want 50Gi", storageReq.String())
	}
	if pvc.Spec.StorageClassName == nil || *pvc.Spec.StorageClassName != "longhorn" {
		t.Error("expected storageClassName = longhorn")
	}
}

func TestBuildMeilisearchStatefulSet_MasterKeySecretRef(t *testing.T) {
	project := newTestProject("default")
	project.Spec.Meilisearch.MasterKeySecretRef = "my-existing-key"
	secretNames := newTestSecretNames()

	sts := BuildMeilisearchStatefulSet(project, secretNames)
	c := sts.Spec.Template.Spec.Containers[0]

	for _, e := range c.Env {
		if e.Name == "MEILI_MASTER_KEY" {
			if e.ValueFrom.SecretKeyRef.Name != "my-existing-key" {
				t.Errorf("MEILI_MASTER_KEY secret = %q, want %q", e.ValueFrom.SecretKeyRef.Name, "my-existing-key")
			}
			return
		}
	}
	t.Error("MEILI_MASTER_KEY env var not found")
}

func TestDefaultMeilisearchResources(t *testing.T) {
	res := DefaultMeilisearchResources()
	if res.Requests.Memory().Cmp(resource.MustParse("512Mi")) != 0 {
		t.Errorf("memory request = %s, want 512Mi", res.Requests.Memory())
	}
	if res.Limits.Memory().Cmp(resource.MustParse("2Gi")) != 0 {
		t.Errorf("memory limit = %s, want 2Gi", res.Limits.Memory())
	}
}

func TestNormalizeMeilisearchResources(t *testing.T) {
	// Empty uses defaults
	got := normalizeMeilisearchResources(corev1.ResourceRequirements{})
	if got.Requests.Memory().Cmp(resource.MustParse("512Mi")) != 0 {
		t.Errorf("expected default 512Mi, got %s", got.Requests.Memory())
	}

	// Custom preserved
	custom := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse("1Gi"),
		},
	}
	got = normalizeMeilisearchResources(custom)
	if got.Requests.Memory().Cmp(resource.MustParse("1Gi")) != 0 {
		t.Errorf("expected custom 1Gi, got %s", got.Requests.Memory())
	}
}

func TestBuildMeilisearchStatefulSet_ImagePullSecrets(t *testing.T) {
	project := newTestProject("default")
	project.Spec.ImagePullSecrets = []corev1.LocalObjectReference{
		{Name: "my-registry-secret"},
	}
	secretNames := newTestSecretNames()

	sts := BuildMeilisearchStatefulSet(project, secretNames)
	if len(sts.Spec.Template.Spec.ImagePullSecrets) != 1 {
		t.Fatal("expected 1 image pull secret")
	}
	if sts.Spec.Template.Spec.ImagePullSecrets[0].Name != "my-registry-secret" {
		t.Errorf("imagePullSecret = %q, want %q", sts.Spec.Template.Spec.ImagePullSecrets[0].Name, "my-registry-secret")
	}
}

func TestBuildMeilisearchStatefulSet_CustomReplicas(t *testing.T) {
	project := newTestProject("default")
	project.Spec.Meilisearch = &supabasev1alpha1.MeilisearchSpec{Replicas: 2}
	secretNames := newTestSecretNames()

	sts := BuildMeilisearchStatefulSet(project, secretNames)
	if *sts.Spec.Replicas != 2 {
		t.Errorf("Replicas = %d, want 2", *sts.Spec.Replicas)
	}
}
