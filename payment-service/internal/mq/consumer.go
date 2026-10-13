package mq

import (
	"context"
	"encoding/json"
	"log"

	amqp "github.com/rabbitmq/amqp091-go"
)

type Handler interface {
	ProcessMessage(msg PaymentMessage) error
}

type Consumer struct {
	channel *amqp.Channel
	queue   string
	handler Handler
}

func NewConsumer(conn *Connection, handler Handler) *Consumer {
	return &Consumer{
		channel: conn.Channel(),
		queue:   conn.Topology().Queue,
		handler: handler,
	}
}

func (c *Consumer) Start(ctx context.Context) error {
	if err := c.channel.Qos(1, 0, false); err != nil {
		return err
	}

	deliveries, err := c.channel.Consume(
		c.queue,
		"",
		false,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		return err
	}

	go c.loop(ctx, deliveries)
	log.Printf("payment consumer started on queue %q", c.queue)
	return nil
}

func (c *Consumer) loop(ctx context.Context, deliveries <-chan amqp.Delivery) {
	for {
		select {
		case <-ctx.Done():
			log.Println("payment consumer stopped")
			return
		case d, ok := <-deliveries:
			if !ok {
				log.Println("payment consumer: delivery channel closed")
				return
			}
			c.handle(d)
		}
	}
}

func (c *Consumer) handle(d amqp.Delivery) {
	var msg PaymentMessage
	if err := json.Unmarshal(d.Body, &msg); err != nil {
		log.Printf("payment consumer: decode failed, dead-lettering: %v", err)
		_ = d.Nack(false, false)
		return
	}

	if err := c.handler.ProcessMessage(msg); err != nil {
		log.Printf("payment consumer: processing %s failed, dead-lettering: %v", msg.PaymentID, err)
		_ = d.Nack(false, false)
		return
	}

	if err := d.Ack(false); err != nil {
		log.Printf("payment consumer: ack %s failed: %v", msg.PaymentID, err)
	}
}
