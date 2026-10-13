package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"payment-service/internal/config"
	"payment-service/internal/coreclient"
	"payment-service/internal/database"
	"payment-service/internal/handler"
	"payment-service/internal/mq"
	"payment-service/internal/repository"
	"payment-service/internal/router"
	"payment-service/internal/security"
	"payment-service/internal/service"
	"payment-service/internal/worker"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	db, err := database.Connect(cfg.Database)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	if err := database.Migrate(db); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	log.Println("payment-service: database connected and migrated")

	verifier := security.NewJWTVerifier(cfg.JWTSecret)

	coreClient := coreclient.New(cfg.CoreService.BaseURL, cfg.CoreService.InternalKey, cfg.CoreService.Timeout)

	mqConn, err := mq.Connect(cfg.RabbitMQ)
	if err != nil {
		log.Fatalf("rabbitmq: %v", err)
	}
	defer mqConn.Close()
	log.Println("payment-service: connected to rabbitmq")

	publisher := mq.NewPublisher(mqConn)

	paymentRepo := repository.NewPaymentRepository(db)
	paymentService := service.NewPaymentService(paymentRepo, coreClient, publisher)

	handlers := router.Handlers{
		Payment: handler.NewPaymentHandler(paymentService),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	workerMgr := worker.NewManager(paymentService)
	workerMgr.Start(ctx)

	consumer := mq.NewConsumer(mqConn, paymentService)
	if err := consumer.Start(ctx); err != nil {
		log.Fatalf("rabbitmq consumer: %v", err)
	}

	engine := router.New(handlers, verifier)
	srv := &http.Server{
		Addr:    ":" + cfg.Server.Port,
		Handler: engine,
	}

	go func() {
		log.Printf("payment-service listening on :%s", cfg.Server.Port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("payment-service: shutting down...")

	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("forced shutdown: %v", err)
	}
	log.Println("payment-service: stopped")
}
