package queue

import (
	"log"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

type RabbitMQ struct {
	conn  *amqp.Connection
	ch    *amqp.Channel
	queue string
}

// NewRabbitMQ connects to RabbitMQ, retrying up to 20 times with a 5 second
// delay between attempts. RabbitMQ can take 45-60s to fully start inside
// Docker, so 20 × 5s = 100s gives it plenty of headroom.
func NewRabbitMQ(cfg *Config) (*RabbitMQ, error) {
	var conn *amqp.Connection
	var err error

	for i := 1; i <= 20; i++ {
		conn, err = amqp.Dial(cfg.URL)
		if err == nil {
			break
		}
		log.Printf("RabbitMQ not ready (attempt %d/20): %v — retrying in 5s...", i, err)
		time.Sleep(5 * time.Second)
	}
	if err != nil {
		return nil, err
	}

	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, err
	}

	// Dead-letter exchange
	err = ch.ExchangeDeclare(
		"jobs.dlx",
		"direct",
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		return nil, err
	}

	// Dead-letter queue
	dlq, err := ch.QueueDeclare(
		"jobs.dlq",
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		return nil, err
	}

	// Bind DLQ to DLX
	err = ch.QueueBind(
		dlq.Name,
		"jobs.dlq",
		"jobs.dlx",
		false,
		nil,
	)
	if err != nil {
		return nil, err
	}

	// Main queue with DLQ config
	args := amqp.Table{
		"x-dead-letter-exchange":    "jobs.dlx",
		"x-dead-letter-routing-key": "jobs.dlq",
	}

	_, err = ch.QueueDeclare(
		cfg.QueueName,
		true,
		false,
		false,
		false,
		args,
	)
	if err != nil {
		return nil, err
	}

	return &RabbitMQ{
		conn:  conn,
		ch:    ch,
		queue: cfg.QueueName,
	}, nil
}

func (r *RabbitMQ) Channel() *amqp.Channel {
	return r.ch
}

func (r *RabbitMQ) Publish(body []byte) error {
	return r.ch.Publish(
		"",
		r.queue,
		false,
		false,
		amqp.Publishing{
			ContentType: "application/json",
			Body:        body,
		},
	)
}

func (r *RabbitMQ) Close() error {
	if err := r.ch.Close(); err != nil {
		return err
	}
	return r.conn.Close()
}
