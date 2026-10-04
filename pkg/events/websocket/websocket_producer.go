package websocket_producer

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gomessguii/logger"
	"github.com/gorilla/websocket"
	logger_wrapper "github.com/lucasgiovannibr/whatygo/pkg/logger"
)

// Limits of a subscriber connection.
//
// The socket is for the server to talk to the client: a subscriber never has to send more
// than control frames. There used to be no limit on what it could send (one 300 MB frame took
// the process from 81 MB to 873 MB), no check that it was still there (a half-open connection
// lived until a write failed), and events were written to every subscriber one after the other
// from the goroutine that produced them, so a subscriber that stopped reading held those
// goroutines, and the payloads they carry, for the whole write timeout, event after event.
const (
	// writeTimeout bounds a single frame write so one stalled subscriber cannot
	// hold its writer forever.
	writeTimeout = 10 * time.Second

	// readLimit is the largest message a subscriber may send.
	readLimit = 4 << 10
	// sendQueueMessages and sendQueueBytes bound what waits for a subscriber that reads
	// slower than events arrive. When either is exceeded the subscriber is disconnected (it
	// can reconnect) instead of piling up events in memory.
	sendQueueMessages = 1024
	sendQueueBytes    = 32 << 20
)

// pongWait is how long a subscriber may stay silent before it is dropped: the server pings
// every pingPeriod and any answer (or message) renews the deadline. Variables so the tests
// do not wait for them.
var (
	pongWait   = 60 * time.Second
	pingPeriod = 25 * time.Second
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		logger.LogInfo("Verificando origem da conexão WebSocket")
		return true
	},
}

// wsConn is one subscriber. Events are queued to it and written by its own goroutine, the
// only one that writes data frames (gorilla/websocket supports a single concurrent writer;
// two events produced at the same time used to make the library panic with "concurrent
// write to websocket connection", issue #99).
type wsConn struct {
	conn *websocket.Conn

	// writeMu serializes the writer goroutine and the close frame sent on shutdown.
	writeMu sync.Mutex

	send   chan []byte
	queued atomic.Int64 // bytes waiting in send

	closeOnce sync.Once
	done      chan struct{}
}

func newWsConn(conn *websocket.Conn) *wsConn {
	return &wsConn{conn: conn, send: make(chan []byte, sendQueueMessages), done: make(chan struct{})}
}

// enqueue hands a frame to the writer without blocking. It reports false when the subscriber
// is gone or has too much waiting; the caller disconnects it.
func (c *wsConn) enqueue(frame []byte) bool {
	select {
	case <-c.done:
		return false
	default:
	}
	if c.queued.Add(int64(len(frame))) > sendQueueBytes {
		c.queued.Add(-int64(len(frame)))
		return false
	}
	select {
	case c.send <- frame:
		return true
	default:
		c.queued.Add(-int64(len(frame)))
		return false
	}
}

// close ends the connection and its writer (once). The reader of ServeWs notices and
// removes the subscriber from the registry.
func (c *wsConn) close() {
	c.closeOnce.Do(func() {
		close(c.done)
		_ = c.conn.Close()
	})
}

// writeLoop writes the queued frames and pings the subscriber; it ends when the connection is
// closed or a write fails.
func (c *wsConn) writeLoop(pingEvery time.Duration) {
	ping := time.NewTicker(pingEvery)
	defer ping.Stop()
	for {
		select {
		case <-c.done:
			return
		case frame := <-c.send:
			c.queued.Add(-int64(len(frame)))
			c.writeMu.Lock()
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeTimeout))
			err := c.conn.WriteMessage(websocket.TextMessage, frame)
			c.writeMu.Unlock()
			if err != nil {
				c.close()
				return
			}
		case <-ping.C:
			if err := c.conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(writeTimeout)); err != nil {
				c.close()
				return
			}
		}
	}
}

type websocketProducer struct {
	clients       map[string][]*wsConn // conexões específicas por instância (várias por instância)
	broadcast     []*wsConn            // conexões que recebem todos os eventos
	clientsMux    sync.RWMutex
	loggerWrapper *logger_wrapper.LoggerManager
}

func NewWebsocketProducer(loggerWrapper *logger_wrapper.LoggerManager) *websocketProducer {
	return &websocketProducer{
		clients:       make(map[string][]*wsConn),
		broadcast:     make([]*wsConn, 0),
		clientsMux:    sync.RWMutex{},
		loggerWrapper: loggerWrapper,
	}
}

// ServeWs lida com as requisições de upgrade para websocket
func ServeWs(w http.ResponseWriter, r *http.Request, instanceId string, producer *websocketProducer) {
	logger.LogInfo("Iniciando upgrade da conexão WebSocket")
	rawConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.LogError("Erro ao fazer upgrade da conexão websocket: %v", err)
		return
	}

	logger.LogInfo("Conexão WebSocket estabelecida com sucesso")

	// The subscriber has nothing to say but control frames, and must answer the pings.
	wait, pingEvery := pongWait, pingPeriod
	rawConn.SetReadLimit(readLimit)
	_ = rawConn.SetReadDeadline(time.Now().Add(wait))
	rawConn.SetPongHandler(func(string) error {
		return rawConn.SetReadDeadline(time.Now().Add(wait))
	})

	conn := newWsConn(rawConn)

	if instanceId == "" {
		producer.AddBroadcastClient(conn)
	} else {
		producer.AddClient(instanceId, conn)
	}

	go conn.writeLoop(pingEvery)

	// Goroutine para limpar conexão quando fechada
	go func() {
		for {
			_, _, err := rawConn.ReadMessage()
			if err != nil {
				if instanceId == "" {
					producer.RemoveBroadcastClient(conn)
				} else {
					producer.RemoveClient(instanceId, conn)
				}
				conn.close()
				break
			}
			_ = rawConn.SetReadDeadline(time.Now().Add(wait))
		}
	}()
}

