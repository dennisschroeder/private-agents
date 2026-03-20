package source

import (
	"context"
	"crypto/tls"
	"encoding/xml"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"
)

type Config struct {
	Address  string
	Username string
	Password string
}

type Client struct {
	config Config
	logger *slog.Logger
	client *http.Client
}

func NewClient(cfg Config, logger *slog.Logger) (*Client, error) {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		MaxIdleConns:        10,
		MaxIdleConnsPerHost: 10,
	}
	return &Client{
		config: cfg,
		logger: logger,
		client: &http.Client{
			Transport: tr,
		},
	}, nil
}

func (c *Client) Connect(ctx context.Context) error {
	c.logger.Info("FritzBox Parallel SOAP source client initialized", "address", c.config.Address)
	return nil
}

func (c *Client) Disconnect() {}

// SOAP structures for TR-064
type envelope struct {
	XMLName xml.Name `xml:"http://schemas.xmlsoap.org/soap/envelope/ Envelope"`
	Body    body
}

type body struct {
	XMLName xml.Name `xml:"http://schemas.xmlsoap.org/soap/envelope/ Body"`
	Content interface{}
}

type getHostNumberOfEntries struct {
	XMLName xml.Name `xml:"urn:dslforum-org:service:Hosts:1 GetHostNumberOfEntries"`
}

type getGenericHostEntry struct {
	XMLName xml.Name `xml:"urn:dslforum-org:service:Hosts:1 GetGenericHostEntry"`
	Index   int      `xml:"NewIndex"`
}

type genericHostEntryResponse struct {
	MAC    string `xml:"NewMACAddress"`
	Active bool   `xml:"NewActive"`
}

func (c *Client) FetchActiveMACs(ctx context.Context) (map[string]bool, error) {
	url := fmt.Sprintf("http://%s:49000/upnp/control/hosts", c.config.Address)

	// 1. Get host count
	count, err := c.getHostCount(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("failed to get host count: %w", err)
	}

	activeMACs := make(map[string]bool)
	var mu sync.Mutex
	
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(10) // Process 10 requests at a time

	for i := 0; i < count; i++ {
		index := i
		g.Go(func() error {
			entry, err := c.getHostEntry(ctx, url, index)
			if err != nil {
				return nil // Skip failed entries
			}
			if entry.Active {
				mu.Lock()
				activeMACs[strings.ToLower(entry.MAC)] = true
				mu.Unlock()
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	return activeMACs, nil
}

func (c *Client) getHostCount(ctx context.Context, url string) (int, error) {
	soapBody := `<?xml version="1.0" encoding="utf-8"?>
<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/">
  <s:Body>
    <u:GetHostNumberOfEntries xmlns:u="urn:dslforum-org:service:Hosts:1" />
  </s:Body>
</s:Envelope>`
	
	req, _ := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(soapBody))
	req.Header.Set("Content-Type", "text/xml; charset=utf-8")
	req.Header.Set("SOAPAction", "\"urn:dslforum-org:service:Hosts:1#GetHostNumberOfEntries\"")
	req.SetBasicAuth(c.config.Username, c.config.Password)

	resp, err := c.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	var res struct {
		Body struct {
			Response struct {
				Count int `xml:"NewHostNumberOfEntries"`
			} `xml:"GetHostNumberOfEntriesResponse"`
		} `xml:"Body"`
	}
	xml.NewDecoder(resp.Body).Decode(&res)
	return res.Body.Response.Count, nil
}

func (c *Client) getHostEntry(ctx context.Context, url string, index int) (genericHostEntryResponse, error) {
	soapBody := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/">
  <s:Body>
    <u:GetGenericHostEntry xmlns:u="urn:dslforum-org:service:Hosts:1">
      <NewIndex>%d</NewIndex>
    </u:GetGenericHostEntry>
  </s:Body>
</s:Envelope>`, index)
	
	req, _ := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(soapBody))
	req.Header.Set("Content-Type", "text/xml; charset=utf-8")
	req.Header.Set("SOAPAction", "\"urn:dslforum-org:service:Hosts:1#GetGenericHostEntry\"")
	req.SetBasicAuth(c.config.Username, c.config.Password)

	resp, err := c.client.Do(req)
	if err != nil {
		return genericHostEntryResponse{}, err
	}
	defer resp.Body.Close()

	var res struct {
		Body struct {
			Response genericHostEntryResponse `xml:"GetGenericHostEntryResponse"`
		} `xml:"Body"`
	}
	xml.NewDecoder(resp.Body).Decode(&res)
	return res.Body.Response, nil
}
