package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestSharedExchangeImportIsLocalForGuestsAndSignedInUsers(t *testing.T) {
	st := newTestStore(t)
	auth, password := newTestAuthStore(t)
	user, err := auth.authenticate("admin", password)
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := auth.createSession(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	srv := &server{store: st, auth: auth}
	exchange, err := EncodeMapExchangeString(json.RawMessage(`{"seed":4294967295,"peaceful_mode":true,"starting_area":2,"autoplace_controls":{"coal":{"frequency":2,"size":3,"richness":4}}}`), json.RawMessage(`{"pollution":{"enabled":false}}`))
	if err != nil {
		t.Fatal(err)
	}
	before, err := st.listProfiles()
	if err != nil {
		t.Fatal(err)
	}
	for _, signedIn := range []bool{false, true} {
		body, _ := json.Marshal(importExchangeStringRequest{Name: "Shared map", ExchangeString: exchange, Local: true})
		req := httptest.NewRequest(http.MethodPost, "/api/profiles/import-exchange", bytes.NewReader(body))
		if signedIn {
			req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
		}
		rec := httptest.NewRecorder()
		srv.handleProfile(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("signedIn=%v status=%d body=%s", signedIn, rec.Code, rec.Body.String())
		}
		var doc profileDocument
		if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
			t.Fatal(err)
		}
		if doc.ID != "local:Shared map" || doc.Source != profileSourceLocal {
			t.Fatalf("signedIn=%v returned nonlocal document: %+v", signedIn, doc)
		}
		var mapGen map[string]any
		if err := json.Unmarshal(doc.MapGen, &mapGen); err != nil {
			t.Fatal(err)
		}
		if mapGen["seed"] != float64(4294967295) || mapGen["peaceful_mode"] != true || mapGen["starting_area"] != float64(2) {
			t.Fatalf("lost shared map settings: %s", doc.MapGen)
		}
	}
	after, err := st.listProfiles()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("opening shared exchange changed server presets")
	}
}
