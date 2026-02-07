package deployments

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestSequinRedisStatefulSetName(t *testing.T) {
	project := newTestProject("my-app", "default")
	got := SequinRedisStatefulSetName(project)
	if got != "my-app-sequin-redis" {
		t.Errorf("SequinRedisStatefulSetName() = %q, want %q", got, "my-app-sequin-redis")
	}
}

func TestSequinRedisServiceName(t *testing.T) {
	project := newTestProject("my-app", "default")
	got := SequinRedisServiceName(project)
	if got != "my-app-sequin-redis" {
		t.Errorf("SequinRedisServiceName() = %q, want %q", got, "my-app-sequin-redis")
	}
}

func TestBuildSequinRedisStatefulSet(t *testing.T) {
	project := newTestProject("my-app", "test-ns")

	sts := BuildSequinRedisStatefulSet(project)

	// Metadata
	if sts.Name != "my-app-sequin-redis" {
		t.Errorf("Name = %q, want %q", sts.Name, "my-app-sequin-redis")
	}
	if sts.Namespace != "test-ns" {
		t.Errorf("Namespace = %q, want %q", sts.Namespace, "test-ns")
	}

	// Always single replica
	if *sts.Spec.Replicas != 1 {
		t.Errorf("Replicas = %d, want 1", *sts.Spec.Replicas)
	}

	// ServiceName matches StatefulSet name
	if sts.Spec.ServiceName != "my-app-sequin-redis" {
		t.Errorf("ServiceName = %q, want %q", sts.Spec.ServiceName, "my-app-sequin-redis")
	}

	// Container
	containers := sts.Spec.Template.Spec.Containers
	if len(containers) != 1 {
		t.Fatalf("expected 1 container, got %d", len(containers))
	}
	c := containers[0]

	if c.Name != "redis" {
		t.Errorf("container name = %q, want %q", c.Name, "redis")
	}

	// AOF persistence command
	if len(c.Command) != 3 || c.Command[0] != "redis-server" || c.Command[2] != "yes" {
		t.Errorf("Command = %v, want [redis-server --appendonly yes]", c.Command)
	}

	// Port
	if len(c.Ports) != 1 || c.Ports[0].ContainerPort != RedisPort {
		t.Errorf("expected port %d", RedisPort)
	}

	// Probes
	if c.LivenessProbe == nil || c.LivenessProbe.TCPSocket == nil {
		t.Error("expected TCP liveness probe")
	}
	if c.ReadinessProbe == nil || c.ReadinessProbe.Exec == nil {
		t.Error("expected exec readiness probe (redis-cli ping)")
	}

	// Volume mount
	if len(c.VolumeMounts) != 1 || c.VolumeMounts[0].MountPath != "/data" {
		t.Error("expected /data volume mount")
	}

	// Security context: non-root
	podSec := sts.Spec.Template.Spec.SecurityContext
	if podSec == nil || !*podSec.RunAsNonRoot {
		t.Error("expected RunAsNonRoot=true")
	}

	// Container security: drop ALL capabilities
	containerSec := c.SecurityContext
	if containerSec == nil || !*containerSec.AllowPrivilegeEscalation {
		// AllowPrivilegeEscalation should be false
	}
	if containerSec == nil || len(containerSec.Capabilities.Drop) == 0 {
		t.Error("expected dropped capabilities")
	}

	// VolumeClaimTemplates
	if len(sts.Spec.VolumeClaimTemplates) != 1 {
		t.Fatalf("expected 1 VolumeClaimTemplate, got %d", len(sts.Spec.VolumeClaimTemplates))
	}
	pvc := sts.Spec.VolumeClaimTemplates[0]
	if pvc.Name != "data" {
		t.Errorf("PVC name = %q, want %q", pvc.Name, "data")
	}

	// Default storage size
	storageReq := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
	if storageReq.Cmp(resource.MustParse("2Gi")) != 0 {
		t.Errorf("storage = %s, want 2Gi", storageReq.String())
	}
}

func TestBuildSequinRedisStatefulSet_CustomStorage(t *testing.T) {
	project := newTestProject("my-app", "default")
	project.Spec.Sequin.Redis.Storage.Size = "5Gi"
	project.Spec.Sequin.Redis.Storage.StorageClass = "fast-ssd"

	sts := BuildSequinRedisStatefulSet(project)

	pvc := sts.Spec.VolumeClaimTemplates[0]
	storageReq := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
	if storageReq.Cmp(resource.MustParse("5Gi")) != 0 {
		t.Errorf("storage = %s, want 5Gi", storageReq.String())
	}
	if pvc.Spec.StorageClassName == nil || *pvc.Spec.StorageClassName != "fast-ssd" {
		t.Error("expected storageClassName = fast-ssd")
	}
}

func TestBuildSequinRedisService(t *testing.T) {
	project := newTestProject("my-app", "test-ns")
	svc := BuildSequinRedisService(project)

	if svc.Name != "my-app-sequin-redis" {
		t.Errorf("Name = %q, want %q", svc.Name, "my-app-sequin-redis")
	}
	if svc.Namespace != "test-ns" {
		t.Errorf("Namespace = %q, want %q", svc.Namespace, "test-ns")
	}
	if svc.Spec.Type != corev1.ServiceTypeClusterIP {
		t.Errorf("Type = %q, want ClusterIP", svc.Spec.Type)
	}
	if len(svc.Spec.Ports) != 1 || svc.Spec.Ports[0].Port != RedisPort {
		t.Errorf("expected port %d", RedisPort)
	}
}

func TestDefaultRedisResources(t *testing.T) {
	res := DefaultRedisResources()
	if res.Requests.Memory().Cmp(resource.MustParse("128Mi")) != 0 {
		t.Errorf("memory request = %s, want 128Mi", res.Requests.Memory())
	}
	if res.Limits.Memory().Cmp(resource.MustParse("256Mi")) != 0 {
		t.Errorf("memory limit = %s, want 256Mi", res.Limits.Memory())
	}
}
