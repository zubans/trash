package service

import (
	"fmt"
	"strconv"
)

// Настройки споров, штрафных баллов и фото-подтверждения. Заводятся миграцией
// 053, правятся на экране «Технические параметры». План механики —
// doc/implementation_plan_disputes_penalties_photo_proof.md.
const (
	// SettingPenaltyPointsThreshold — N: столько активных баллов роли включают
	// период фото-подтверждения, вдвое больше — тихую блокировку.
	SettingPenaltyPointsThreshold = "penalty_points_threshold"
	// SettingPhotoRequirementMonths — длительность периода фото и шаг, на который
	// его продлевает новый балл.
	SettingPhotoRequirementMonths = "photo_requirement_months"
	// SettingPenaltyPointsTTLMonths — сколько месяцев без нового балла живут
	// баллы роли.
	SettingPenaltyPointsTTLMonths = "penalty_points_ttl_months"
	// SettingSilentBlockMonths — длительность тихой блокировки. Сгорание баллов
	// её не сокращает.
	SettingSilentBlockMonths = "silent_block_months"
	// SettingPhotoProofMaxTimeDiffMin и SettingPhotoProofMaxDistanceM — пороги,
	// выше которых арбитраж подсвечивает расхождение снимка с отметкой «Исполнил»
	// по времени и с адресом заказа по расстоянию.
	SettingPhotoProofMaxTimeDiffMin = "photo_proof_max_time_diff_min"
	SettingPhotoProofMaxDistanceM   = "photo_proof_max_distance_m"
)

// Умолчания — те же значения, что заводит миграция. Нужны, когда строки
// настройки нет или она нечитаема: механика не должна выключаться оттого, что
// кто-то стёр ключ.
const (
	defaultPenaltyPointsThreshold   = 2
	defaultPhotoRequirementMonths   = 3
	defaultPenaltyPointsTTLMonths   = 3
	defaultSilentBlockMonths        = 6
	defaultPhotoProofMaxTimeDiffMin = 30
	defaultPhotoProofMaxDistanceM   = 300
)

// penaltySettingBounds — допустимые целые значения каждой настройки. Нижняя
// граница везде 1: нулевой порог штрафовал бы за отсутствие баллов, нулевые
// сроки включали бы период или блокировку, которые кончаются в момент начала.
// Верхние границы отсекают опечатки, которые превратили бы трёхмесячный период
// в пожизненный.
var penaltySettingBounds = map[string][2]int{
	SettingPenaltyPointsThreshold:   {1, 100},
	SettingPhotoRequirementMonths:   {1, 60},
	SettingPenaltyPointsTTLMonths:   {1, 60},
	SettingSilentBlockMonths:        {1, 60},
	SettingPhotoProofMaxTimeDiffMin: {1, 24 * 60},
	SettingPhotoProofMaxDistanceM:   {1, 100000},
}

// validatePenaltySetting проверяет одну настройку, если это настройка штрафов.
// Прочие ключи пропускает: их проверяет UpdateSettings.
func validatePenaltySetting(key, value string) error {
	bounds, ok := penaltySettingBounds[key]
	if !ok {
		return nil
	}
	v, err := strconv.Atoi(value)
	if err != nil {
		return fmt.Errorf("setting %s must be an integer", key)
	}
	if v < bounds[0] || v > bounds[1] {
		return fmt.Errorf("setting %s must be between %d and %d", key, bounds[0], bounds[1])
	}
	return nil
}
