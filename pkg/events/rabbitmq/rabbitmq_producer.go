package rabbitmq_producer

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	producer_interfaces "github.com/lucasgiovannibr/whatygo/pkg/events/interfaces"
	logger_wrapper "github.com/lucasgiovannibr/whatygo/pkg/logger"
	"github.com/gomessguii/logger"
	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	// confirmTimeout is how long a publish waits for the broker to confirm it.
	confirmTimeout = 15 * time.Second
	// dialBackoff is how long after a failed connection attempt the next event fails fast
	// instead of dialing again: the dial used to sleep inside the send path.
	dialBackoff = 3 * time.Second
	dialTimeout = 5 * time.Second
)

// queueArgs are the arguments every queue is declared with. They must not change: a queue
// that already exists with other arguments rejects the declaration (PRECONDITION_FAILED).
func queueArgs() amqp.Table {
	return amqp.Table{
		"x-queue-type": "quorum",
		"x-ha-policy":  "all",
	}
}

// rabbitMQProducer publishes through ONE long-lived channel in confirm mode, guarded by a
// mutex. It used to open a channel, declare the queue and enable confirms for every single
// event (three or four round trips), never read the confirmations (so "persistent +
// confirm" guaranteed nothing), and read/wrote the connection from many goroutines
// without a lock.
type rabbitMQProducer struct {
	amqpGlobalEnabled  bool
	amqpGlobalEvents   []string
	amqpSpecificEvents []string
	connStr            string
	loggerWrapper      *logger_wrapper.LoggerManager

	mu          sync.Mutex
	conn        *amqp.Connection
	channel     *amqp.Channel
	declared    map[string]struct{} // queues declared on the current channel
	lastDialErr time.Time
	closed      bool
}

func NewRabbitMQProducer(
	conn *amqp.Connection,
	amqpGlobalEnabled bool,
	amqpGlobalEvents []string,
	amqpSpecificEvents []string,
	connStr string,
	loggerWrapper *logger_wrapper.LoggerManager,
) producer_interfaces.Producer {
	return &rabbitMQProducer{
		conn:               conn,
		amqpGlobalEnabled:  amqpGlobalEnabled,
		amqpGlobalEvents:   amqpGlobalEvents,
		amqpSpecificEvents: amqpSpecificEvents,
		connStr:            connStr,
		loggerWrapper:      loggerWrapper,
		declared:           map[string]struct{}{},
	}
}

// maskConnectionString masks sensitive information in the connection string for logging
func (p *rabbitMQProducer) maskConnectionString(connStr string) string {
	if connStr == "" {
		return "empty"
	}

	parsedURL, err := url.Parse(connStr)
	if err != nil {
		return "invalid-url"
	}

	// Mask password if present
	if parsedURL.User != nil {
		if _, hasPassword := parsedURL.User.Password(); hasPassword {
			parsedURL.User = url.UserPassword(parsedURL.User.Username(), "***")
		}
	}

	return parsedURL.String()
}

// dropChannel forgets the channel (and what was declared on it). Caller holds p.mu.
func (p *rabbitMQProducer) dropChannel() {
	if p.channel != nil {
		_ = p.channel.Close()
	}
	p.channel = nil
	p.declared = map[string]struct{}{}
}

// ensureChannel returns a usable confirm-mode channel, reconnecting if needed. Caller
// holds p.mu.
func (p *rabbitMQProducer) ensureChannel() (*amqp.Channel, error) {
	if p.closed {
		return nil, errors.New("rabbitmq producer is closed")
	}
	if p.channel != nil && !p.channel.IsClosed() && p.conn != nil && !p.conn.IsClosed() {
		return p.channel, nil
	}
	p.dropChannel()

	if p.conn == nil || p.conn.IsClosed() {
		if p.connStr == "" {
			return nil, errors.New("RabbitMQ connection string is empty - check AMQP_URL configuration")
		}
		if !p.lastDialErr.IsZero() && time.Since(p.lastDialErr) < dialBackoff {
			return nil, errors.New("rabbitmq unavailable (recent connection failure)")
		}
		logger.LogInfo("Connecting to RabbitMQ: %s", p.maskConnectionString(p.connStr))
		conn, err := amqp.DialConfig(p.connStr, amqp.Config{
			Heartbeat: 30 * time.Second, // Send heartbeat every 30 seconds
			Locale:    "en_US",
			Dial:      amqp.DefaultDial(dialTimeout),
		})
		if err != nil {
			p.lastDialErr = time.Now()
			return nil, fmt.Errorf("failed to connect to RabbitMQ: %w", err)
		}
		p.lastDialErr = time.Time{}
		p.conn = conn
		logger.LogInfo("Connected to RabbitMQ with a 30s heartbeat")
	}

	ch, err := p.conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("failed to open channel: %w", err)
	}
	if err := ch.Confirm(false); err != nil {
		_ = ch.Close()
		return nil, fmt.Errorf("failed to enable publisher confirms: %w", err)
	}
	p.channel = ch
	return ch, nil
}

// declare declares a queue once per channel. Caller holds p.mu.
func (p *rabbitMQProducer) declare(ch *amqp.Channel, queueName string) error {
	if _, ok := p.declared[queueName]; ok {
		return nil
	}
	if _, err := ch.QueueDeclare(queueName, true, false, false, false, queueArgs()); err != nil {
		return err
	}
	p.declared[queueName] = struct{}{}
	return nil
}

