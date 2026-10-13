package service

import "errors"

var (
	// ErrInvalidAmount indicates a non-positive amount (400).
	ErrInvalidAmount = errors.New("amount must be greater than zero")
	// ErrInsufficientFunds indicates the wallet balance is too low (400).
	ErrInsufficientFunds = errors.New("insufficient funds")
	// ErrWalletNotFound indicates the user's wallet does not exist (404).
	ErrWalletNotFound = errors.New("wallet not found")
	// ErrPaymentExpired indicates the session expired before processing.
	ErrPaymentExpired = errors.New("payment session expired")
	// ErrNonRetryable marks a message that cannot be processed and should be
	// dead-lettered rather than retried (e.g. a malformed payload).
	ErrNonRetryable = errors.New("non-retryable message")
)
