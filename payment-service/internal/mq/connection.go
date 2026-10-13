package mq

import (
	"fmt"

	"payment-service/internal/config"

	amqp "github.com/rabbitmq/amqp091-go"
)

type Topology struct {
	Exchange           string
	Queue              string
	RoutingKey         string
	DeadLetterExchange string
	DeadLetterQueue    string
}

type Connection struct {
	conn     *amqp.Connection
	channel  *amqp.Channel
	topology Topology
}

func Connect(cfg config.RabbitMQConfig) (*Connection, error) {
	conn, err := amqp.Dial(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("dial rabbitmq: %w", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("open channel: %w", err)
	}

	topology := Topology{
		Exchange:           cfg.Exchange,
		Queue:              cfg.Queue,
		RoutingKey:         cfg.RoutingKey,
		DeadLetterExchange: cfg.DeadLetterExchange,
		DeadLetterQueue:    cfg.DeadLetterQueue,
	}

	if err := declareTopology(ch, topology); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, err
	}

	return &Connection{conn: conn, channel: ch, topology: topology}, nil
}

func declareTopology(ch *amqp.Channel, t Topology) error {
	if err := ch.ExchangeDeclare(
		t.Exchange, "direct", true /*durable*/, false, false, false, nil,
	); err != nil {
		return fmt.Errorf("declare exchange %q: %w", t.Exchange, err)
	}

	if err := ch.ExchangeDeclare(
		t.DeadLetterExchange, "direct", true, false, false, false, nil,
	); err != nil {
		return fmt.Errorf("declare dlx %q: %w", t.DeadLetterExchange, err)
	}
	if _, err := ch.QueueDeclare(
		t.DeadLetterQueue, true, false, false, false, nil,
	); err != nil {
		return fmt.Errorf("declare dlq %q: %w", t.DeadLetterQueue, err)
	}
	if err := ch.QueueBind(
		t.DeadLetterQueue, t.RoutingKey, t.DeadLetterExchange, false, nil,
	); err != nil {
		return fmt.Errorf("bind dlq: %w", err)
	}

	args := amqp.Table{
		"x-dead-letter-exchange":    t.DeadLetterExchange,
		"x-dead-letter-routing-key": t.RoutingKey,
	}
	if _, err := ch.QueueDeclare(
		t.Queue, true, false, false, false, args,
	); err != nil {
		return fmt.Errorf("declare queue %q: %w", t.Queue, err)
	}
	if err := ch.QueueBind(
		t.Queue, t.RoutingKey, t.Exchange, false, nil,
	); err != nil {
		return fmt.Errorf("bind queue: %w", err)
	}

	return nil
}

func (c *Connection) Channel() *amqp.Channel { return c.channel }

func (c *Connection) Topology() Topology { return c.topology }

func (c *Connection) Close() error {
	var firstErr error
	if c.channel != nil {
		if err := c.channel.Close(); err != nil {
			firstErr = err
		}
	}
	if c.conn != nil {
		if err := c.conn.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
