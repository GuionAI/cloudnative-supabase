package deployments

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"
)

func TestBuildKongDeploymentRendersCredentialTemplateBeforeStartup(t *testing.T) {
	project := newTestProject(testNamespace)
	deployment := BuildKongDeployment(project, newTestSecretNames())
	container := deployment.Spec.Template.Spec.Containers[0]

	if len(container.Command) == 0 || len(container.Args) == 0 {
		t.Fatal("Kong starts without rendering its credential template")
	}
	if !strings.Contains(container.Args[0], "${SUPABASE_ANON_KEY}") ||
		!strings.Contains(container.Args[0], "${SUPABASE_SERVICE_KEY}") {
		t.Fatal("Kong startup does not substitute both API credentials")
	}
	for _, variable := range container.Env {
		if variable.Name == "KONG_DECLARATIVE_CONFIG" && variable.Value == "/kong/config/kong.yml" {
			t.Fatal("Kong still reads the unrendered ConfigMap")
		}
	}
}

func TestBuildKongDeployment_DefaultWorkerProcesses(t *testing.T) {
	project := newTestProject(testNamespace)
	secretNames := newTestSecretNames()

	deployment := BuildKongDeployment(project, secretNames)
	for _, env := range deployment.Spec.Template.Spec.Containers[0].Env {
		if env.Name == "KONG_NGINX_WORKER_PROCESSES" {
			if env.Value != "1" {
				t.Fatalf("KONG_NGINX_WORKER_PROCESSES = %q, want 1", env.Value)
			}
			return
		}
	}

	t.Fatal("KONG_NGINX_WORKER_PROCESSES env var not found")
}

func TestDefaultKongResources(t *testing.T) {
	resources := DefaultKongResources()

	if resources.Requests.Memory().Cmp(resource.MustParse("512Mi")) != 0 {
		t.Errorf("memory request = %s, want 512Mi", resources.Requests.Memory())
	}
	if resources.Limits.Memory().Cmp(resource.MustParse("1Gi")) != 0 {
		t.Errorf("memory limit = %s, want 1Gi", resources.Limits.Memory())
	}
}
