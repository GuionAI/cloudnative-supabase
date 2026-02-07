package services

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	supabasev1alpha1 "github.com/GuionAI/cloudnative-supabase/api/v1alpha1"
)

func newTestProject(name, namespace string) *supabasev1alpha1.SupabaseProject {
	return &supabasev1alpha1.SupabaseProject{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
}

func TestBuildSequinService(t *testing.T) {
	project := newTestProject("my-app", "test-ns")
	svc := BuildSequinService(project)

	if svc.Name != "my-app-sequin" {
		t.Errorf("Name = %q, want %q", svc.Name, "my-app-sequin")
	}
	if svc.Namespace != "test-ns" {
		t.Errorf("Namespace = %q, want %q", svc.Namespace, "test-ns")
	}
	if svc.Spec.Type != corev1.ServiceTypeClusterIP {
		t.Errorf("Type = %q, want ClusterIP", svc.Spec.Type)
	}

	// Should have HTTP (7376) and metrics (4000) ports
	if len(svc.Spec.Ports) != 2 {
		t.Fatalf("expected 2 ports, got %d", len(svc.Spec.Ports))
	}

	portMap := make(map[string]int32)
	for _, p := range svc.Spec.Ports {
		portMap[p.Name] = p.Port
	}

	if portMap["http"] != 7376 {
		t.Errorf("http port = %d, want 7376", portMap["http"])
	}
	if portMap["metrics"] != 4000 {
		t.Errorf("metrics port = %d, want 4000", portMap["metrics"])
	}
}

func TestBuildPowersyncAPIService(t *testing.T) {
	project := newTestProject("my-app", "test-ns")
	svc := BuildPowersyncAPIService(project)

	if svc.Name != "my-app-powersync-api" {
		t.Errorf("Name = %q, want %q", svc.Name, "my-app-powersync-api")
	}
	if svc.Namespace != "test-ns" {
		t.Errorf("Namespace = %q, want %q", svc.Namespace, "test-ns")
	}
	if svc.Spec.Type != corev1.ServiceTypeClusterIP {
		t.Errorf("Type = %q, want ClusterIP", svc.Spec.Type)
	}

	// Should have HTTP (8080) and metrics (9464) ports
	if len(svc.Spec.Ports) != 2 {
		t.Fatalf("expected 2 ports, got %d", len(svc.Spec.Ports))
	}

	portMap := make(map[string]int32)
	for _, p := range svc.Spec.Ports {
		portMap[p.Name] = p.Port
	}

	if portMap["http"] != 8080 {
		t.Errorf("http port = %d, want 8080", portMap["http"])
	}
	if portMap["metrics"] != 9464 {
		t.Errorf("metrics port = %d, want 9464", portMap["metrics"])
	}
}

func TestBuildMeilisearchService(t *testing.T) {
	project := newTestProject("my-app", "test-ns")
	svc := BuildMeilisearchService(project)

	if svc.Name != "my-app-meilisearch" {
		t.Errorf("Name = %q, want %q", svc.Name, "my-app-meilisearch")
	}
	if svc.Namespace != "test-ns" {
		t.Errorf("Namespace = %q, want %q", svc.Namespace, "test-ns")
	}
	if svc.Spec.Type != corev1.ServiceTypeClusterIP {
		t.Errorf("Type = %q, want ClusterIP", svc.Spec.Type)
	}

	// Single HTTP port (7700)
	if len(svc.Spec.Ports) != 1 {
		t.Fatalf("expected 1 port, got %d", len(svc.Spec.Ports))
	}
	if svc.Spec.Ports[0].Port != 7700 {
		t.Errorf("port = %d, want 7700", svc.Spec.Ports[0].Port)
	}
}

func TestServiceLabels(t *testing.T) {
	project := newTestProject("my-app", "default")

	tests := []struct {
		name      string
		svc       *corev1.Service
		component string
	}{
		{"Sequin", BuildSequinService(project), "sequin"},
		{"Powersync", BuildPowersyncAPIService(project), "powersync-api"},
		{"Meilisearch", BuildMeilisearchService(project), "meilisearch"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			labels := tt.svc.Labels
			if labels["app.kubernetes.io/component"] != tt.component {
				t.Errorf("component label = %q, want %q", labels["app.kubernetes.io/component"], tt.component)
			}

			selector := tt.svc.Spec.Selector
			if selector["app.kubernetes.io/component"] != tt.component {
				t.Errorf("selector component = %q, want %q", selector["app.kubernetes.io/component"], tt.component)
			}
		})
	}
}
