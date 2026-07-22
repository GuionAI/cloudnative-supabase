package deployments

import (
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"
)

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
