package mq

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

type Publisher struct {
	channel    *amqp.Channel
	exchange   string
	routingKey string
}

func NewPublisher(conn *Connection) *Publisher {
	t := conn.Topology()
	return &Publisher{
		channel:    conn.Channel(),
		exchange:   t.Exchange,
		routingKey: t.RoutingKey,
	}
}

func (p *Publisher) Publish(ctx context.Context, msg PaymentMessage) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal payment message: %w", err)
	}

	publishing := amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		MessageId:    msg.PaymentID,
		Timestamp:    time.Now(),
		Body:         body,
	}

	if err := p.channel.PublishWithContext(
		ctx,
		p.exchange,
		p.routingKey,
		false,
		false,
		publishing,
	); err != nil {
		return fmt.Errorf("publish payment message: %w", err)
	}
	return nil
}
