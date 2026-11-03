package main

import (
	"context"
	"fmt"
	"hash/fnv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// NewServer builds the demo MCP server with fake-data tools.
func NewServer() *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "demo-mcp", Version: "0.1.0"}, nil)
	mcp.AddTool(s, &mcp.Tool{Name: "get_weather", Description: "Get the current weather for a city."}, getWeather)
	mcp.AddTool(s, &mcp.Tool{Name: "lookup_order", Description: "Look up an order's status by its id (e.g. ORD-1001)."}, lookupOrder)
	mcp.AddTool(s, &mcp.Tool{Name: "search_faq", Description: "Search the help center FAQ."}, searchFAQ)
	return s
}

type WeatherIn struct {
	City string `json:"city" jsonschema:"city name, e.g. Lisbon"`
}

type WeatherOut struct {
	City       string `json:"city"`
	Condition  string `json:"condition"`
	TempC      int    `json:"tempC"`
	HumidityPc int    `json:"humidityPct"`
}

var conditions = []string{"sunny", "partly cloudy", "cloudy", "light rain", "windy"}

func getWeather(_ context.Context, _ *mcp.CallToolRequest, in WeatherIn) (*mcp.CallToolResult, WeatherOut, error) {
	city := strings.TrimSpace(in.City)
	if city == "" {
		return nil, WeatherOut{}, fmt.Errorf("city is required")
	}
	h := hash(strings.ToLower(city))
	return nil, WeatherOut{City: city, Condition: conditions[h%uint32(len(conditions))], TempC: 8 + int(h%22), HumidityPc: 40 + int(h%50)}, nil
}

type OrderIn struct {
	OrderID string `json:"order_id" jsonschema:"order id, e.g. ORD-1001"`
}

type OrderOut struct {
	OrderID   string `json:"orderId"`
	Status    string `json:"status"`
	Carrier   string `json:"carrier,omitempty"`
	ETA       string `json:"eta,omitempty"`
	ItemCount int    `json:"itemCount"`
}

var orders = map[string]OrderOut{
	"ORD-1001": {OrderID: "ORD-1001", Status: "shipped", Carrier: "DHL", ETA: "2 days", ItemCount: 2},
	"ORD-1002": {OrderID: "ORD-1002", Status: "processing", ItemCount: 1},
	"ORD-1003": {OrderID: "ORD-1003", Status: "delivered", Carrier: "UPS", ItemCount: 4},
}

func lookupOrder(_ context.Context, _ *mcp.CallToolRequest, in OrderIn) (*mcp.CallToolResult, OrderOut, error) {
	id := strings.ToUpper(strings.TrimSpace(in.OrderID))
	o, ok := orders[id]
	if !ok {
		return nil, OrderOut{}, fmt.Errorf("order %q not found", in.OrderID)
	}
	return nil, o, nil
}

type FAQIn struct {
	Query string `json:"query" jsonschema:"what the customer is asking about"`
}

type FAQEntry struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
}

type FAQOut struct {
	Results []FAQEntry `json:"results"`
}

var faq = []FAQEntry{
	{"How do I return an item?", "You can return items within 30 days from the Orders page. Refunds take 5-7 business days."},
	{"How long does shipping take?", "Standard shipping takes 3-5 business days; express takes 1-2."},
	{"How do I change my password?", "Go to Settings > Security and choose Change password."},
	{"Do you ship internationally?", "Yes, to the EU, UK and Brazil."},
}

func searchFAQ(_ context.Context, _ *mcp.CallToolRequest, in FAQIn) (*mcp.CallToolResult, FAQOut, error) {
	words := strings.Fields(strings.ToLower(in.Query))
	out := FAQOut{Results: []FAQEntry{}}
	for _, e := range faq {
		text := strings.ToLower(e.Question + " " + e.Answer)
		for _, w := range words {
			if len(w) > 3 && strings.Contains(text, w) {
				out.Results = append(out.Results, e)
				break
			}
		}
	}
	return nil, out, nil
}

func hash(s string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	return h.Sum32()
}
