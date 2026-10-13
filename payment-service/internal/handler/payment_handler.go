package handler

import (
	"errors"
	"net/http"

	"payment-service/internal/middleware"
	"payment-service/internal/model"
	"payment-service/internal/service"

	"github.com/gin-gonic/gin"
)

type PaymentHandler struct {
	payments *service.PaymentService
}

func NewPaymentHandler(payments *service.PaymentService) *PaymentHandler {
	return &PaymentHandler{payments: payments}
}

func (h *PaymentHandler) Pay(c *gin.Context) {
	userID, ok := middleware.UserIDFromContext(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req model.PayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	session, err := h.payments.Pay(c.Request.Context(), userID, req.MerchantID, req.Amount, req.Description)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidAmount):
			respondError(c, http.StatusBadRequest, err.Error())
		default:
			respondError(c, http.StatusInternalServerError, "payment failed")
		}
		return
	}

	c.JSON(http.StatusAccepted, model.PayResponse{
		TransactionID: session.ID.String(),
		Status:        string(session.Status),
		Message:       "Payment accepted and queued for processing",
	})
}
