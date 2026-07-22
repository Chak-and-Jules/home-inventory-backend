package handlers

import (
	"testing"
	"time"

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

		// Custom valid
		{"Every 1 Days", true},
		{"Every 1 Day", true},
		{"Every 40 Days", true},
		{"every 10 days", true},
		{"Every 3 Weeks", true},
		{"Every 1 Week", true},
		{"every 12 weeks", true},

		// Custom invalid
		{"Every 0 Days", false},
		{"Every -5 Days", false},
		{"Every 0 Weeks", false},
		{"Every Days", false},
		{"Every abc Days", false},
		{"Every 2.5 Days", false},
		{"Every 3 Years", false},
		{"Every 6 Months Extra", false},
		{"RandomString", false},
	}

	for _, tt := range tests {
		t.Run(tt.freq, func(t *testing.T) {
			assert.Equal(t, tt.expected, isValidFrequency(tt.freq))
		})
	}
}

func TestParseFrequencyAndAdvance(t *testing.T) {
	baseTime := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name         string
		freq         string
		expectedTime time.Time
		expectedRep  bool
	}{
		{"Once", "Once", baseTime, false},
		{"Empty", "", baseTime, false},
		{"Daily", "Daily", baseTime.AddDate(0, 0, 1), true},
		{"Weekly", "Weekly", baseTime.AddDate(0, 0, 7), true},
		{"Monthly", "Monthly", baseTime.AddDate(0, 1, 0), true},
		{"Every 3 Months", "Every 3 Months", baseTime.AddDate(0, 3, 0), true},
		{"Every 6 Months", "Every 6 Months", baseTime.AddDate(0, 6, 0), true},
		{"Yearly", "Yearly", baseTime.AddDate(1, 0, 0), true},
		{"Every 40 Days", "Every 40 Days", baseTime.AddDate(0, 0, 40), true},
		{"Every 1 Day", "Every 1 Day", baseTime.AddDate(0, 0, 1), true},
		{"Every 3 Weeks", "Every 3 Weeks", baseTime.AddDate(0, 0, 21), true},
		{"Every 1 Week", "Every 1 Week", baseTime.AddDate(0, 0, 7), true},
		{"Invalid", "InvalidFrequency", baseTime, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTime, gotRep := parseFrequencyAndAdvance(baseTime, tt.freq)
			assert.Equal(t, tt.expectedRep, gotRep)
			assert.True(t, gotTime.Equal(tt.expectedTime), "expected %v, got %v", tt.expectedTime, gotTime)
		})
	}
}
