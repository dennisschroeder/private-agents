package source

import (
	"bytes"
	"encoding/xml"
	"os"
	"strings"
	"testing"

	"golang.org/x/net/html/charset"
)

func TestParseListDevicesXML(t *testing.T) {
	data, err := os.ReadFile("../../ccu_response.xml")
	if err != nil {
		t.Fatalf("failed to read test xml: %v", err)
	}

	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.CharsetReader = charset.NewReaderLabel

	var res struct {
		Params []struct {
			Value struct {
				Array []Value `xml:"array>data>value"`
			} `xml:"value"`
		} `xml:"params>param"`
	}

	if err := decoder.Decode(&res); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	if len(res.Params) == 0 {
		t.Fatal("no params found in xml")
	}

	foundBlinds := 0
	for _, v := range res.Params[0].Value.Array {
		var addr, typeStr, parent string
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
		t.Logf("Checking: addr=%s, type=%s, parent=%s", addr, typeStr, parent)
		if typeStr == "BLIND" && parent != "" && strings.Contains(addr, ":") {
			foundBlinds++
		}
	}

	if foundBlinds != 2 {
		t.Errorf("expected 2 blind channels, found %d", foundBlinds)
	}
}
