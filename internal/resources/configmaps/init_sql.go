/*
Copyright 2026 GuionAI.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package configmaps

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	supabasev1alpha1 "github.com/GuionAI/cloudnative-supabase/api/v1alpha1"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/common"
)

// InitSQLTemplate is the database initialization SQL for Supabase
// This runs during CNPG bootstrap BEFORE managed.roles are created
const InitSQLTemplate = `-- ============================================
-- Supabase Database Initialization SQL
-- ============================================
-- This runs during bootstrap BEFORE managed.roles.
-- Creates roles, schemas, and sets up all grants.
-- ============================================

-- 1. Create api_access_role (group role for API permissions)
CREATE ROLE api_access_role NOLOGIN;

-- 2. Create supabase_auth_admin (needed before auth schema creation)
CREATE ROLE supabase_auth_admin NOINHERIT LOGIN CREATEROLE;

-- 3. Create Supabase service schemas
CREATE SCHEMA IF NOT EXISTS auth AUTHORIZATION supabase_auth_admin;
CREATE SCHEMA IF NOT EXISTS extensions AUTHORIZATION supabase_admin;

-- 4. JWT settings (required for RLS policies)
ALTER DATABASE %s SET "app.settings.jwt_secret" TO '$(JWT_SECRET)';
ALTER DATABASE %s SET "app.settings.jwt_exp" TO '$(JWT_EXP)';

-- 5. Grant API access via group role (anon/authenticated/service_role inherit later)
GRANT USAGE ON SCHEMA public TO api_access_role;
GRANT USAGE ON SCHEMA extensions TO api_access_role;

-- Default privileges for future objects in public schema
-- Must specify FOR ROLE since tables are created by supabase_admin, not postgres
ALTER DEFAULT PRIVILEGES FOR ROLE supabase_admin IN SCHEMA public GRANT ALL ON TABLES TO api_access_role;
ALTER DEFAULT PRIVILEGES FOR ROLE supabase_admin IN SCHEMA public GRANT ALL ON FUNCTIONS TO api_access_role;
ALTER DEFAULT PRIVILEGES FOR ROLE supabase_admin IN SCHEMA public GRANT ALL ON SEQUENCES TO api_access_role;

-- 6. Grant supabase_admin access to auth schema (extensions already owned by supabase_admin)
GRANT ALL ON SCHEMA auth TO supabase_admin;

-- 7. Default privileges for future objects in auth schema (created by supabase_auth_admin)
ALTER DEFAULT PRIVILEGES FOR ROLE supabase_auth_admin IN SCHEMA auth GRANT ALL ON TABLES TO supabase_admin;
ALTER DEFAULT PRIVILEGES FOR ROLE supabase_auth_admin IN SCHEMA auth GRANT ALL ON SEQUENCES TO supabase_admin;
ALTER DEFAULT PRIVILEGES FOR ROLE supabase_auth_admin IN SCHEMA auth GRANT ALL ON ROUTINES TO supabase_admin;

-- 8. PostgREST DDL watch functions (notify PostgREST to reload schema on DDL changes)
CREATE FUNCTION extensions.pgrst_ddl_watch() RETURNS event_trigger
    LANGUAGE plpgsql
    AS $$
DECLARE
  cmd record;
BEGIN
  FOR cmd IN SELECT * FROM pg_event_trigger_ddl_commands()
  LOOP
    IF cmd.command_tag IN (
      'CREATE SCHEMA', 'ALTER SCHEMA'
    , 'CREATE TABLE', 'CREATE TABLE AS', 'SELECT INTO', 'ALTER TABLE'
    , 'CREATE FOREIGN TABLE', 'ALTER FOREIGN TABLE'
    , 'CREATE VIEW', 'ALTER VIEW'
    , 'CREATE MATERIALIZED VIEW', 'ALTER MATERIALIZED VIEW'
    , 'CREATE FUNCTION', 'ALTER FUNCTION'
    , 'CREATE TRIGGER'
    , 'CREATE TYPE', 'ALTER TYPE'
    , 'CREATE RULE'
    , 'COMMENT'
    )
    AND cmd.schema_name is distinct from 'pg_temp'
    THEN
      NOTIFY pgrst, 'reload schema';
    END IF;
  END LOOP;
END; $$;

CREATE FUNCTION extensions.pgrst_drop_watch() RETURNS event_trigger
    LANGUAGE plpgsql
    AS $$
DECLARE
  obj record;
BEGIN
  FOR obj IN SELECT * FROM pg_event_trigger_dropped_objects()
  LOOP
    IF obj.object_type IN (
      'schema'
    , 'table'
    , 'foreign table'
    , 'view'
    , 'materialized view'
    , 'function'
    , 'trigger'
    , 'type'
    , 'rule'
    )
    AND obj.is_temporary IS false
    THEN
      NOTIFY pgrst, 'reload schema';
    END IF;
  END LOOP;
END; $$;

CREATE EVENT TRIGGER pgrst_ddl_watch
  ON ddl_command_end
  EXECUTE FUNCTION extensions.pgrst_ddl_watch();

CREATE EVENT TRIGGER pgrst_drop_watch
  ON sql_drop
  EXECUTE FUNCTION extensions.pgrst_drop_watch();
`

// InitSQLConfigMapName returns the name of the init SQL ConfigMap
func InitSQLConfigMapName(project *supabasev1alpha1.SupabaseProject) string {
	return project.Name + "-init-sql"
}

// BuildInitSQLConfigMap creates the ConfigMap containing init SQL for CNPG bootstrap
func BuildInitSQLConfigMap(project *supabasev1alpha1.SupabaseProject) *corev1.ConfigMap {
	dbName := common.DatabaseName

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      InitSQLConfigMapName(project),
			Namespace: project.Namespace,
			Labels:    common.ComponentLabels(project, "init-sql"),
		},
		Data: map[string]string{
			"init.sql": fmt.Sprintf(InitSQLTemplate, dbName, dbName),
		},
	}
}
