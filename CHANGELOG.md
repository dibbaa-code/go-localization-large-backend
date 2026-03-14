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
