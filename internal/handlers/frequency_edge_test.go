package handlers

import (
	"testing"
	"time"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/stretchr/testify/require"
)

func TestCustomFrequencyInvalidSchedulesDoNotAdvance(t *testing.T) {
	base := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	positive, zero, negative, fractional := 2.0, 0.0, -1.0, 0.5
	day, invalid := "day", "hour"
	for _, tt := range []struct {
		name   string
		value  *float64
		metric *string
	}{
		{"missing value", nil, &day}, {"zero", &zero, &day},
		{"negative", &negative, &day}, {"missing metric", &positive, nil},
		{"less than one", &fractional, &day}, {"unknown metric", &positive, &invalid},
	} {
		t.Run(tt.name, func(t *testing.T) {
			next, repeats := parseFrequencyAndAdvance(base, models.MaintenanceTask{Frequency: "Custom", CustomFrequency: tt.value, CustomFrequencyMetric: tt.metric})
			require.False(t, repeats)
			require.Equal(t, base, next)
		})
	}
}

func TestCustomFrequencyNormalizesAndTruncates(t *testing.T) {
	base := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	value, metric := 2.9, " Week "
	next, repeats := parseFrequencyAndAdvance(base, models.MaintenanceTask{Frequency: " CUSTOM ", CustomFrequency: &value, CustomFrequencyMetric: &metric})
	require.True(t, repeats)
	require.Equal(t, base.AddDate(0, 0, 14), next)
}
