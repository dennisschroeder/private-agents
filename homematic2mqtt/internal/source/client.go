package source

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html/charset"
)

type Config struct {
	CCUAddress   string
	CCUPort      int
	CallbackIP   string
	CallbackPort int
}

type Client struct {
	config      Config
	logger      *slog.Logger
	server      *http.Server
	events      chan Event
	isStarted   bool
	mu          sync.Mutex
	deviceNames map[string]string
	client      *http.Client
}

type Event struct {
	DeviceID string
	Key      string
	Value    interface{}
}

func NewClient(cfg Config, logger *slog.Logger) (*Client, error) {
	return &Client{
		config:      cfg,
		logger:      logger,
		events:      make(chan Event, 1000),
		deviceNames: make(map[string]string),
		client:      &http.Client{},
	}, nil
}

func (c *Client) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.isStarted {
		return nil
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", c.handleCallback)

	c.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", c.config.CallbackPort),
		Handler: mux,
	}

	go func() {
		c.logger.Info("Starting Homematic callback server", "port", c.config.CallbackPort)
		if err := c.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			c.logger.Error("Callback server failed", "error", err)
		}
	}()

	// 2. Register at CCU (init)
	time.Sleep(1 * time.Second)
	if err := c.sendInit(ctx, true); err != nil {
		return fmt.Errorf("failed to register at CCU: %w", err)
	}

	c.isStarted = true
	return nil
}

func (c *Client) Disconnect() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.isStarted {
		return
	}

	ctx := context.Background()
	c.sendInit(ctx, false)

	if c.server != nil {
		c.server.Shutdown(ctx)
	}
	c.isStarted = false
}

func (c *Client) Events() <-chan Event {
	return c.events
}

type MethodCall struct {
	XMLName    xml.Name `xml:"methodCall"`
	MethodName string   `xml:"methodName"`
	Params     []Param  `xml:"params>param"`
}

type Param struct {
	Value Value `xml:"value"`
}

type Value struct {
	Raw     string   `xml:",chardata"`
	String  string   `xml:"string"`
	Int     int      `xml:"int"`
	I4      int      `xml:"i4"`
	Double  float64  `xml:"double"`
	Boolean int      `xml:"boolean"`
	Array   []Value  `xml:"array>data>value"`
	Struct  []Member `xml:"struct>member"`
}

type Member struct {
	Name  string `xml:"name"`
	Value Value  `xml:"value"`
}

func (c *Client) handleCallback(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return
	}

	c.logger.Debug("Incoming XML-RPC", "len", len(body))

	decoder := xml.NewDecoder(bytes.NewReader(body))
	decoder.CharsetReader = charset.NewReaderLabel

	var call MethodCall
	if err := decoder.Decode(&call); err != nil {
		c.logger.Error("XML-RPC decode error", "error", err)
	} else {
		c.processMethodCall(call)
	}

	fmt.Fprintf(w, "<?xml version=\"1.0\"?><methodResponse><params><param><value><string></string></value></param></params></methodResponse>")
}

func (c *Client) processMethodCall(call MethodCall) {
	switch call.MethodName {
	case "event":
		c.parseEvent(call.Params)
	case "system.multicall":
		c.parseMulticall(call.Params)
	case "newDevices":
		c.parseNewDevices(call.Params)
	default:
		c.logger.Debug("Received other method call", "method", call.MethodName)
	}
}

func (c *Client) parseNewDevices(params []Param) {
	if len(params) < 2 {
		return
	}
	for _, dev := range params[1].Value.Array {
		var address string
		for _, m := range dev.Struct {
			if m.Name == "ADDRESS" {
				address = m.Value.String
			}
		}
		if address != "" {
			c.logger.Info("Discovered device address", "address", address)
		}
	}
}

