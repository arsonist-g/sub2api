-- Persist the upstream provider a successful response declared having served the
-- request, for platforms that echo one (currently cline, at
-- choices[].{delta,message}.provider_metadata.gateway.routing.finalProvider).
-- NULL means the platform does not declare a provider, or the response never
-- carried the declaration.
--
-- Nullable with no default: on PostgreSQL 11+ this is a metadata-only change and
-- does not rewrite the (potentially large, partitioned) usage_logs table.
ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS upstream_provider VARCHAR(100);
