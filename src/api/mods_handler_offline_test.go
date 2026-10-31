package api

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/OpenFactorioServerManager/factorio-server-manager/factorio"
	"github.com/gorilla/mux"
)

// TestCallRouteOffline exercises the refactored CallRoute helper without any
// network or Factorio mod setup. It verifies that the method, body, status
// code and JSON expectation are all validated through the new routeExpectation
// struct signature.
func TestCallRouteOffline(t *testing.T) {
	var receivedMethod string
	var receivedBody string
	var receivedID string

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		receivedID = mux.Vars(r)["id"]
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "error reading body", http.StatusInternalServerError)
			return
		}
		receivedBody = string(body)
		w.Header().Set("Content-Type", "application/json;charset=UTF-8")
		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte(`{"received":true}`))
	})

	// Matching expectation: must not fatal.
	CallRoute(t, "POST", "/api/test/{id}", "/api/test/fixture", strings.NewReader(`{"key":"value"}`), handler, routeExpectation{http.StatusAccepted, `{"received":true}`})

	if receivedMethod != "POST" {
		t.Fatalf("expected method POST, got %s", receivedMethod)
	}
	if receivedBody != `{"key":"value"}` {
		t.Fatalf("expected body %q, got %q", `{"key":"value"}`, receivedBody)
	}
	if receivedID != "fixture" {
		t.Fatalf("expected route ID fixture, got %q", receivedID)
	}
}

// TestAssertModEnabledStateOffline exercises the new assertModEnabledState
// helper using a synthetic mod-list.json written to a temp mods directory.
// It covers both enabled states. The missing-mod failure is preserved by the
// missing-mod failure branch in the helper. The explicit directory keeps the
// fixture independent of the global bootstrap configuration and server state.
func TestAssertModEnabledStateOffline(t *testing.T) {
	modsDir := t.TempDir()

	writeModList := func(t *testing.T, mods []factorio.ModSimple) {
		t.Helper()
		list := factorio.ModSimpleList{Mods: mods}
		data, err := json.Marshal(list)
		if err != nil {
			t.Fatalf("error marshalling mod list: %s", err)
		}
		if err := os.WriteFile(filepath.Join(modsDir, "mod-list.json"), data, 0644); err != nil {
			t.Fatalf("error writing mod-list.json: %s", err)
		}
	}

	// Mod enabled: expect enabled == true must pass.
	writeModList(t, []factorio.ModSimple{{Name: "belt-balancer", Enabled: true}})
	assertModEnabledState(t, modsDir, "belt-balancer", true)

	// Mod disabled: expect enabled == false must pass.
	writeModList(t, []factorio.ModSimple{{Name: "belt-balancer", Enabled: false}})
	assertModEnabledState(t, modsDir, "belt-balancer", false)
}
