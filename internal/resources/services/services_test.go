package services

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	supabasev1alpha1 "github.com/GuionAI/cloudnative-supabase/api/v1alpha1"
)

func TestBuildPowersyncAPIService(t *testing.T) {
	project := &supabasev1alpha1.SupabaseProject{
		ObjectMeta: metav1.ObjectMeta{Name: "my-app", Namespace: "test-ns"},
	}
	svc := BuildPowersyncAPIService(project)

	if svc.Name != "my-app-powersync-api" || svc.Namespace != "test-ns" {
		t.Errorf("unexpected service identity: %s/%s", svc.Namespace, svc.Name)
	}
	if svc.Spec.Type != corev1.ServiceTypeClusterIP {
		t.Errorf("Type = %q, want ClusterIP", svc.Spec.Type)
	}
	if len(svc.Spec.Ports) != 2 || svc.Spec.Ports[0].Port != 8080 || svc.Spec.Ports[1].Port != 9464 {
		t.Errorf("unexpected ports: %v", svc.Spec.Ports)
	}
	if svc.Spec.Selector["app.kubernetes.io/component"] != "powersync-api" {
		t.Errorf("unexpected selector: %v", svc.Spec.Selector)
	}
}
