package mq

import "time"

type PaymentMessage struct {
	PaymentID   string    `json:"payment_id"`
	UserID      string    `json:"user_id"`
	Amount      float64   `json:"amount"`
	MerchantID  string    `json:"merchant_id"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}

func (m PaymentMessage) MarshalCreatedAt() string {
	return m.CreatedAt.Format(time.RFC3339)
}
