package mqtt

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

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
	brokerUrl := fmt.Sprintf("tcp://%s:%d", cfg.Host, cfg.Port)
	opts.AddBroker(brokerUrl)
	opts.SetClientID(cfg.ClientID)
	opts.SetAutoReconnect(true)
	opts.SetCleanSession(false)
	opts.SetResumeSubs(true)

	// Last Will
	statusTopic := fmt.Sprintf("%s/status", cfg.ClientID)
	opts.SetWill(statusTopic, "offline", 0, true)

	opts.SetOnConnectHandler(func(c paho.Client) {
		logger.Info("Connected to MQTT broker, signaling online status")
		topic := fmt.Sprintf("%s/status", cfg.ClientID)
		c.Publish(topic, 0, true, "online")
	})

	return &Client{
		config: cfg,
		logger: logger,
		client: paho.NewClient(opts),
	}, nil
}

func (m *Client) Connect(ctx context.Context) error {
	m.logger.Info("Connecting to MQTT broker...")
	token := m.client.Connect()
	if token.Wait() && token.Error() != nil {
		return token.Error()
	}
	m.logger.Info("Connected to MQTT broker")
	return nil
}

func (m *Client) Disconnect() {
	m.PublishAvailability(false)
	m.client.Disconnect(250)
}

func (m *Client) PublishAvailability(online bool) {
	topic := fmt.Sprintf("%s/status", m.config.ClientID)
	status := "offline"
	if online {
		status = "online"
	}
	m.client.Publish(topic, 0, true, status)
}

func (m *Client) PublishPresenceDiscovery(id, name string) {
	topic := fmt.Sprintf("homeassistant/binary_sensor/%s/%s/config", m.config.ClientID, id)
	m.logger.Info("Publishing presence discovery", "topic", topic, "id", id, "name", name)
	payload := map[string]interface{}{
		"name":               name,
		"state_topic":        fmt.Sprintf("%s/binary_sensor/%s/state", m.config.ClientID, id),
		"unique_id":          fmt.Sprintf("%s_%s", m.config.ClientID, id),
		"device_class":       "presence",
		"availability_topic": fmt.Sprintf("%s/status", m.config.ClientID),
		"payload_available":   "online",
		"payload_not_available": "offline",
		"device": map[string]interface{}{
			"identifiers":  []string{m.config.ClientID},
			"name":         "FritzBox Presence Bridge",
			"manufacturer": "Homelab Custom",
			"model":        "Go Fritz presence Bridge",
		},
	}
	jsonData, _ := json.Marshal(payload)
	m.client.Publish(topic, 0, true, string(jsonData))

	// EXPLICIT BRIDGE STATUS SENSOR
	statusSensorTopic := fmt.Sprintf("homeassistant/sensor/%s/status/config", m.config.ClientID)
	statusPayload := map[string]interface{}{
		"name":               "Bridge Status",
		"state_topic":        fmt.Sprintf("%s/status", m.config.ClientID),
		"unique_id":          fmt.Sprintf("%s_bridge_status", m.config.ClientID),
		"device": map[string]interface{}{
			"identifiers": []string{m.config.ClientID},
		},
	}
	statusData, _ := json.Marshal(statusPayload)
	m.client.Publish(statusSensorTopic, 0, true, string(statusData))
}

func (m *Client) PublishState(id string, active bool) {
	topic := fmt.Sprintf("%s/binary_sensor/%s/state", m.config.ClientID, id)
	state := "OFF"
	if active {
		state = "ON"
	}
	m.client.Publish(topic, 0, false, state)
}