// Close tells every subscriber the server is going away and closes the connections: the
// HTTP server's Shutdown does not touch hijacked (upgraded) connections, so they used to
// keep the process waiting or be cut without a word.
func (p *websocketProducer) Close(ctx context.Context) error {
	p.clientsMux.Lock()
	var all []*wsConn
	all = append(all, p.broadcast...)
	for _, list := range p.clients {
		all = append(all, list...)
	}
	p.broadcast = nil
	p.clients = make(map[string][]*wsConn)
	p.clientsMux.Unlock()

	msg := websocket.FormatCloseMessage(websocket.CloseGoingAway, "server shutting down")
	for _, c := range all {
		c.writeMu.Lock()
		_ = c.conn.WriteControl(websocket.CloseMessage, msg, time.Now().Add(time.Second))
		c.writeMu.Unlock()
		c.close()
	}
	return nil
}

func (p *websocketProducer) AddBroadcastClient(conn *wsConn) {
	p.clientsMux.Lock()
	defer p.clientsMux.Unlock()
	p.broadcast = append(p.broadcast, conn)
	logger.LogInfo("Cliente broadcast websocket adicionado")
}

func (p *websocketProducer) RemoveBroadcastClient(conn *wsConn) {
	p.clientsMux.Lock()
	defer p.clientsMux.Unlock()
	p.broadcast = removeConn(p.broadcast, conn)
	logger.LogInfo("Cliente broadcast websocket removido")
}

func (p *websocketProducer) AddClient(instanceID string, conn *wsConn) {
	p.clientsMux.Lock()
	defer p.clientsMux.Unlock()
	p.clients[instanceID] = append(p.clients[instanceID], conn)
	p.loggerWrapper.GetLogger(instanceID).LogInfo("Cliente websocket adicionado para instância: %s", instanceID)
}

// RemoveClient removes one specific connection. Previously the whole instance
// entry was deleted, so when one of several subscribers disconnected the others
// silently stopped receiving events.
func (p *websocketProducer) RemoveClient(instanceID string, conn *wsConn) {
	p.clientsMux.Lock()
	defer p.clientsMux.Unlock()
	remaining := removeConn(p.clients[instanceID], conn)
	if len(remaining) == 0 {
		delete(p.clients, instanceID)
	} else {
		p.clients[instanceID] = remaining
	}
	p.loggerWrapper.GetLogger(instanceID).LogInfo("Cliente websocket removido para instância: %s", instanceID)
}

// removeConn returns list without conn, without mutating the input slice, so a
// concurrent Produce iterating over a snapshot is never affected.
func removeConn(list []*wsConn, conn *wsConn) []*wsConn {
	out := make([]*wsConn, 0, len(list))
	for _, c := range list {
		if c != conn {
			out = append(out, c)
		}
	}
	return out
}

// Produce queues the event for every subscriber of the instance and every broadcast
// subscriber. It never waits for a socket: a subscriber that cannot keep up is disconnected.
func (p *websocketProducer) Produce(queueName string, payload []byte, instanceID string, _ string) error {
	// Snapshot the subscribers and release the lock: nothing below holds the registry.
	p.clientsMux.RLock()
	instanceConns := append([]*wsConn(nil), p.clients[instanceID]...)
	broadcastConns := append([]*wsConn(nil), p.broadcast...)
	p.clientsMux.RUnlock()

	if len(instanceConns) == 0 && len(broadcastConns) == 0 {
		return nil
	}

	// One encoding for every subscriber (the payload, media included, used to be encoded once
	// per subscriber).
	frame, err := json.Marshal(map[string]interface{}{
		"queue":   strings.ToLower(queueName),
		"payload": string(payload),
	})
	if err != nil {
		return err
	}

	for _, c := range instanceConns {
		if c.enqueue(frame) {
			p.loggerWrapper.GetLogger(instanceID).LogInfo("Mensagem websocket enfileirada para instância %s na fila %s", instanceID, queueName)
			continue
		}
		p.loggerWrapper.GetLogger(instanceID).LogError("Assinante websocket de %s não acompanha os eventos (fila cheia ou conexão encerrada): desconectando", instanceID)
		// Fecha a conexão: a goroutine de leitura de ServeWs faz a limpeza do registro.
		c.close()
	}

	for _, c := range broadcastConns {
		if !c.enqueue(frame) {
			p.loggerWrapper.GetLogger(instanceID).LogError("Assinante broadcast websocket não acompanha os eventos (fila cheia ou conexão encerrada): desconectando")
			c.close()
		}
	}

	return nil
}

// CreateGlobalQueues não faz nada para websocket producer
func (p *websocketProducer) CreateGlobalQueues() error {
	return nil
}
