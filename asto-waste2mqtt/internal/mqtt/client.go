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

	m := &Client{
		config: cfg,
		logger: logger,
	}

	opts.SetOnConnectHandler(func(c paho.Client) {
		logger.Info("Connected to MQTT broker, signaling online status")
		topic := fmt.Sprintf("%s/status", cfg.ClientID)
		c.Publish(topic, 0, true, "online")
	})

	m.client = paho.NewClient(opts)
	return m, nil
}

func (m *Client) Connect(ctx context.Context) error {
	m.logger.Info("Connecting to MQTT broker...")
	token := m.client.Connect()
	if token.Wait() && token.Error() != nil {
		return token.Error()
	}
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

func (m *Client) PublishDiscovery(sensorID, name, stateClass, deviceClass, unit string) {
	topic := fmt.Sprintf("homeassistant/sensor/%s/%s/config", m.config.ClientID, sensorID)
	payload := map[string]interface{}{
		"name":               name,
		"state_topic":        fmt.Sprintf("%s/sensor/%s/state", m.config.ClientID, sensorID),
		"availability_topic": fmt.Sprintf("%s/status", m.config.ClientID),
		"payload_available":   "online",
		"payload_not_available": "offline",
		"unique_id":          fmt.Sprintf("%s_%s", m.config.ClientID, sensorID),
		"device": map[string]interface{}{
			"identifiers":  []string{m.config.ClientID},
			"name":         "ASTO Waste Bridge",
			"manufacturer": "Homelab Custom",
			"model":        "Go ASTO Bridge",
		},
	}

	if stateClass != "" {
		payload["state_class"] = stateClass
	}
	if deviceClass != "" {
		payload["device_class"] = deviceClass
	}
	if unit != "" {
		payload["unit_of_measurement"] = unit
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

func (m *Client) PublishState(sensorID string, value interface{}) {
	topic := fmt.Sprintf("%s/sensor/%s/state", m.config.ClientID, sensorID)
	m.client.Publish(topic, 0, false, fmt.Sprintf("%v", value))
}
