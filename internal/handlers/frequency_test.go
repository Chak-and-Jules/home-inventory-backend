package handlers

import (
	"testing"
	"time"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestIsValidFrequency(t *testing.T) {
	tests := []struct {
		freq     string
		expected bool
	}{
		{"Once", true},
		{"once", true},
		{"ONCE", true},
		{"", true},
		{"   ", true},
		{"Daily", true},
		{"daily", true},
		{"Weekly", true},
		{"weekly", true},
		{"Monthly", true},
		{"monthly", true},
		{"Every 3 Months", true},
		{"every 3 months", true},
		{"Every 6 Months", true},
		{"every 6 months", true},
		{"Yearly", true},
		{"yearly", true},
		{"Custom", true},
		{"custom", true},

		// No longer valid since Custom is its own option now
		{"Every 1 Days", false},
		{"Every 40 Days", false},
	}

	for _, tt := range tests {
		t.Run(tt.freq, func(t *testing.T) {
			assert.Equal(t, tt.expected, isValidFrequency(tt.freq))
		})
	}
}

func TestValidateMaintenanceTaskRequest(t *testing.T) {
	floatPtr := func(f float64) *float64 { return &f }
	strPtr := func(s string) *string { return &s }

	tests := []struct {
		name     string
		req      MaintenanceTaskRequest
		expected string
	}{
		{
			"valid standard Once",
			MaintenanceTaskRequest{Frequency: "Once"},
			"",
		},
		{
			"valid standard custom day",
			MaintenanceTaskRequest{
				Frequency:             "Custom",
				CustomFrequency:       floatPtr(15),
				CustomFrequencyMetric: strPtr("day"),
			},
			"",
		},
		{
			"valid standard custom Week capitalized",
			MaintenanceTaskRequest{
				Frequency:             "custom",
				CustomFrequency:       floatPtr(3),
				CustomFrequencyMetric: strPtr("Week"),
			},
			"",
		},
		{
			"missing custom frequency",
			MaintenanceTaskRequest{
				Frequency:             "Custom",
				CustomFrequencyMetric: strPtr("day"),
			},
			"Custom frequency must be a positive number",
		},
		{
			"negative custom frequency",
			MaintenanceTaskRequest{
				Frequency:             "Custom",
				CustomFrequency:       floatPtr(-5),
				CustomFrequencyMetric: strPtr("day"),
			},
			"Custom frequency must be a positive number",
		},
		{
			"missing custom metric",
			MaintenanceTaskRequest{
				Frequency:       "Custom",
				CustomFrequency: floatPtr(5),
			},
			"Custom frequency metric is required",
		},
		{
			"empty custom metric",
			MaintenanceTaskRequest{
				Frequency:             "Custom",
				CustomFrequency:       floatPtr(5),
				CustomFrequencyMetric: strPtr(""),
			},
			"Custom frequency metric is required",
		},
		{
			"invalid custom metric",
			MaintenanceTaskRequest{
				Frequency:             "Custom",
				CustomFrequency:       floatPtr(5),
				CustomFrequencyMetric: strPtr("hour"),
			},
			"Custom frequency metric must be day, week, month, or year",
		},
		{
			"non-custom frequency with custom_frequency provided",
			MaintenanceTaskRequest{
				Frequency:       "Monthly",
				CustomFrequency: floatPtr(5),
			},
			"Custom frequency and metric should not be provided for non-custom frequencies",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := validateMaintenanceTaskRequest(tt.req)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestParseFrequencyAndAdvance(t *testing.T) {
	baseTime := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)
	floatPtr := func(f float64) *float64 { return &f }
	strPtr := func(s string) *string { return &s }

	tests := []struct {
		name          string
		task          models.MaintenanceTask
		expectedTime  time.Time
		expectedRep   bool
	}{
		{"Once", models.MaintenanceTask{Frequency: "Once"}, baseTime, false},
		{"Empty", models.MaintenanceTask{Frequency: ""}, baseTime, false},
		{"Daily", models.MaintenanceTask{Frequency: "Daily"}, baseTime.AddDate(0, 0, 1), true},
		{"Weekly", models.MaintenanceTask{Frequency: "Weekly"}, baseTime.AddDate(0, 0, 7), true},
		{"Monthly", models.MaintenanceTask{Frequency: "Monthly"}, baseTime.AddDate(0, 1, 0), true},
		{"Every 3 Months", models.MaintenanceTask{Frequency: "Every 3 Months"}, baseTime.AddDate(0, 3, 0), true},
		{"Every 6 Months", models.MaintenanceTask{Frequency: "Every 6 Months"}, baseTime.AddDate(0, 6, 0), true},
		{"Yearly", models.MaintenanceTask{Frequency: "Yearly"}, baseTime.AddDate(1, 0, 0), true},
		{
			"Custom 40 days",
			models.MaintenanceTask{
				Frequency:             "Custom",
				CustomFrequency:       floatPtr(40),
				CustomFrequencyMetric: strPtr("day"),
			},
			baseTime.AddDate(0, 0, 40),
			true,
		},
		{
			"Custom 3 weeks",
			models.MaintenanceTask{
				Frequency:             "custom",
				CustomFrequency:       floatPtr(3),
				CustomFrequencyMetric: strPtr("week"),
			},
			baseTime.AddDate(0, 0, 21),
			true,
		},
		{
			"Custom 2 months",
			models.MaintenanceTask{
				Frequency:             "custom",
				CustomFrequency:       floatPtr(2),
				CustomFrequencyMetric: strPtr("month"),
			},
			baseTime.AddDate(0, 2, 0),
			true,
		},
		{
			"Custom 5 years",
			models.MaintenanceTask{
				Frequency:             "custom",
				CustomFrequency:       floatPtr(5),
				CustomFrequencyMetric: strPtr("year"),
			},
			baseTime.AddDate(5, 0, 0),
			true,
		},
		{"Invalid", models.MaintenanceTask{Frequency: "InvalidFrequency"}, baseTime, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTime, gotRep := parseFrequencyAndAdvance(baseTime, tt.task)
			assert.Equal(t, tt.expectedRep, gotRep)
			assert.True(t, gotTime.Equal(tt.expectedTime), "expected %v, got %v", tt.expectedTime, gotTime)
		})
	}
}
