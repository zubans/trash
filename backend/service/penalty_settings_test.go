package service

import (
	"context"
	"testing"
)

// Настройки штрафов — целые числа в границах. Ноль, дробь, мусор и значение за
// верхней границей отвергаются целиком: UpdateSettings не сохраняет ничего из
// запроса, где хоть одна настройка неверна.
func TestAdminService_UpdateSettings_PenaltyBounds(t *testing.T) {
	newSvc := func() (*AdminService, *mockSettingsRepo) {
		settingsRepo := &mockSettingsRepo{settings: make(map[string]string)}
		svc := NewAdminService(newMockRepo(), &mockAdminRepo{}, settingsRepo, "secret", nil)
		return svc, settingsRepo
	}

	valid := map[string]string{
		SettingPenaltyPointsThreshold:   "2",
		SettingPhotoRequirementMonths:   "3",
		SettingPenaltyPointsTTLMonths:   "3",
		SettingSilentBlockMonths:        "6",
		SettingPhotoProofMaxTimeDiffMin: "30",
		SettingPhotoProofMaxDistanceM:   "300",
	}
	svc, repo := newSvc()
	if err := svc.UpdateSettings(context.Background(), valid); err != nil {
		t.Fatalf("valid penalty settings rejected: %v", err)
	}
	if repo.settings[SettingSilentBlockMonths] != "6" {
		t.Fatalf("settings not saved: %+v", repo.settings)
	}

	invalid := []struct {
		key, value string
	}{
		{SettingPenaltyPointsThreshold, "0"},
		{SettingPenaltyPointsThreshold, "1.5"},
		{SettingPenaltyPointsThreshold, "два"},
		{SettingPhotoRequirementMonths, "-3"},
		{SettingPenaltyPointsTTLMonths, "61"},
		{SettingSilentBlockMonths, ""},
		{SettingPhotoProofMaxTimeDiffMin, "1441"},
		{SettingPhotoProofMaxDistanceM, "0"},
	}
	for _, tc := range invalid {
		svc, repo := newSvc()
		err := svc.UpdateSettings(context.Background(), map[string]string{tc.key: tc.value, "currency": "RUB"})
		if err == nil {
			t.Errorf("%s=%q accepted", tc.key, tc.value)
		}
		if len(repo.settings) != 0 {
			t.Errorf("%s=%q: request partly saved: %+v", tc.key, tc.value, repo.settings)
		}
	}
}

// Разделы споров и штрафов есть в каталоге прав: без них право, выданное
// миграцией модератору, нельзя было бы ни показать, ни пересохранить.
func TestPermissionCatalog_DisputesAndPenalties(t *testing.T) {
	for _, code := range []string{"disputes.view", "disputes.edit", "penalties.edit"} {
		if !IsKnownPermission(code) {
			t.Errorf("permission %s is missing from the catalog", code)
		}
	}
}
