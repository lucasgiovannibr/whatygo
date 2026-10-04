package nats_producer

import (
	"context"
	"strings"
	"time"

	producer_interfaces "github.com/lucasgiovannibr/whatygo/pkg/events/interfaces"
	logger_wrapper "github.com/lucasgiovannibr/whatygo/pkg/logger"
	"github.com/gomessguii/logger"
	"github.com/nats-io/nats.go"
)

type natsProducer struct {
	conn              *nats.Conn
	natsGlobalEnabled bool
	natsGlobalEvents  []string
	loggerWrapper     *logger_wrapper.LoggerManager
}

// connectOptions makes the client keep trying: with a plain nats.Connect, a NATS server
// that is down when the process starts left the producer disabled until the next restart,
// and one that goes away later was only retried a few times.
func connectOptions() []nats.Option {
	return []nats.Option{
		nats.Name("whatygo"),
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2 * time.Second),
		nats.ReconnectJitter(500*time.Millisecond, 2*time.Second),
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			logger.LogWarn("NATS disconnected: %v (reconnecting)", err)
		}),
		nats.ReconnectHandler(func(c *nats.Conn) {
			logger.LogInfo("NATS reconnected to %s", c.ConnectedUrl())
		}),
		nats.ClosedHandler(func(_ *nats.Conn) {
			logger.LogInfo("NATS connection closed")
		}),
	}
}

func NewNatsProducer(
	url string,
	natsGlobalEnabled bool,
	natsGlobalEvents []string,
	loggerWrapper *logger_wrapper.LoggerManager,
) producer_interfaces.Producer {
	if strings.TrimSpace(url) == "" {
		return &natsProducer{
			conn:              nil,
			natsGlobalEnabled: false,
			natsGlobalEvents:  nil,
			loggerWrapper:     loggerWrapper,
		}
	}

	// With RetryOnFailedConnect an unreachable server is not an error here: the client
	// connects in the background and publishes are buffered meanwhile.
	conn, err := nats.Connect(url, connectOptions()...)
	if err != nil {
		logger.LogError("Failed to connect to NATS: %v", err)
		return &natsProducer{
			conn:              nil,
			natsGlobalEnabled: false,
			natsGlobalEvents:  nil,
			loggerWrapper:     loggerWrapper,
		}
	}

	return &natsProducer{
		conn:              conn,
		natsGlobalEnabled: natsGlobalEnabled,
		natsGlobalEvents:  natsGlobalEvents,
		loggerWrapper:     loggerWrapper,
	}
}

func (p *natsProducer) Produce(
	queueName string,
	payload []byte,
	natsEnable string,
	userID string,
) error {
	log := p.loggerWrapper.GetLogger(userID)

	if p.conn == nil {
		log.LogWarn("[%s] NATS connection is nil", userID)
		return nil
	}

	// "global" and "enabled" both publish the subject they are given.
	if natsEnable != "global" && natsEnable != "enabled" {
		return nil
	}
	if err := p.conn.Publish(queueName, payload); err != nil {
		log.LogError("[%s] Failed to publish message to subject %s: %v", userID, queueName, err)
		return err
	}
	return nil
}

// CreateGlobalQueues não faz nada para NATS producer pois os subjects são criados dinamicamente
func (p *natsProducer) CreateGlobalQueues() error {
	return nil
}

// Close flushes what was published and closes the connection, within ctx.
func (p *natsProducer) Close(ctx context.Context) error {
	if p.conn == nil {
		return nil
	}
	done := make(chan error, 1)
	go func() { done <- p.conn.Drain() }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		p.conn.Close()
		return ctx.Err()
	}
}
