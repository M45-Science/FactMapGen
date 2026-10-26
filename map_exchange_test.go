package main

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

// The shared URL came from SoftMod via ChatWire. Expected settings were
// independently decoded by helpers.parse_map_exchange_string in Factorio 2.1.23.
func TestDecodeChatWireFactorio2123Exchange(t *testing.T) {
	input, err := os.ReadFile("testdata/map-exchange/chatwire-2.1.23.txt")
	if err != nil {
		t.Fatal(err)
	}
	data, err := ParseMapExchangeString(string(input))
	if err != nil {
		t.Fatal(err)
	}
	if data.Version != [4]uint16{2, 1, 23, 2} || !data.ChecksumOK {
		t.Fatalf("version=%v checksumOK=%v", data.Version, data.ChecksumOK)
	}
	expectedRaw, err := os.ReadFile("testdata/map-exchange/chatwire-2.1.23.parsed.json")
	if err != nil {
		t.Fatal(err)
	}
	var expected struct {
		MapSettings map[string]any `json:"map_settings"`
		MapGen      map[string]any `json:"map_gen_settings"`
	}
	if err := json.Unmarshal(expectedRaw, &expected); err != nil {
		t.Fatal(err)
	}
	// Normalize Go numeric types to JSON's numbers for an independent comparison.
	actualRaw, err := json.Marshal(data.MapSettings)
	if err != nil {
		t.Fatal(err)
	}
	var actual map[string]any
	if err := json.Unmarshal(actualRaw, &actual); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, expected.MapSettings) {
		t.Fatalf("map settings differ from Factorio decoder:\ngot %s\nwant %v", actualRaw, expected.MapSettings)
	}
	if data.MapGenSettings["seed"] != uint32(266869157) {
		t.Fatalf("seed=%v", data.MapGenSettings["seed"])
	}
	for key, want := range expected.MapGen["autoplace_controls"].(map[string]any) {
		gotRaw, _ := json.Marshal(asMap(data.MapGenSettings["autoplace_controls"])[key])
		wantRaw, _ := json.Marshal(want)
		if string(gotRaw) != string(wantRaw) {
			t.Errorf("autoplace control %s=%s, want %s", key, gotRaw, wantRaw)
		}
	}
}
