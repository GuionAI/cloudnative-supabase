package cnpg

import (
	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	supabasev1alpha1 "github.com/GuionAI/cloudnative-supabase/api/v1alpha1"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/common"
)

// BuildPowerSyncPublication creates the CNPG resource that manages PowerSync's
// PostgreSQL publication, including future tables in the public schema.
func BuildPowerSyncPublication(project *supabasev1alpha1.SupabaseProject) *cnpgv1.Publication {
	return &cnpgv1.Publication{
		ObjectMeta: metav1.ObjectMeta{
			Name:      project.Name + "-powersync",
			Namespace: project.Namespace,
			Labels:    common.ComponentLabels(project, "powersync-publication"),
		},
		Spec: cnpgv1.PublicationSpec{
			ClusterRef: corev1.LocalObjectReference{Name: ClusterName(project)},
			Name:       "powersync",
			DBName:     common.DatabaseName,
			Target: cnpgv1.PublicationTarget{
				Objects: []cnpgv1.PublicationTargetObject{{TablesInSchema: "public"}},
			},
			ReclaimPolicy: cnpgv1.PublicationReclaimDelete,
		},
	}
}
