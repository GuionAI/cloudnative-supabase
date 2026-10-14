package jobs

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
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

	cm, err := BuildCDCMigrationsConfigMap(project)
	if err != nil {
		t.Fatal(err)
	}
	script := cm.Data["setup.sh"]
	if !strings.Contains(script, "GRANT SELECT ON ALL TABLES IN SCHEMA \"public\" TO powersync_replication") {
		t.Error("CDC setup must grant PowerSync access to public tables")
	}
}

func TestCDCSelectedSchemas(t *testing.T) {
	for _, schemas := range [][]string{nil, {"public", "app_data"}, {"app_data"}, {"select"}} {
		project := &supabasev1alpha1.SupabaseProject{Spec: supabasev1alpha1.SupabaseProjectSpec{Powersync: &supabasev1alpha1.PowersyncSpec{PublicationSchemas: schemas}}}
		cm, err := BuildCDCMigrationsConfigMap(project)
		if err != nil {
			t.Fatal(err)
		}
		script := cm.Data["setup.sh"]
		want := schemas
		if want == nil {
			want = []string{"public"}
		}
		if strings.Count(script, "GRANT USAGE ON SCHEMA") != len(want) {
			t.Fatal("unexpected schema grants")
		}
		for _, schema := range want {
			for _, grant := range []string{`GRANT USAGE ON SCHEMA "` + schema + `" TO powersync_replication;`, `GRANT SELECT ON ALL TABLES IN SCHEMA "` + schema + `" TO powersync_replication;`, `ALTER DEFAULT PRIVILEGES FOR ROLE supabase_admin IN SCHEMA "` + schema + `" GRANT SELECT ON TABLES TO powersync_replication;`} {
				if !strings.Contains(script, grant) {
					t.Fatalf("missing grant %s", grant)
				}
			}
		}
		if strings.Contains(script, "CREATE SCHEMA") || strings.Contains(script, "anon") || strings.Contains(script, "authenticated") {
			t.Fatal("CDC exceeds replication boundary")
		}
	}
	for _, schemas := range [][]string{{}, {""}, {"public", "public"}, {"a.b"}, {"Upper"}, {strings.Repeat("a", 64)}, {`x"; DROP SCHEMA public;--`}, {"$(touch /tmp/unsafe)"}} {
		cm, err := BuildCDCMigrationsConfigMap(&supabasev1alpha1.SupabaseProject{Spec: supabasev1alpha1.SupabaseProjectSpec{Powersync: &supabasev1alpha1.PowersyncSpec{PublicationSchemas: schemas}}})
		if err == nil || cm != nil {
			t.Fatalf("accepted invalid schemas %q", schemas)
		}
	}
}

// Execute the generated artifact with a test-owned psql fake. SQL errors must
// propagate through sh, and the fake records transaction/error-stop arguments.
func TestCDCScriptPropagatesSQLFailure(t *testing.T) {
	dir := t.TempDir()
	fake := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$TEST_ARGS\"\ncat > \"$TEST_SQL\"\nexit 3\n"
	if err := os.WriteFile(filepath.Join(dir, "psql"), []byte(fake), 0700); err != nil {
		t.Fatal(err)
	}
	project := &supabasev1alpha1.SupabaseProject{Spec: supabasev1alpha1.SupabaseProjectSpec{Powersync: &supabasev1alpha1.PowersyncSpec{PublicationSchemas: []string{"missing_schema"}}}}
	cm, err := BuildCDCMigrationsConfigMap(project)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh")
	cmd.Stdin = strings.NewReader(cm.Data["setup.sh"])
	cmd.Env = []string{"PATH=" + dir + ":/usr/bin:/bin", "PGCONNSTR=test-only", "TEST_ARGS=" + filepath.Join(dir, "args"), "TEST_SQL=" + filepath.Join(dir, "sql")}
	output, err := cmd.CombinedOutput()
	if err == nil || strings.Contains(string(output), "Setup Complete") {
		t.Fatalf("SQL failure ignored: %v %s", err, output)
	}
	args, err := os.ReadFile(filepath.Join(dir, "args"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(strings.Fields(string(args)), []string{"-v", "ON_ERROR_STOP=1", "--single-transaction", "test-only"}) {
		t.Fatalf("psql args = %s", args)
	}
	sql, err := os.ReadFile(filepath.Join(dir, "sql"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(sql), `GRANT USAGE ON SCHEMA "missing_schema"`) {
		t.Fatalf("SQL heredoc not delivered: %s", sql)
	}
}
