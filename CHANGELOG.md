# Changelog

## Task 1: A/B Testing (Experimentation System)

### What Changed

The `/experiment` endpoint now implements a deterministic A/B testing system. Each user is assigned exactly one of the 6 payload files in `/payloads`, and that assignment never changes.

### How It Works

1. All 6 payload JSON files are loaded into memory at server startup.
2. When a request comes in with a `userId`, the server uses **MurmurHash3 + modulo** to deterministically assign the user to a payload cohort.
3. The same `userId` always maps to the same payload — the assignment is deterministic.
4. Users are distributed evenly across all payloads.

### Why MurmurHash3 + Modulo (and not direct modulo)

This is the industry-standard approach used by major experimentation platforms. The bucketing works as:

```
bucket = MurmurHash3(userId + ":" + experimentId) % numberOfPayloads
```

**Why not just `userId % numberOfPayloads` (direct modulo)?**

Direct modulo only works on numeric IDs and breaks badly in practice:
- **UUIDs are strings** — you can't modulo a string. You need a hash function to convert it to a number first.
- **Sequential numeric IDs produce biased splits** — if user IDs are auto-incrementing (1, 2, 3, ...), then `id % 6` puts users 1, 7, 13, 19... all in the same bucket. Users who signed up around the same time end up in the same cohort, which biases experiments by registration date.
- **Poor avalanche effect** — similar inputs (e.g., `user-100` vs `user-101`) produce similar outputs. A good hash function ensures that even tiny input changes produce completely different bucket assignments.

**Why MurmurHash3 specifically:**
- Strong avalanche effect — every bit of input affects every bit of output, ensuring uniform distribution even with patterned IDs
- ~2.4 GB/s throughput — fast enough for high-traffic bucketing with negligible CPU overhead
- Battle-tested across Hadoop, Cassandra, Elasticsearch, Kafka, and nginx
- Cross-platform determinism — same algorithm produces identical results in Go, Python, JavaScript, etc., so mobile clients and backend can agree on assignments independently

**Why include `experimentId` in the hash key:**
- Prevents cross-experiment contamination — without it, the same users always land in the same buckets across different experiments, which biases results
- The `CurrentExperimentID` constant in `main.go` should be changed when launching a new experiment

### Usage

```bash
curl -X POST http://localhost:3000/experiment \
  -H "Content-Type: application/json" \
  -d '{"userId": "user-abc-123"}'
```

**Response:**
```json
{
  "experimentId": "exp-6",
  "selectedPayloadName": "localization_dummy_3.json",
  "payload": "{ ... full payload content ... }"
}
```

- `experimentId` — identifies this experiment (includes the number of cohorts)
- `selectedPayloadName` — which payload file this user was assigned
- `payload` — the full JSON content of that payload

### Files Modified

- `main.go` — replaced single hardcoded payload with multi-payload loading + MurmurHash3 bucketing + request validation
- `go.mod` / `go.sum` — added `github.com/spaolacci/murmur3` dependency

---

## Task 2: Load Test for Experimental Allocation Verification

### What Changed

The existing load test (`cmd/loadtest/main.go`) now verifies allocation determinism alongside performance metrics. Each client sends requests with a stable `userId` (UUID), and the test checks that every user always receives the same payload across all their requests.

### What Was Added

1. **User identity per client** — each fast/slow client gets a unique UUID at startup and sends it as `userId` in every request.
2. **Allocation tracking** — every response is parsed and the `selectedPayloadName` is recorded per user. After the test, the results are analyzed for consistency.
3. **Determinism check** — verifies that each user received exactly one payload across all their requests. If any user got different payloads on different requests, the test reports FAIL.
4. **Payload distribution** — shows how users are split across the 6 cohorts with a visual bar chart.
5. **Results file output** — all performance and allocation results are written to a markdown file (`load_test_results.md` by default, configurable via `-output` flag).

### How to Run

```bash
# Normal mode (fast clients only)
go run cmd/loadtest/main.go -fast 10 -requests 20 -duration 15s

# With slow clients (saturation mode)
go run cmd/loadtest/main.go -fast 10 -slow 5 -requests 20 -duration 30s -mode saturation

# Custom output file
go run cmd/loadtest/main.go -fast 10 -requests 20 -duration 15s -output my_results.md
```

### Sample Output (Allocation Section)

```
Allocation Determinism Check:
  Total Users:      60
  Consistent:       60/60 (100.0%)
  Inconsistent:     0
  PASS - Every user received the same payload on every request

Payload Distribution:
  localization_dummy_3.json             8 users ( 13.3%) ######
  localization_dummy_4.json            10 users ( 16.7%) ########
  localization_example.json             8 users ( 13.3%) ######
  localization_example_2.json          12 users ( 20.0%) ##########
  nested_large.json                    12 users ( 20.0%) ##########
  small_payload.json                   10 users ( 16.7%) ########
```

### Files Modified

- `cmd/loadtest/main.go` — added userId per client, allocation tracking, determinism analysis, and file output
- `cmd/allocation-test/main.go` — standalone allocation test (can also be used independently)

---

## Task 3: Production Hardening

### The Problem

When the server sends ~1MB responses, slow clients (poor network connections) hold connections open while downloading. A fast client takes ~10ms; a slow one at 128KB/s takes ~8 seconds. If enough slow clients pile up, they exhaust the connection pool, causing fast clients to queue even though the server's CPU is idle.

### What I'd Do

**Layer 1: Limit how long slow clients can hold connections**

- **WriteTimeout (10s)** — if the client can't receive the full response within 10 seconds, drop the connection. Puts a hard cap on how long any slow client can occupy a connection.
- **ReadTimeout (5s)** — max time to read the full request. Prevents slowloris-style attacks where clients send headers very slowly.
- **IdleTimeout (30s)** — closes keep-alive connections that sit idle. Prevents pool exhaustion from clients that connect and never send.
- **Concurrency limit** — cap max connections (e.g., 512) so the server rejects excess load rather than degrading into unresponsiveness.

**Layer 2: Architectural (production deployment)**

- **CDN for payloads** — since the payloads are static files that don't change per-request, they can be pre-uploaded to a CDN. The server resolves the cohort and responds with a 302 redirect to the CDN URL (e.g., `cdn.example.com/payloads/localization_dummy_3.json`). The client follows the redirect and downloads the large payload from the CDN edge, not from our server. One request from the client's perspective, but the Go server only does the lightweight bucketing — the CDN handles the slow download.
