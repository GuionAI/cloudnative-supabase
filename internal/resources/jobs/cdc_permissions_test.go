package jobs

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	supabasev1alpha1 "github.com/GuionAI/cloudnative-supabase/api/v1alpha1"
)

func TestBuildCDCSetupScriptGrantsPowerSyncAccess(t *testing.T) {
	project := &supabasev1alpha1.SupabaseProject{
		ObjectMeta: metav1.ObjectMeta{Name: "my-app", Namespace: "default"},
		Spec: supabasev1alpha1.SupabaseProjectSpec{
			Powersync: &supabasev1alpha1.PowersyncSpec{},
		},
	}

	script := BuildCDCMigrationsConfigMap(project).Data["setup.sh"]
	if !strings.Contains(script, "GRANT SELECT ON ALL TABLES IN SCHEMA public TO powersync_replication") {
		t.Error("CDC setup must grant PowerSync access to public tables")
	}
}
