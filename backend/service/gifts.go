package service

import (
	"time"

	"healthlogin/backend/repository"
)

// MarkExpiredGifts показывает просроченный купон просроченным, даже если
// ночной проход ещё не успел его пометить: состояние на экране не должно
// зависеть от того, работал ли фоновый воркер. Правило живёт здесь, а не в
// чтении строки репозитория: это решение площадки о том, что считать
// действующим купоном, и строка в базе от него не меняется.
func MarkExpiredGifts(gifts []*repository.UserGift, now time.Time) {
	for _, ug := range gifts {
		if ug.Status == repository.GiftStatusIssued && ug.ExpiresAt != nil && ug.ExpiresAt.Before(now) {
			ug.Status = repository.GiftStatusExpired
		}
	}
}
