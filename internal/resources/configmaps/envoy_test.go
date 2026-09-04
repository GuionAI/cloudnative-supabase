package configmaps

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	supabasev1alpha1 "github.com/GuionAI/cloudnative-supabase/api/v1alpha1"
)

func TestBuildEnvoyConfigMapRendersManagedProfile(t *testing.T) {
	project := &supabasev1alpha1.SupabaseProject{ObjectMeta: metav1.ObjectMeta{Name: "example", Namespace: "default"}}
	config := BuildEnvoyConfigMap(project)
	for _, key := range []string{"envoy.yaml", "cds.yaml", "lds.template.yaml", "docker-entrypoint.sh"} {
		if config.Data[key] == "" {
			t.Fatalf("missing Envoy asset %q", key)
		}
	}
	for _, key := range []string{"envoy.yaml", "cds.yaml", "lds.template.yaml"} {
		var document any
		if err := yaml.Unmarshal([]byte(config.Data[key]), &document); err != nil {
			t.Fatalf("asset %s is not valid YAML: %v", key, err)
		}
	}
	lds := config.Data["lds.template.yaml"]
	for _, value := range []string{"${SUPABASE_PUBLISHABLE_KEY}", "${SUPABASE_SECRET_KEY}", "${SUPABASE_ANON_ROLE_JWT}", "${SUPABASE_SERVICE_ROLE_JWT}"} {
		if !strings.Contains(lds, value) {
			t.Fatalf("LDS template missing placeholder %q", value)
		}
	}
	if strings.Contains(strings.ToLower(lds), "kong") || strings.Contains(lds, "${ANON_KEY}") || strings.Contains(lds, "${SERVICE_ROLE_KEY}") {
		t.Fatal("LDS template contains a legacy gateway credential or route")
	}
}

func TestBuildJWKSConfigMapUsesPublicProjection(t *testing.T) {
	project := &supabasev1alpha1.SupabaseProject{ObjectMeta: metav1.ObjectMeta{Name: "example", Namespace: "default"}}
	cm := BuildJWKSConfigMap(project, `{"keys":[]}`)
	if cm.Name != "example-auth-jwks" || cm.Data[JWKSDataKey] != `{"keys":[]}` {
		t.Fatalf("JWKS ConfigMap = %#v", cm)
	}
}