func (c *Client) FetchDeviceNames(ctx context.Context) error {
	ccuURL := fmt.Sprintf("http://%s:8181/tclrega.exe", c.config.CCUAddress)
	// Improved script for CCU2: Iterate channel names and get addresses, linking to device name
	script := `string s; foreach(s, dom.GetObject(ID_CHANNELS).EnumNames()){ var o = dom.GetObject(s); if (o) { var d = dom.GetObject(o.Device()); WriteLine(o.Address() # "	" # d.Name()); } }`

	c.logger.Info("Fetching device names from CCU2 ReGaHSS with device-link fallback...")
	req, err := http.NewRequestWithContext(ctx, "POST", ccuURL, strings.NewReader(script))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	c.logger.Info("ReGaHSS raw response received", "len", len(bodyBytes))

	responseStr := string(bodyBytes)
	if idx := strings.Index(responseStr, "<xml>"); idx != -1 {
		responseStr = responseStr[:idx]
	}
	// Normalize line endings and split
	responseStr = strings.ReplaceAll(responseStr, "\r\n", "\n")
	lines := strings.Split(responseStr, "\n")

	c.mu.Lock()
	defer c.mu.Unlock()
	mappingCount := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		parts := strings.Split(line, "\t")
		if len(parts) == 2 {
			addr := strings.TrimSpace(parts[0])
			name := strings.TrimSpace(parts[1])
			c.deviceNames[addr] = name
			c.logger.Debug("Mapped ReGa device", "address", addr, "name", name)
			mappingCount++
		}
	}
	c.logger.Info("Mapped devices from ReGaHSS", "count", mappingCount)
	return nil
}

func (c *Client) GetDeviceName(id string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if name, ok := c.deviceNames[id]; ok {
		return name
	}
	parts := strings.Split(id, ":")
	if len(parts) > 1 {
		if name, ok := c.deviceNames[parts[0]]; ok {
			return fmt.Sprintf("%s:%s", name, parts[1])
		}
	}
	return id
}

func (v Value) ToString() string {
	if v.String != "" {
		return v.String
	}
	return strings.TrimSpace(v.Raw)
}

func (c *Client) ListDevices(ctx context.Context) ([]string, error) {
	ccuURL := fmt.Sprintf("http://%s:%d", c.config.CCUAddress, c.config.CCUPort)
	payload := `<?xml version="1.0"?><methodCall><methodName>listDevices</methodName><params><param><value><string>fritz_hm_bridge</string></value></param></params></methodCall>`
	req, _ := http.NewRequestWithContext(ctx, "POST", ccuURL, strings.NewReader(payload))
	req.Header.Set("Content-Type", "text/xml")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	decoder := xml.NewDecoder(resp.Body)
	decoder.CharsetReader = charset.NewReaderLabel
	var res struct {
		Params []struct {
			Value struct {
				Array []Value `xml:"array>data>value"`
			} `xml:"value"`
		} `xml:"params>param"`
	}
	if err := decoder.Decode(&res); err != nil {
		return nil, err
	}

	var addrs []string
	if len(res.Params) > 0 {
		c.logger.Info("Decoding listDevices response", "item_count", len(res.Params[0].Value.Array))
		for i, v := range res.Params[0].Value.Array {
			var addr string
			var typeStr string
			var parent string
			for _, m := range v.Struct {
				switch m.Name {
				case "ADDRESS":
					addr = m.Value.ToString()
				case "TYPE":
					typeStr = m.Value.ToString()
				case "PARENT":
					parent = m.Value.ToString()
				}
			}

			// LOG EVERY ENTRY TO FIND THE SHUTTERS
			c.logger.Info("Checking device entry", "index", i, "address", addr, "type", typeStr, "parent", parent)

			// CCU2: BLIND channels have a PARENT (the device) and TYPE 'BLIND'.
			if typeStr == "BLIND" && parent != "" && strings.Contains(addr, ":") {
				addrs = append(addrs, addr)
				c.logger.Info("Found BLIND channel", "address", addr, "parent", parent)
			}
		}
	}
	return addrs, nil
}

