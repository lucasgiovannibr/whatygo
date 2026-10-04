package websocket_producer

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	logger_wrapper "github.com/lucasgiovannibr/whatygo/pkg/logger"
	"github.com/gomessguii/logger"
	"github.com/gorilla/websocket"
)

// writeTimeout bounds a single frame write so one stalled subscriber cannot
// hold the event pipeline (and its per-connection write lock) forever.
const writeTimeout = 10 * time.Second

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		logger.LogInfo("Verificando origem da conexão WebSocket")
		return true
	},
}

// wsConn serializes writes to a gorilla connection. gorilla/websocket supports
// at most one concurrent writer per connection; without this lock, two events
// produced at the same time (e.g. a Receipt during a HistorySync burst) made the
// library panic with "concurrent write to websocket connection" and took the
// whole process down (issue #99).
type wsConn struct {
	conn    *websocket.Conn
	writeMu sync.Mutex
}

func (c *wsConn) writeJSON(v interface{}) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(writeTimeout))
	return c.conn.WriteJSON(v)
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

	conn := &wsConn{conn: rawConn}

	if instanceId == "" {
		producer.AddBroadcastClient(conn)
	} else {
		producer.AddClient(instanceId, conn)
	}

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
				rawConn.Close()
				break
			}
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
		_ = c.conn.Close()
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

func (p *websocketProducer) Produce(queueName string, payload []byte, instanceID string, _ string) error {
	message := map[string]interface{}{
		"queue":   strings.ToLower(queueName),
		"payload": string(payload),
	}

	// Snapshot the subscribers and release the lock before writing: writes are
	// network I/O and must not be done while holding the registry lock.
	p.clientsMux.RLock()
	instanceConns := append([]*wsConn(nil), p.clients[instanceID]...)
	broadcastConns := append([]*wsConn(nil), p.broadcast...)
	p.clientsMux.RUnlock()

	var firstErr error

	// Envia para todos os clientes da instância
	for _, c := range instanceConns {
		if err := c.writeJSON(message); err != nil {
			p.loggerWrapper.GetLogger(instanceID).LogError("Erro ao enviar mensagem websocket para %s: %v", instanceID, err)
			if firstErr == nil {
				firstErr = err
			}
			// Fecha a conexão: a goroutine de leitura de ServeWs faz a limpeza do registro.
			c.conn.Close()
			continue
		}
		p.loggerWrapper.GetLogger(instanceID).LogInfo("Mensagem websocket enviada com sucesso para instância %s na fila %s", instanceID, queueName)
	}

	// Envia para todos os clientes broadcast
	for _, c := range broadcastConns {
		if err := c.writeJSON(message); err != nil {
			p.loggerWrapper.GetLogger(instanceID).LogError("Erro ao enviar mensagem broadcast websocket: %v", err)
			c.conn.Close()
			continue
		}
	}

	return firstErr
}

// CreateGlobalQueues não faz nada para websocket producer
func (p *websocketProducer) CreateGlobalQueues() error {
	return nil
}
