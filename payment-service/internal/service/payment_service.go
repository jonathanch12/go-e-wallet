package service

import (
	"context"
	"errors"
	"log"
	"time"

	"payment-service/internal/coreclient"
	"payment-service/internal/model"
	"payment-service/internal/mq"
	"payment-service/internal/repository"

	"github.com/google/uuid"
)

const PaymentExpiry = 10 * time.Minute
const processingDelay = 2 * time.Second

type Publisher interface {
	Publish(ctx context.Context, msg mq.PaymentMessage) error
}

type PaymentService struct {
	payments  *repository.PaymentRepository
	core      *coreclient.Client
	publisher Publisher
}

func NewPaymentService(payments *repository.PaymentRepository, core *coreclient.Client, publisher Publisher) *PaymentService {
	return &PaymentService{payments: payments, core: core, publisher: publisher}
}

func (s *PaymentService) Pay(ctx context.Context, userID uuid.UUID, merchantID string, amount float64, description string) (*model.PaymentSession, error) {
	if amount <= 0 {
		return nil, ErrInvalidAmount
	}

	now := time.Now()
	session := &model.PaymentSession{
		UserID:      userID,
		MerchantID:  merchantID,
		Amount:      amount,
		Description: description,
		Status:      model.StatusPending,
		ExpiresAt:   now.Add(PaymentExpiry),
	}
	if err := s.payments.Create(session); err != nil {
		return nil, err
	}

	msg := mq.PaymentMessage{
		PaymentID:   session.ID.String(),
		UserID:      userID.String(),
		Amount:      amount,
		MerchantID:  merchantID,
		Description: description,
		CreatedAt:   session.CreatedAt,
	}
	if err := s.publisher.Publish(ctx, msg); err != nil {
		session.Status = model.StatusFailed
		_ = s.payments.Update(session)
		return nil, err
	}

	return session, nil
}

func (s *PaymentService) ProcessMessage(msg mq.PaymentMessage) error {
	paymentID, err := uuid.Parse(msg.PaymentID)
	if err != nil {
		return errors.Join(ErrNonRetryable, err)
	}

	session, err := s.payments.FindByID(paymentID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			log.Printf("payment %s: session not found, discarding message", msg.PaymentID)
			return nil
		}
		return err
	}

	if session.Status != model.StatusPending {
		log.Printf("payment %s: already in status %q, skipping", session.ID, session.Status)
		return nil
	}

	if time.Now().After(session.ExpiresAt) {
		session.Status = model.StatusExpired
		if err := s.payments.Update(session); err != nil {
			return err
		}
		log.Printf("payment %s: expired (created %s), not debiting", session.ID, session.CreatedAt.Format(time.RFC3339))
		return nil
	}

	time.Sleep(processingDelay)

	resp, err := s.core.DebitWallet(model.WalletDebitRequest{
		UserID:        session.UserID.String(),
		Amount:        session.Amount,
		MerchantID:    session.MerchantID,
		Description:   session.Description,
		TransactionID: session.ID.String(),
	})
	if err != nil {
		switch {
		case errors.Is(err, coreclient.ErrInsufficientFunds),
			errors.Is(err, coreclient.ErrWalletNotFound),
			errors.Is(err, coreclient.ErrBadRequest):
			session.Status = model.StatusFailed
			if uerr := s.payments.Update(session); uerr != nil {
				return uerr
			}
			log.Printf("payment %s: failed (%v)", session.ID, err)
			return nil
		default:
			log.Printf("payment %s: transient error, dead-lettering: %v", session.ID, err)
			return err
		}
	}

	session.Status = model.StatusCompleted
	session.LedgerTransactionID = resp.TransactionID
	if err := s.payments.Update(session); err != nil {
		return err
	}
	log.Printf("payment %s: completed (ledger %s, new balance %.2f)", session.ID, resp.TransactionID, resp.NewBalance)
	return nil
}

func (s *PaymentService) ExpireStaleSessions() (int64, error) {
	return s.payments.MarkExpiredBatch(time.Now())
}
