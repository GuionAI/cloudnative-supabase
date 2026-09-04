package deployments

import "testing"

func TestBuildGatewayDeploymentUsesOnlyOpaqueAndRoleCredentials(t *testing.T) {
	project := newTestProject("default")
	project.Spec.ProjectCredentialsSecret = "project-credentials"
	deployment := BuildGatewayDeployment(project, newTestSecretNames())
	if deployment.Name != "my-app-api-gw" {
		t.Fatalf("gateway name = %q", deployment.Name)
	}
	env := deployment.Spec.Template.Spec.Containers[0].Env
	if len(env) != 4 {
		t.Fatalf("gateway credential env count = %d, want 4", len(env))
	}
	for _, variable := range env {
		if variable.ValueFrom == nil || variable.ValueFrom.SecretKeyRef == nil {
			t.Fatalf("gateway env %s is not Secret-backed", variable.Name)
		}
		if variable.ValueFrom.SecretKeyRef.Name != project.Spec.ProjectCredentialsSecret {
			t.Fatalf("gateway env %s references %q", variable.Name, variable.ValueFrom.SecretKeyRef.Name)
		}
		if variable.Name == "GOTRUE_JWT_SECRET" || variable.Name == "GOTRUE_JWT_KEYS" {
			t.Fatalf("gateway received GoTrue private material: %s", variable.Name)
		}
	}
	if deployment.Spec.Template.Spec.Containers[0].Image != "envoyproxy/envoy:v1.39.0" {
		t.Fatalf("gateway image = %q", deployment.Spec.Template.Spec.Containers[0].Image)
	}
}
