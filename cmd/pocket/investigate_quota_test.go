package main

import "testing"

func TestInvestigationQuotaReserveEnvironment(t *testing.T) {
	for _, tc := range []struct {
		value   string
		want    int
		wantErr bool
	}{
		{"0", 0, false},
		{"10", 10, false},
		{"20", 20, false},
		{"101", 0, true},
		{"-1", 0, true},
		{"invalid", 0, true},
	} {
		t.Run(tc.value, func(t *testing.T) {
			t.Setenv("POCKETCLI_INVESTIGATE_QUOTA_RESERVE_PERCENT", tc.value)
			percent, err := investigationQuotaReservePercent()
			if tc.wantErr {
				if err == nil || percent != nil {
					t.Fatalf("valor inválido aceito: percent=%v err=%v", percent, err)
				}
				return
			}
			if err != nil || percent == nil || *percent != tc.want {
				t.Fatalf("reserva inválida: percent=%v err=%v", percent, err)
			}
		})
	}
}