func (c *Client) GetValue(ctx context.Context, address, key string) (interface{}, error) {
	ccuURL := fmt.Sprintf("http://%s:%d", c.config.CCUAddress, c.config.CCUPort)
	payload := fmt.Sprintf(`<?xml version="1.0"?><methodCall><methodName>getValue</methodName><params><param><value><string>%s</string></value></param><param><value><string>%s</string></value></param></params></methodCall>`, address, key)
	req, _ := http.NewRequestWithContext(ctx, "POST", ccuURL, strings.NewReader(payload))
	req.Header.Set("Content-Type", "text/xml")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	decoder := xml.NewDecoder(resp.Body)
	decoder.CharsetReader = charset.NewReaderLabel
	var res struct {
		Params []struct {
			Value Value `xml:"value"`
		} `xml:"params>param"`
	}
	if err := decoder.Decode(&res); err != nil {
		return nil, err
	}
	if len(res.Params) == 0 {
		return nil, fmt.Errorf("no value")
	}

	v := res.Params[0].Value
	if v.Double != 0 {
		return v.Double, nil
	}
	if v.String != "" {
		return v.String, nil
	}
	if v.Boolean != 0 {
		return v.Boolean == 1, nil
	}
	return v.Int, nil
}

func (c *Client) SetValue(ctx context.Context, address, key string, value interface{}) error {
	ccuURL := fmt.Sprintf("http://%s:%d", c.config.CCUAddress, c.config.CCUPort)

	var valXML string
	switch v := value.(type) {
	case float64:
		valXML = fmt.Sprintf("<value><double>%f</double></value>", v)
	case int:
		valXML = fmt.Sprintf("<value><int>%d</int></value>", v)
	case bool:
		val := "0"
		if v {
			val = "1"
		}
		valXML = fmt.Sprintf("<value><boolean>%s</boolean></value>", val)
	case string:
		valXML = fmt.Sprintf("<value><string>%s</string></value>", v)
	}

	payload := fmt.Sprintf(`<?xml version="1.0"?>
<methodCall>
  <methodName>setValue</methodName>
  <params>
    <param><value><string>%s</string></value></param>
    <param><value><string>%s</string></value></param>
    <param>%s</param>
  </params>
</methodCall>`, address, key, valXML)

	req, err := http.NewRequestWithContext(ctx, "POST", ccuURL, strings.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "text/xml")

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("CCU returned status %d", resp.StatusCode)
	}

	return nil
}

func (c *Client) parseMulticall(params []Param) {
	if len(params) == 0 {
		return
	}
	for _, v := range params[0].Value.Array {
		var methodName string
		var eventParams []Param
		for _, member := range v.Struct {
			if member.Name == "methodName" {
				methodName = member.Value.String
			} else if member.Name == "params" {
				for _, pVal := range member.Value.Array {
					eventParams = append(eventParams, Param{Value: pVal})
				}
			}
		}
		if methodName == "event" {
			c.parseEvent(eventParams)
		}
	}
}

func (c *Client) parseEvent(params []Param) {
	if len(params) < 4 {
		return
	}
	deviceID := params[1].Value.String
	key := params[2].Value.String
	v := params[3].Value
	var val interface{}
	if v.Double != 0 {
		val = v.Double
	} else if v.String != "" {
		val = v.String
	} else if v.Boolean != 0 {
		val = v.Boolean == 1
	} else {
		val = v.Int
	}
	c.events <- Event{DeviceID: deviceID, Key: key, Value: val}
}

func (c *Client) sendInit(ctx context.Context, register bool) error {
	ccuURL := fmt.Sprintf("http://%s:%d", c.config.CCUAddress, c.config.CCUPort)
	callbackURL := ""
	if register {
		callbackURL = fmt.Sprintf("http://%s:%d", c.config.CallbackIP, c.config.CallbackPort)
	}
	c.logger.Info("Sending init to CCU", "ccu", ccuURL, "callback", callbackURL)
	payload := fmt.Sprintf(`<?xml version="1.0"?><methodCall><methodName>init</methodName><params><param><value><string>%s</string></value></param><param><value><string>fritz_hm_bridge</string></value></param></params></methodCall>`, callbackURL)
	req, err := http.NewRequestWithContext(ctx, "POST", ccuURL, bytes.NewBufferString(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "text/xml")
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}
