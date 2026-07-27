package deployments

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"
)

func TestBuildKongDeploymentRendersCredentialTemplateBeforeStartup(t *testing.T) {
	project := newTestProject(testNamespace)
	secretNames := newTestSecretNames()
	deployment := BuildKongDeployment(project, secretNames)
	container := deployment.Spec.Template.Spec.Containers[0]

	if len(container.Command) == 0 || len(container.Args) == 0 {
		t.Fatal("Kong starts without rendering its credential template")
	}

	wantSecretKeys := map[string]string{
		"SUPABASE_ANON_KEY":    "anonKey",
		"SUPABASE_SERVICE_KEY": "serviceKey",
	}
	declarativeConfigFound := false
	for _, variable := range container.Env {
		if variable.Name == "KONG_DECLARATIVE_CONFIG" {
			declarativeConfigFound = true
			if variable.Value != "/tmp/kong.yml" {
				t.Fatalf("KONG_DECLARATIVE_CONFIG = %q, want rendered config path", variable.Value)
			}
		}
		secretKey, ok := wantSecretKeys[variable.Name]
		if !ok {
			continue
		}
		if variable.ValueFrom == nil || variable.ValueFrom.SecretKeyRef == nil {
			t.Fatalf("%s is not sourced from the JWT Secret", variable.Name)
		}
		ref := variable.ValueFrom.SecretKeyRef
		if ref.Name != secretNames.JWT || ref.Key != secretKey {
			t.Fatalf("%s secret ref = %s/%s, want %s/%s", variable.Name, ref.Name, ref.Key, secretNames.JWT, secretKey)
		}
		delete(wantSecretKeys, variable.Name)
	}
	if !declarativeConfigFound {
		t.Fatal("KONG_DECLARATIVE_CONFIG is missing")
	}
	if len(wantSecretKeys) != 0 {
		t.Fatalf("missing Kong credential env vars: %v", wantSecretKeys)
	}

	templatePath := filepath.Join(t.TempDir(), "kong-template.yml")
	renderedPath := filepath.Join(t.TempDir(), "kong.yml")
	template := "anon: ${SUPABASE_ANON_KEY}\nservice: ${SUPABASE_SERVICE_KEY}\n"
	if err := os.WriteFile(templatePath, []byte(template), 0o600); err != nil {
		t.Fatal(err)
	}
	script := strings.ReplaceAll(container.Args[0], "/kong/config/kong.yml", templatePath)
	script = strings.ReplaceAll(script, "/tmp/kong.yml", renderedPath)
	script = strings.Replace(script, "exec /docker-entrypoint.sh kong docker-start", ":", 1)
	command := exec.Command(container.Command[0], append(container.Command[1:], script)...)
	command.Env = append(os.Environ(),
		"SUPABASE_ANON_KEY=anon.jwt.value",
		"SUPABASE_SERVICE_KEY=service.jwt.value",
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("render Kong config: %v: %s", err, output)
	}
	rendered, err := os.ReadFile(renderedPath)
	if err != nil {
		t.Fatal(err)
	}
	want := "anon: anon.jwt.value\nservice: service.jwt.value\n"
	if string(rendered) != want {
		t.Fatalf("rendered Kong config = %q, want %q", rendered, want)
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
