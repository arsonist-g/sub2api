package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClinePlatformMigration(t *testing.T) {
	content, err := FS.ReadFile("235_cline_platform.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "user_platform_quotas_platform_check")
	require.Contains(t, sql, "CHECK (platform IN ('anthropic', 'openai', 'gemini', 'antigravity', 'grok', 'kimi', 'zhipu', 'deepseek', 'opencode', 'cline'))")
	require.Contains(t, sql, "channel_monitors_provider_check")
	require.Contains(t, sql, "channel_monitor_request_templates_provider_check")
	require.Contains(t, sql, "composite_model_routes_target_platform_check")
	require.Contains(t, sql, "position('cline' IN monitor_constraint_def) = 0")
	require.Contains(t, sql, "position('cline' IN template_constraint_def) = 0")
}