// publishOnce declares (once) and publishes, then waits for the broker's confirmation.
// The mutex is held only while publishing: the wait for the confirmation (the broker's
// disk write, tens to hundreds of milliseconds on a quorum queue) happens without it, so
// concurrent events are pipelined instead of taking turns.
func (p *rabbitMQProducer) publishOnce(queueName string, payload []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), confirmTimeout)
	defer cancel()

	p.mu.Lock()
	ch, err := p.ensureChannel()
	if err != nil {
		p.mu.Unlock()
		return err
	}
	if err := p.declare(ch, queueName); err != nil {
		p.dropChannel() // a failed declaration closes the channel
		p.mu.Unlock()
		return fmt.Errorf("failed to declare queue %s: %w", queueName, err)
	}
	deferred, err := ch.PublishWithDeferredConfirmWithContext(ctx, "", queueName, false, false, amqp.Publishing{
		ContentType:  "application/json",
		Body:         payload,
		DeliveryMode: amqp.Persistent,
	})
	if err != nil {
		p.dropChannel()
		p.mu.Unlock()
		return fmt.Errorf("failed to publish: %w", err)
	}
	p.mu.Unlock()

	// A slow confirmation does not make the channel unusable (and dropping it would fail
	// every other publish waiting on it); a channel that really died nacks its pending
	// confirmations, and the next publish finds it closed and reconnects.
	acked, err := deferred.WaitContext(ctx)
	if err != nil {
		return fmt.Errorf("no confirmation from the broker: %w", err)
	}
	if !acked {
		return errors.New("the broker rejected the message or the channel closed (nack)")
	}
	return nil
}

func (p *rabbitMQProducer) Produce(
	queueName string,
	payload []byte,
	rabbitmqEnable string,
	userID string,
) error {
	if rabbitmqEnable != "global" && rabbitmqEnable != "enabled" {
		return nil
	}

	// One retry: the first failure is usually a channel/connection that died while idle.
	err := p.publishOnce(queueName, payload)
	if err != nil {
		p.loggerWrapper.GetLogger(userID).LogWarn("[%s] RabbitMQ publish to %s failed, retrying once: %v", userID, queueName, err)
		err = p.publishOnce(queueName, payload)
	}
	if err != nil {
		p.loggerWrapper.GetLogger(userID).LogError("[%s] RabbitMQ publish to %s failed: %v", userID, queueName, err)
		return err
	}
	return nil
}

// Close closes the channel and the connection.
func (p *rabbitMQProducer) Close(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	p.dropChannel()
	if p.conn != nil && !p.conn.IsClosed() {
		return p.conn.Close()
	}
	return nil
}

// CreateGlobalQueues cria todas as filas globais no startup da aplicação
func (p *rabbitMQProducer) CreateGlobalQueues() error {
	if !p.amqpGlobalEnabled {
		return nil
	}

	p.loggerWrapper.GetLogger("system").LogInfo("Creating global queues for enabled events")

	p.mu.Lock()
	defer p.mu.Unlock()

	channel, err := p.ensureChannel()
	if err != nil {
		return fmt.Errorf("failed to prepare the RabbitMQ channel: %v", err)
	}

	createdQueues := 0

	// AMQP_SPECIFIC_EVENTS tem prioridade sobre AMQP_GLOBAL_EVENTS
	if len(p.amqpSpecificEvents) > 0 {
		p.loggerWrapper.GetLogger("system").LogInfo("Using AMQP_SPECIFIC_EVENTS (priority over AMQP_GLOBAL_EVENTS)")

		// Cria filas diretas para eventos específicos
		for _, eventName := range p.amqpSpecificEvents {
			queueName := strings.ToLower(eventName)

			err = p.declare(channel, queueName)
			if err != nil {
				p.loggerWrapper.GetLogger("system").LogError("Failed to create specific queue %s: %v", queueName, err)
				return fmt.Errorf("failed to create specific queue %s: %v", queueName, err)
			}
			p.loggerWrapper.GetLogger("system").LogInfo("Specific queue created: %s", queueName)
			createdQueues++
		}
	} else {
		p.loggerWrapper.GetLogger("system").LogInfo("Using AMQP_GLOBAL_EVENTS (fallback mode)")

		// Mapeia eventos globais para os eventos originais que precisam de filas (modo antigo)
		eventMap := map[string][]string{
			"MESSAGE":       {"message"},
			"SEND_MESSAGE":  {"sendmessage"},
			"READ_RECEIPT":  {"receipt"},
			"PRESENCE":      {"presence"},
			"HISTORY_SYNC":  {"historysync"},
			"CHAT_PRESENCE": {"chatpresence", "archive"},
			"CALL":          {"calloffer", "callaccept", "callterminate", "calloffernotice", "callrelaylatency"},
			"CONNECTION":    {"connected", "pairsuccess", "temporaryban", "loggedout", "connectfailure", "disconnected"},
			"LABEL":         {"labeledit", "labelassociationchat", "labelassociationmessage"},
			"CONTACT":       {"contact", "pushname"},
			"GROUP":         {"groupinfo", "joinedgroup"},
			"NEWSLETTER":    {"newsletterjoin", "newsletterleave"},
			"QRCODE":        {"qrcode", "qrtimeout", "qrsuccess"},
		}

		for _, globalEvent := range p.amqpGlobalEvents {
			if queueNames, exists := eventMap[globalEvent]; exists {
				for _, queueName := range queueNames {
					err = p.declare(channel, queueName)
					if err != nil {
						p.loggerWrapper.GetLogger("system").LogError("Failed to create global queue %s: %v", queueName, err)
						return fmt.Errorf("failed to create global queue %s: %v", queueName, err)
					}
					p.loggerWrapper.GetLogger("system").LogInfo("Global queue created: %s", queueName)
					createdQueues++
				}
			}
		}
	}

	p.loggerWrapper.GetLogger("system").LogInfo("Successfully created %d global queues", createdQueues)
	return nil
}
