// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

//go:build integration

package observability

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// alertsFilePath is the relative path from test/integration/observability to the alerts file
const alertsFilePath = "../../../deployments/prometheus/alerts.yml"

// PrometheusAlertRules represents the structure of Prometheus alert rules
type PrometheusAlertRules struct {
	Groups []AlertGroup `yaml:"groups"`
}

// AlertGroup represents a group of related alerts
type AlertGroup struct {
	Name     string  `yaml:"name"`
	Interval string  `yaml:"interval"`
	Rules    []Alert `yaml:"rules"`
}

// Alert represents a single alert rule
type Alert struct {
	Alert       string            `yaml:"alert"`
	Expr        string            `yaml:"expr"`
	For         string            `yaml:"for"`
	Labels      map[string]string `yaml:"labels"`
	Annotations map[string]string `yaml:"annotations"`
}

// TestPrometheusAlerts validates Prometheus alert configurations
func TestPrometheusAlerts(t *testing.T) {
	t.Run("AlertRulesFileExists", func(t *testing.T) {
		_, err := os.Stat(alertsFilePath)
		require.NoError(t, err, "Alert rules file should exist at %s", alertsFilePath)

		t.Log("Alert rules file exists")
	})

	t.Run("AlertRulesSyntaxValid", func(t *testing.T) {
		data, err := os.ReadFile(alertsFilePath)
		require.NoError(t, err, "Should be able to read alert rules")

		var rules PrometheusAlertRules
		err = yaml.Unmarshal(data, &rules)
		require.NoError(t, err, "Alert rules should be valid YAML")

		assert.NotEmpty(t, rules.Groups, "Should have at least one alert group")

		t.Logf("Loaded %d alert groups", len(rules.Groups))
	})

	t.Run("RequiredAlertGroupsExist", func(t *testing.T) {
		data, err := os.ReadFile(alertsFilePath)
		require.NoError(t, err)

		var rules PrometheusAlertRules
		err = yaml.Unmarshal(data, &rules)
		require.NoError(t, err)

		requiredGroups := []string{
			"quicbaas_availability",
			"quicbaas_performance",
			"quicbaas_resources",
			"quicbaas_storage",
			"quicbaas_raft",
			"quicbaas_backup",
			"quicbaas_operations",
		}

		groupNames := make(map[string]bool)
		for _, group := range rules.Groups {
			groupNames[group.Name] = true
		}

		for _, requiredGroup := range requiredGroups {
			assert.True(t, groupNames[requiredGroup],
				"Required alert group %s should exist", requiredGroup)
		}

		t.Log("All required alert groups exist")
	})

	t.Run("CriticalAlertsExist", func(t *testing.T) {
		data, err := os.ReadFile(alertsFilePath)
		require.NoError(t, err)

		var rules PrometheusAlertRules
		err = yaml.Unmarshal(data, &rules)
		require.NoError(t, err)

		criticalAlerts := []string{
			"QuicBaaSNodeDown",
			"RaftLeaderMissing",
			"ClusterQuorumLost",
			"HighDiskUsage",
			"UpgradeFailed",
		}

		foundAlerts := make(map[string]bool)
		for _, group := range rules.Groups {
			for _, alert := range group.Rules {
				foundAlerts[alert.Alert] = true
			}
		}

		for _, criticalAlert := range criticalAlerts {
			assert.True(t, foundAlerts[criticalAlert],
				"Critical alert %s should exist", criticalAlert)
		}

		t.Log("All critical alerts exist")
	})

	t.Run("AlertsHaveRequiredFields", func(t *testing.T) {
		data, err := os.ReadFile(alertsFilePath)
		require.NoError(t, err)

		var rules PrometheusAlertRules
		err = yaml.Unmarshal(data, &rules)
		require.NoError(t, err)

		for _, group := range rules.Groups {
			for _, alert := range group.Rules {
				// Every alert should have a name
				assert.NotEmpty(t, alert.Alert, "Alert should have a name")

				// Every alert should have an expression
				assert.NotEmpty(t, alert.Expr, "Alert %s should have an expression", alert.Alert)

				// Every alert should have a duration
				assert.NotEmpty(t, alert.For, "Alert %s should have a 'for' duration", alert.Alert)

				// Every alert should have severity label
				assert.Contains(t, alert.Labels, "severity",
					"Alert %s should have severity label", alert.Alert)

				// Every alert should have summary annotation
				assert.Contains(t, alert.Annotations, "summary",
					"Alert %s should have summary annotation", alert.Alert)

				// Every alert should have description annotation
				assert.Contains(t, alert.Annotations, "description",
					"Alert %s should have description annotation", alert.Alert)
			}
		}

		t.Log("All alerts have required fields")
	})

	t.Run("AlertSeverityLevelsValid", func(t *testing.T) {
		data, err := os.ReadFile(alertsFilePath)
		require.NoError(t, err)

		var rules PrometheusAlertRules
		err = yaml.Unmarshal(data, &rules)
		require.NoError(t, err)

		validSeverities := map[string]bool{
			"critical": true,
			"warning":  true,
			"info":     true,
		}

		for _, group := range rules.Groups {
			for _, alert := range group.Rules {
				severity := alert.Labels["severity"]
				assert.True(t, validSeverities[severity],
					"Alert %s has invalid severity: %s", alert.Alert, severity)
			}
		}

		t.Log("All alert severity levels are valid")
	})

	t.Run("AvailabilityAlerts", func(t *testing.T) {
		data, err := os.ReadFile(alertsFilePath)
		require.NoError(t, err)

		var rules PrometheusAlertRules
		err = yaml.Unmarshal(data, &rules)
		require.NoError(t, err)

		var availabilityGroup *AlertGroup
		for _, group := range rules.Groups {
			if group.Name == "quicbaas_availability" {
				availabilityGroup = &group
				break
			}
		}

		require.NotNil(t, availabilityGroup, "Availability alert group should exist")

		expectedAlerts := []string{"QuicBaaSNodeDown", "RaftLeaderMissing", "ClusterQuorumLost"}
		foundAlerts := make(map[string]bool)
		for _, alert := range availabilityGroup.Rules {
			foundAlerts[alert.Alert] = true

			// All availability alerts should be critical
			assert.Equal(t, "critical", alert.Labels["severity"],
				"Availability alert %s should have critical severity", alert.Alert)
		}

		for _, expected := range expectedAlerts {
			assert.True(t, foundAlerts[expected],
				"Expected availability alert %s not found", expected)
		}

		t.Log("Availability alerts validated")
	})

	t.Run("PerformanceAlerts", func(t *testing.T) {
		data, err := os.ReadFile(alertsFilePath)
		require.NoError(t, err)

		var rules PrometheusAlertRules
		err = yaml.Unmarshal(data, &rules)
		require.NoError(t, err)

		var perfGroup *AlertGroup
		for _, group := range rules.Groups {
			if group.Name == "quicbaas_performance" {
				perfGroup = &group
				break
			}
		}

		require.NotNil(t, perfGroup, "Performance alert group should exist")

		// Verify latency alerts exist
		foundLatencyAlerts := false
		for _, alert := range perfGroup.Rules {
			if strings.Contains(alert.Alert, "Latency") {
				foundLatencyAlerts = true
				// Latency alerts should check histogram quantiles
				assert.Contains(t, alert.Expr, "histogram_quantile",
					"Latency alert %s should use histogram_quantile", alert.Alert)
			}
		}

		assert.True(t, foundLatencyAlerts, "Should have latency alerts")

		t.Log("Performance alerts validated")
	})

	t.Run("ResourceAlerts", func(t *testing.T) {
		data, err := os.ReadFile(alertsFilePath)
		require.NoError(t, err)

		var rules PrometheusAlertRules
		err = yaml.Unmarshal(data, &rules)
		require.NoError(t, err)

		var resourceGroup *AlertGroup
		for _, group := range rules.Groups {
			if group.Name == "quicbaas_resources" {
				resourceGroup = &group
				break
			}
		}

		require.NotNil(t, resourceGroup, "Resource alert group should exist")

		expectedResourceAlerts := []string{"HighMemoryUsage", "HighCPUUsage", "HighDiskUsage"}
		foundAlerts := make(map[string]bool)
		for _, alert := range resourceGroup.Rules {
			foundAlerts[alert.Alert] = true
		}

		for _, expected := range expectedResourceAlerts {
			assert.True(t, foundAlerts[expected],
				"Expected resource alert %s not found", expected)
		}

		t.Log("Resource alerts validated")
	})

	t.Run("BackupOperationalAlerts", func(t *testing.T) {
		data, err := os.ReadFile(alertsFilePath)
		require.NoError(t, err)

		var rules PrometheusAlertRules
		err = yaml.Unmarshal(data, &rules)
		require.NoError(t, err)

		var backupGroup *AlertGroup
		for _, group := range rules.Groups {
			if group.Name == "quicbaas_backup" {
				backupGroup = &group
				break
			}
		}

		require.NotNil(t, backupGroup, "Backup alert group should exist")

		expectedBackupAlerts := []string{"BackupFailed", "BackupTooOld"}
		foundAlerts := make(map[string]bool)
		for _, alert := range backupGroup.Rules {
			foundAlerts[alert.Alert] = true
		}

		for _, expected := range expectedBackupAlerts {
			assert.True(t, foundAlerts[expected],
				"Expected backup alert %s not found", expected)
		}

		t.Log("Backup operational alerts validated")
	})
}
