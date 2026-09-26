package handler

import (
	"net/http"

	"healthlogin/backend/money"
	"healthlogin/backend/service"

	"github.com/go-chi/chi/v5"
)

// WalletHandler обслуживает заявки пользователя на пополнение и вывод
// собственных денег: POST /customer/finances/topup и POST /finances/withdrawals.
type WalletHandler struct {
	wallet *service.WalletService
}

// NewWalletHandler создаёт WalletHandler.
func NewWalletHandler(wallet *service.WalletService) *WalletHandler {
	return &WalletHandler{wallet: wallet}
}

// amountRequest — тело обеих заявок.
type amountRequest struct {
	Amount money.Amount `json:"amount"`
}

// CreateTopUpRequestHandler создаёт заявку на пополнение баланса.
func (h *WalletHandler) CreateTopUpRequestHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req amountRequest
	if !decodeBody(w, r, &req) {
		return
	}
	created, err := h.wallet.CreateTopUpRequest(r.Context(), user.ID, req.Amount)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

// CreateWithdrawalRequestHandler создаёт заявку на вывод для аутентифицированного пользователя.
func (h *WalletHandler) CreateWithdrawalRequestHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req amountRequest
	if !decodeBody(w, r, &req) {
		return
	}
	created, err := h.wallet.CreateWithdrawalRequest(r.Context(), user.ID, req.Amount)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

// RegisterUserRoutes — заявки на пополнение и вывод. Исполнителям тоже
// нужны пополнения: штрафы могут увести баланс в минус.
func (h *WalletHandler) RegisterUserRoutes(r chi.Router) {
	r.Post("/customer/finances/topup", h.CreateTopUpRequestHandler)
	r.Post("/finances/withdrawals", h.CreateWithdrawalRequestHandler)
}
