package model

// ---- Payment DTOs (public API) ----

type PayRequest struct {
	MerchantID  string  `json:"merchantID" binding:"required"`
	Amount      float64 `json:"amount" binding:"required,gt=0"`
	Description string  `json:"description"`
}

type PayResponse struct {
	TransactionID string `json:"transactionID"`
	Status        string `json:"status"`
	Message       string `json:"message"`
}

// ---- Internal DTOs (calls to core-service) ----

type WalletDebitRequest struct {
	UserID        string  `json:"userID"`
	Amount        float64 `json:"amount"`
	MerchantID    string  `json:"merchantID"`
	Description   string  `json:"description"`
	TransactionID string  `json:"transactionID"`
}

type WalletDebitResponse struct {
	TransactionID string  `json:"transactionID"`
	NewBalance    float64 `json:"newBalance"`
}

// ---- Common error DTO ----

type ErrorResponse struct {
	Error string `json:"error"`
}
