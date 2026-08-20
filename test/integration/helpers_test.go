package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	httpapi "github.com/nemes1s/interbellum/internal/http"
	"github.com/nemes1s/interbellum/internal/repository/postgres"
	"github.com/nemes1s/interbellum/internal/service/alertservice"
	"github.com/nemes1s/interbellum/internal/service/investigationservice"
	"github.com/nemes1s/interbellum/internal/service/playbookservice"
)

// Node and edge IDs from test/fixtures/example-playbook.json. Naming them here
// lets the workflow tests read as the decision path a real analyst would take
// rather than as a sequence of opaque UUIDs.
const (
	nodeKnownWorkstation = "a0000000-0000-4000-8000-000000000001"
	nodeMaintWindow      = "a0000000-0000-4000-8000-000000000002"
	nodeSISRegister      = "a0000000-0000-4000-8000-000000000003"
	nodeHistorian        = "a0000000-0000-4000-8000-000000000004"

	terminalCloseAuthorized = "b0000000-0000-4000-8000-000000000001"

	edgeKnownWorkstationYes = "e0000000-0000-4000-8000-000000000001"
	edgeKnownWorkstationNo  = "e0000000-0000-4000-8000-000000000002"
	edgeMaintWindowYes      = "e0000000-0000-4000-8000-000000000003"
	edgeMaintWindowNo       = "e0000000-0000-4000-8000-000000000004"
	edgeSISRegisterYes      = "e0000000-0000-4000-8000-000000000005"
	edgeSISRegisterNo       = "e0000000-0000-4000-8000-000000000006"
	edgeHistorianYes        = "e0000000-0000-4000-8000-000000000007"
)

// repositories bundles the three repositories for tests that exercise
// persistence directly rather than through HTTP.
type repositories struct {
	playbooks      *postgres.PlaybookRepository
	alerts         *postgres.AlertRepository
	investigations *postgres.InvestigationRepository
}

func newRepositories(pool *pgxpool.Pool) repositories {
	return repositories{
		playbooks:      postgres.NewPlaybookRepository(pool),
		alerts:         postgres.NewAlertRepository(pool),
		investigations: postgres.NewInvestigationRepository(pool),
	}
}

// newTestServer builds the entire HTTP stack over the test database — the same
// router, middleware, services and repositories main.go wires up. API tests
// therefore exercise real routing, decoding and error mapping, not a stub.
func newTestServer(t *testing.T, pool *pgxpool.Pool) *httptest.Server {
	t.Helper()

	log := testLogger()
	repos := newRepositories(pool)

	router := httpapi.NewRouter(httpapi.Dependencies{
		Playbooks: playbookservice.New(repos.playbooks, log),
		Alerts:    alertservice.New(repos.alerts, log),
		Investigations: investigationservice.New(
			repos.investigations, repos.alerts, repos.playbooks, log),
		Logger:          log,
		Ping:            pool.Ping,
		MaxRequestBytes: 1 << 20,
	})

	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	return server
}

// client is a tiny HTTP helper. It exists so tests read as API calls and
// assertions rather than as request plumbing.
type client struct {
	t       *testing.T
	baseURL string
}

func newClient(t *testing.T, server *httptest.Server) *client {
	return &client{t: t, baseURL: server.URL}
}

// response holds a decoded API response.
type response struct {
	t      *testing.T
	Status int
	Body   map[string]any
	Raw    []byte
}

func (c *client) do(method, path string, body any, headers map[string]string) *response {
	c.t.Helper()

	var reader *strings.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			c.t.Fatalf("encode request body: %v", err)
		}
		reader = strings.NewReader(string(encoded))
	} else {
		reader = strings.NewReader("")
	}

	req, err := http.NewRequestWithContext(context.Background(), method, c.baseURL+path, reader)
	if err != nil {
		c.t.Fatalf("build request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw := make([]byte, 0, 4096)
	buf := make([]byte, 4096)
	for {
		n, readErr := resp.Body.Read(buf)
		raw = append(raw, buf[:n]...)
		if readErr != nil {
			break
		}
	}

	decoded := map[string]any{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &decoded); err != nil {
			c.t.Fatalf("%s %s: response is not a JSON object (status %d): %s",
				method, path, resp.StatusCode, raw)
		}
	}
	return &response{t: c.t, Status: resp.StatusCode, Body: decoded, Raw: raw}
}

func (c *client) post(path string, body any) *response {
	return c.do(http.MethodPost, path, body, nil)
}

func (c *client) postWithHeaders(path string, body any, headers map[string]string) *response {
	return c.do(http.MethodPost, path, body, headers)
}

func (c *client) put(path string, body any) *response {
	return c.do(http.MethodPut, path, body, nil)
}

func (c *client) get(path string) *response {
	return c.do(http.MethodGet, path, nil, nil)
}

// expectStatus fails with the response body included, which is what makes a
// failing API test diagnosable at a glance.
func (r *response) expectStatus(want int) *response {
	r.t.Helper()
	if r.Status != want {
		r.t.Fatalf("got status %d, want %d; body: %s", r.Status, want, r.Raw)
	}
	return r
}

// expectCode asserts the machine-readable error code, which is what clients
// actually branch on.
func (r *response) expectCode(want string) *response {
	r.t.Helper()
	got, _ := r.Body["code"].(string)
	if got != want {
		r.t.Fatalf("got error code %q, want %q; body: %s", got, want, r.Raw)
	}
	return r
}

func (r *response) str(key string) string {
	r.t.Helper()
	v, ok := r.Body[key].(string)
	if !ok {
		r.t.Fatalf("field %q is not a string in: %s", key, r.Raw)
	}
	return v
}

func (r *response) obj(key string) map[string]any {
	r.t.Helper()
	v, ok := r.Body[key].(map[string]any)
	if !ok {
		r.t.Fatalf("field %q is not an object in: %s", key, r.Raw)
	}
	return v
}

func (r *response) arr(key string) []any {
	r.t.Helper()
	v, ok := r.Body[key].([]any)
	if !ok {
		r.t.Fatalf("field %q is not an array in: %s", key, r.Raw)
	}
	return v
}

// loadFixture reads a fixture file into a generic map so tests can tweak
// individual fields before posting it.
func loadFixture(t *testing.T, name string) map[string]any {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("..", "fixtures", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("parse fixture %s: %v", name, err)
	}
	return decoded
}

// publishExamplePlaybook creates and publishes the assignment's example
// playbook, returning its version ID. Most tests start from a published
// playbook, so this is the shared first step.
func publishExamplePlaybook(t *testing.T, c *client) string {
	t.Helper()

	created := c.post("/api/v1/playbooks", loadFixture(t, "example-playbook.json")).
		expectStatus(http.StatusCreated)

	versions := created.arr("versions")
	if len(versions) != 1 {
		t.Fatalf("expected exactly one initial version, got %d", len(versions))
	}
	versionID, _ := versions[0].(map[string]any)["id"].(string)

	c.post("/api/v1/playbook-versions/"+versionID+"/publish", nil).
		expectStatus(http.StatusOK)
	return versionID
}

// createExampleAlert ingests the assignment's example alert and returns its ID.
func createExampleAlert(t *testing.T, c *client) string {
	t.Helper()
	return c.post("/api/v1/alerts", loadFixture(t, "example-alert.json")).
		expectStatus(http.StatusCreated).
		str("id")
}

// agentActor is the actor block an automated agent sends. Human and agent
// callers use exactly the same endpoint; only this field differs.
func agentActor() map[string]any {
	return map[string]any{"type": "agent", "id": "investigation-agent-v1"}
}
