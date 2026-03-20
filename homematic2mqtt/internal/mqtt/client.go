package mqtt

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	paho "github.com/eclipse/paho.mqtt.golang"
)

type Config struct {
	Host     string
	Port     int
	ClientID string
}

type Client struct {
	config Config
	logger *slog.Logger
	client paho.Client
}

func NewClient(cfg Config, logger *slog.Logger) (*Client, error) {
	opts := paho.NewClientOptions()
	opts.AddBroker(fmt.Sprintf("tcp://%s:%d", cfg.Host, cfg.Port))
	opts.SetClientID(cfg.ClientID)
	opts.SetAutoReconnect(true)
	opts.SetCleanSession(false)
	opts.SetResumeSubs(true)

	return &Client{
		config: cfg,
		logger: logger,
		client: paho.NewClient(opts),
	}, nil
}

func (m *Client) Connect(ctx context.Context) error {
	m.logger.Info("Connecting to MQTT broker...", "host", m.config.Host)
	token := m.client.Connect()
	if token.Wait() && token.Error() != nil {
		return token.Error()
	}
	m.logger.Info("Connected to MQTT broker")
	return nil
}

func (m *Client) IsConnected() bool {
	return m.client != nil && m.client.IsConnected()
}

func (m *Client) Disconnect() {
	m.client.Disconnect(250)
}

func (m *Client) PublishShutterDiscovery(id, name string) {
	// Create a safe ID without colons for MQTT topics and unique_ids
	safeID := strings.ReplaceAll(id, ":", "_")
	topic := fmt.Sprintf("homeassistant/cover/%s/%s/config", m.config.ClientID, safeID)

	payload := map[string]interface{}{
		"name":               name,
		"command_topic":      fmt.Sprintf("%s/cover/%s/set", m.config.ClientID, id),
		"position_topic":     fmt.Sprintf("%s/cover/%s/position", m.config.ClientID, id),
		"set_position_topic": fmt.Sprintf("%s/cover/%s/set_position", m.config.ClientID, id),
		"unique_id":          fmt.Sprintf("%s_%s", m.config.ClientID, safeID),
		"device_class":       "blind",
		"payload_open":       "OPEN",
		"payload_close":      "CLOSE",
		"payload_stop":       "STOP",
		"position_open":      100,
		"position_closed":    0,
		"device": map[string]interface{}{
			"identifiers":  []string{fmt.Sprintf("%s_%s", m.config.ClientID, safeID)},
			"name":         name,
			"manufacturer": "Homelab Custom",
			"model":        "CCU2 Shutter",
			"via_device":   "homematic_bridge_gateway",
		},
	}
	jsonData, _ := json.Marshal(payload)
	m.client.Publish(topic, 0, true, string(jsonData))

	// Also publish a basic bridge device config once to ensure the "via_device" parent exists
	bridgeTopic := "homeassistant/sensor/homematic-bridge/config"
	bridgePayload := map[string]interface{}{
		"name":      "Homematic Bridge Status",
		"unique_id": "homematic_bridge_status",
		"state_topic": "homematic-bridge/status",
		"device": map[string]interface{}{
			"identifiers": []string{"homematic_bridge_gateway"},
			"name":        "Homematic Bridge",
			"model":       "Go XML-RPC Bridge",
			"manufacturer": "Homelab Custom",
		},
	}
	bridgeData, _ := json.Marshal(bridgePayload)
	m.client.Publish(bridgeTopic, 0, true, string(bridgeData))
}

func (m *Client) PublishPosition(id string, position int) {
	topic := fmt.Sprintf("%s/cover/%s/position", m.config.ClientID, id)
	m.client.Publish(topic, 0, false, fmt.Sprintf("%d", position))
}

func (m *Client) SubscribeCommands(handler func(id, cmd string, payload []byte)) error {
	topic := fmt.Sprintf("%s/cover/+/+", m.config.ClientID)
	m.logger.Info("Subscribing to MQTT commands", "topic", topic)
	
	if !m.client.IsConnected() {
		return fmt.Errorf("MQTT client not connected")
	}

	token := m.client.Subscribe(topic, 0, func(c paho.Client, msg paho.Message) {
		parts := strings.Split(msg.Topic(), "/")
		if len(parts) >= 4 {
			id := parts[2]
			cmd := parts[3]
			handler(id, cmd, msg.Payload())
		}
	})
	token.Wait()
	return token.Error()
}
