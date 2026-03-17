package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/IBM/sarama"
	"github.com/gorilla/websocket"
)

const (
	wsEventTypePriceUpdate = "price_update"

	wsClientSendBuffer = 32
	wsBroadcastBuffer  = 128

	wsWriteWait      = 10 * time.Second
	wsPongWait       = 60 * time.Second
	wsPingPeriod     = (wsPongWait * 9) / 10
	wsMaxMessageSize = 512
)

var wsNewline = []byte{'\n'}

var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// Local MVP: dashboard runs on a separate localhost origin during development.
	CheckOrigin: func(_ *http.Request) bool {
		return true
	},
}

type kafkaPriceUpdate struct {
	ProductID string  `json:"product_id"`
	NewPrice  float64 `json:"new_price"`
	Timestamp int64   `json:"timestamp"`
}

type wsPriceUpdateEvent struct {
	Type      string  `json:"type"`
	ProductID string  `json:"product_id"`
	NewPrice  float64 `json:"new_price"`
	Timestamp int64   `json:"timestamp"`
}

type wsClient struct {
	hub  *priceUpdateHub
	conn *websocket.Conn
	send chan []byte
}

type priceUpdateHub struct {
	register   chan *wsClient
	unregister chan *wsClient
	broadcast  chan wsPriceUpdateEvent
	clients    map[*wsClient]struct{}
	stop       chan struct{}
	done       chan struct{}

	shutdownOnce sync.Once
}

func newPriceUpdateHub() *priceUpdateHub {
	return &priceUpdateHub{
		register:   make(chan *wsClient),
		unregister: make(chan *wsClient),
		broadcast:  make(chan wsPriceUpdateEvent, wsBroadcastBuffer),
		clients:    map[*wsClient]struct{}{},
		stop:       make(chan struct{}),
		done:       make(chan struct{}),
	}
}

func (h *priceUpdateHub) run() {
	defer close(h.done)

	for {
		select {
		case client := <-h.register:
			h.clients[client] = struct{}{}
		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
		case update := <-h.broadcast:
			payload, err := json.Marshal(update)
			if err != nil {
				log.Printf("warning: failed to marshal ws update: %v", err)
				continue
			}
			for client := range h.clients {
				select {
				case client.send <- payload:
				default:
					delete(h.clients, client)
					close(client.send)
				}
			}
		case <-h.stop:
			for client := range h.clients {
				delete(h.clients, client)
				close(client.send)
			}
			return
		}
	}
}

func (h *priceUpdateHub) shutdown() {
	h.shutdownOnce.Do(func() {
		close(h.stop)
		<-h.done
	})
}

func (h *priceUpdateHub) broadcastUpdate(update wsPriceUpdateEvent) {
	select {
	case h.broadcast <- update:
	default:
		log.Printf("warning: dropping ws update for %s due to broadcast backpressure", update.ProductID)
	}
}

func wsPricesHandler(hub *priceUpdateHub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := wsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("warning: websocket upgrade failed: %v", err)
			return
		}

		client := &wsClient{
			hub:  hub,
			conn: conn,
			send: make(chan []byte, wsClientSendBuffer),
		}
		client.hub.register <- client

		go client.writePump()
		go client.readPump()
	}
}

func (c *wsClient) readPump() {
	defer func() {
		c.hub.unregister <- c
		_ = c.conn.Close()
	}()

	c.conn.SetReadLimit(wsMaxMessageSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(wsPongWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(wsPongWait))
	})

	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("warning: websocket client read error: %v", err)
			}
			return
		}
	}
}

func (c *wsClient) writePump() {
	ticker := time.NewTicker(wsPingPeriod)
	defer func() {
		ticker.Stop()
		_ = c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			writer, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			if _, err := writer.Write(message); err != nil {
				_ = writer.Close()
				return
			}

			queuedCount := len(c.send)
			for i := 0; i < queuedCount; i++ {
				if _, err := writer.Write(wsNewline); err != nil {
					_ = writer.Close()
					return
				}
				if _, err := writer.Write(<-c.send); err != nil {
					_ = writer.Close()
					return
				}
			}

			if err := writer.Close(); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func startPriceUpdatesFanout(brokers []string, topic string, hub *priceUpdateHub) (sarama.Consumer, sarama.PartitionConsumer, error) {
	kafkaConfig := sarama.NewConfig()
	kafkaConfig.Consumer.Return.Errors = true
	kafkaConfig.Version = sarama.V2_5_0_0

	consumer, err := sarama.NewConsumer(brokers, kafkaConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("create kafka consumer: %w", err)
	}

	partitionConsumer, err := consumer.ConsumePartition(topic, 0, sarama.OffsetNewest)
	if err != nil {
		_ = consumer.Close()
		return nil, nil, fmt.Errorf("consume topic %q partition 0: %w", topic, err)
	}

	go fanoutPriceUpdates(partitionConsumer, hub)

	return consumer, partitionConsumer, nil
}

func fanoutPriceUpdates(partitionConsumer sarama.PartitionConsumer, hub *priceUpdateHub) {
	for {
		select {
		case message, ok := <-partitionConsumer.Messages():
			if !ok {
				return
			}

			update, err := parsePriceUpdate(message.Value)
			if err != nil {
				log.Printf("warning: invalid price-updates payload ignored: %v", err)
				continue
			}

			hub.broadcastUpdate(update)
		case consumeErr, ok := <-partitionConsumer.Errors():
			if !ok {
				return
			}
			log.Printf("warning: kafka price-updates consume error: %v", consumeErr)
		}
	}
}

func parsePriceUpdate(raw []byte) (wsPriceUpdateEvent, error) {
	var update kafkaPriceUpdate
	if err := json.Unmarshal(raw, &update); err != nil {
		return wsPriceUpdateEvent{}, fmt.Errorf("decode price update: %w", err)
	}

	update.ProductID = strings.TrimSpace(update.ProductID)
	if update.ProductID == "" {
		return wsPriceUpdateEvent{}, errors.New("missing product_id")
	}
	if update.NewPrice <= 0 {
		return wsPriceUpdateEvent{}, fmt.Errorf("invalid new_price: %.2f", update.NewPrice)
	}
	if update.Timestamp <= 0 {
		return wsPriceUpdateEvent{}, fmt.Errorf("invalid timestamp: %d", update.Timestamp)
	}

	return wsPriceUpdateEvent{
		Type:      wsEventTypePriceUpdate,
		ProductID: update.ProductID,
		NewPrice:  update.NewPrice,
		Timestamp: update.Timestamp,
	}, nil
}
