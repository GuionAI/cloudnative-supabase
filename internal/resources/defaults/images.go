// Package defaults contains default configuration values for Supabase components
package defaults

const (
	// PostgreSQL image defaults (CNPG)
	PostgresImage = "ghcr.io/cloudnative-pg/postgresql"
	PostgresTag   = "17"

	// Studio image defaults
	StudioImage = "supabase/studio"
	StudioTag   = "2025.12.17-sha-43f4f7f"

	// Auth (GoTrue) image defaults
	AuthImage = "supabase/gotrue"
	AuthTag   = "v2.184.0"

	// Rest (PostgREST) image defaults
	RestImage = "postgrest/postgrest"
	RestTag   = "v14.1"

	// Meta (postgres-meta) image defaults
	MetaImage = "supabase/postgres-meta"
	MetaTag   = "v0.95.1"

	// Kong image defaults
	KongImage = "kong"
	KongTag   = "2.8.1"

	// Sequin image defaults
	SequinImage = "sequin/sequin"
	SequinTag   = "v0.13.25"

	// Powersync image defaults
	PowersyncImage = "journeyapps/powersync-service"
	PowersyncTag   = "1.18.2"

	// Meilisearch image defaults
	MeilisearchImage = "getmeili/meilisearch"
	MeilisearchTag   = "v1.11.0"

	// Note: CDC permissions Job uses the same PostgresImage for psql compatibility
)
