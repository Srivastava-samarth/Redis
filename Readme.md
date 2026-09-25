# Redis Lab

A hands-on Redis learning project built around a simple URL shortener using **Go, PostgreSQL, and Redis**.

The goal of this project is not to build a production-grade URL shortener, but to understand Redis concepts that are directly relevant to backend and fintech systems such as **rate limiting, idempotency, counters, caching, persistence, concurrency, and failure handling**.

---

## 🎯 Goals

The project focuses on understanding Redis through practical experiments:

* Redis data structures and basic commands
* Redis persistence using **RDB and AOF**
* Atomic counters using `INCR`
* Rate limiting
* Idempotency
* Caching
* Redis failure behavior
* Concurrency and race-condition behavior
* API load testing

The emphasis is on **understanding system behavior through experiments**, rather than simply learning Redis commands.

---

## 🏗️ Architecture

```text
                    ┌──────────────┐
                    │    Client    │
                    └──────┬───────┘
                           │
                           ▼
                    ┌──────────────┐
                    │   Go API     │
                    └──────┬───────┘
                           │
                    ┌──────┴───────┐
                    │    Service   │
                    └──┬─────────┬─┘
                       │         │
                       ▼         ▼
                 ┌─────────┐  ┌────────────┐
                 │  Redis  │  │ PostgreSQL │
                 └─────────┘  └────────────┘
```

Redis is used for:

* Counters
* Rate limiting
* Idempotency state
* URL caching

PostgreSQL remains the **source of truth for URL data**.

---

## 🛠️ Tech Stack

* Go
* Gin
* GORM
* PostgreSQL
* Redis
* Docker
* PowerShell
* Custom Go load-testing programs

---

# Redis Persistence

Redis primarily stores data in memory, but persistence allows data to survive Redis restarts.

This project experimented with both:

### RDB

RDB periodically creates snapshots of Redis data.

Current configuration:

```conf
save 60 10000
save 300 100
save 3600 1
```

This means Redis creates a snapshot when the configured number of changes occurs within the configured time window.

Manual snapshots were also tested using:

```redis
BGSAVE
```

A crash experiment demonstrated that writes made **after the latest snapshot** can be lost.

### AOF

AOF records write operations so Redis can reconstruct its state after a restart.

Configuration:

```conf
appendonly yes
appendfsync everysec
```

An abrupt Redis termination was tested using:

```bash
docker kill redis-lab
```

A key written before the crash was recovered after restarting Redis.

### Key takeaway

```text
RDB → Snapshot
AOF → Write log
```

Both approaches have different durability and performance characteristics.

---

# Atomic Counters

Redis `INCR` was used to create atomic counters.

Example:

```redis
INCR url:id
```

The URL shortener uses this to generate sequential short codes:

```text
url1
url2
url3
...
```

The important concept here is that `INCR` is atomic.

A naive implementation such as:

```text
GET
 ↓
increment in application
 ↓
SET
```

can produce race conditions when multiple requests execute concurrently.

Redis handles the increment atomically.

---

# Rate Limiting

A simple fixed-window rate limiter was implemented using:

```text
rate-limit:<client_id>
```

The counter is incremented using:

```redis
INCR rate-limit:<client_id>
```

The first request sets a 60-second expiration:

```redis
EXPIRE rate-limit:<client_id> 60
```

The current experiment uses a low limit during normal testing.

### Load test

A concurrent test was executed with:

```text
100 requests
100 concurrent workers
same client
```

With a limit of 5 requests per minute:

```text
Successful:      5
Rate limited:   95
Errors:          0
```

Redis failure was also tested.

When Redis was unavailable:

```text
Successful: 0
Errors:     100
```

The rate limiter therefore currently follows a **fail-closed** strategy.

---

# Idempotency

Idempotency was implemented to prevent multiple concurrent requests from creating duplicate URLs.

The Redis key format is:

```text
idempotency:<client_id>:<idempotency_key>
```

A request first attempts to claim the key using:

```redis
SET key processing NX EX 60
```

Only one concurrent request can successfully claim the key.

The flow is:

```text
Request
   │
   ▼
Check idempotency key
   │
   ├── Completed → return existing result
   │
   ├── Processing → recover/check existing DB record
   │
   └── Missing → claim key
                     │
                     ▼
                  Create URL
                     │
                     ▼
              Store completed result
```

PostgreSQL also enforces:

```sql
UNIQUE (client_id, original_url)
```

This provides a durable database-level safety net.

### Concurrent test

Test:

```text
100 concurrent requests
same client
same URL
same idempotency key
```

Result:

```text
Successful: 100
Conflicts:    0
Errors:       0
```

PostgreSQL contained exactly one URL record for the client and original URL.

The Redis idempotency key was then manually deleted and the same test was repeated.

The database still contained only one URL, demonstrating that Redis idempotency state is not the ultimate source of truth.

---

# Caching

URL caching was added to the GET endpoint using a **cache-aside pattern**.

Redis key:

```text
url:<short_code>
```

Example:

```text
url:url1001
```

The cached value contains:

```json
{
  "client_id": "client-1",
  "short_code": "url1001",
  "original_url": "https://google.com"
}
```

The cache does not currently have a TTL.

## Write Path

When a URL is created:

```text
POST /urls
      │
      ├──────────────► PostgreSQL
      │
      └──────────────► Redis cache
```

PostgreSQL remains the source of truth.

Redis stores a copy for fast reads.

## Read Path

The GET endpoint follows:

```text
GET /urls/:shortCode
          │
          ▼
        Redis
          │
      ┌───┴────┐
      │        │
     HIT      MISS
      │        │
      ▼        ▼
 Validate    PostgreSQL
 client_id      │
      │         ▼
      │      Redis SET
      │         │
      └────┬────┘
           ▼
        Response
```

The cached `client_id` is compared against the `X-Client-ID` supplied with the request.

This prevents a cached URL belonging to one client from being returned to another client.

---

## Cache Hit Experiment

A URL was created and verified in Redis:

```redis
GET url:url1001
```

Result:

```json
{
  "client_id": "client-1",
  "short_code": "url1001",
  "original_url": "https://google.com"
}
```

The GET endpoint successfully returned:

```text
https://google.com
```

The wrong client ID was also tested and correctly rejected.

---

## Cache Miss Experiment

The Redis cache entry was manually deleted:

```redis
DEL url:url1001
```

The GET endpoint was called again.

The URL was successfully returned from PostgreSQL.

Redis was then checked again and the cache entry had been recreated.

This demonstrated:

```text
Cache miss
    ↓
PostgreSQL
    ↓
Redis repopulation
    ↓
Response
```

---

## Redis Failure Experiment

Redis was abruptly terminated:

```bash
docker kill redis-lab
```

The GET endpoint was called while Redis was unavailable.

The request still successfully returned:

```text
https://google.com
```

because the service fell back to PostgreSQL.

This demonstrates an important distinction:

```text
Rate limiting / Idempotency
        ↓
Redis is part of correctness
        ↓
Redis failure → request failure


Caching
        ↓
Redis is an optimization
        ↓
Redis failure → PostgreSQL fallback
```

### Key takeaway

> Redis failure should hurt GET performance, not correctness, when Redis is being used only as a cache.

---

# API Load Testing

A custom Go worker-pool based load tester was created instead of using Go test cases.

Example experiment:

```text
Requests:       1000
Concurrency:    100
```

Observed baseline:

```text
Successful:     1000
Errors:            0
Throughput:     ~143 req/s
Average latency: ~672 ms
```

These numbers are environment-specific and are used primarily to understand how the API behaves under concurrent load rather than as a performance benchmark.

Redis was monitored during load testing to observe:

* `GET`
* `SET`
* `SETNX`
* `INCR`
* Connection behavior
* Evictions
* Rejected connections

No Redis evictions or rejected connections were observed during the experiment.

---

# Failure Testing

A major goal of the project is to deliberately break dependencies and observe system behavior.

Experiments include:

* Redis abrupt termination
* Redis restart
* RDB recovery
* AOF recovery
* Rate limiter failure
* Idempotency recovery
* Cache miss
* Cache repopulation
* Cache failure with PostgreSQL fallback
* Concurrent requests

The goal is not simply:

> "Does the API work?"

but:

> **"What happens when part of the system fails?"**

---

# Project Structure

```text
redis-lab/
├── main.go
├── controller/
│   └── url.go
├── service/
│   └── url.go
├── repository/
│   └── url.go
├── database/
│   └── models/
│       └── url.go
├── docker-compose.yml
├── docker/
│   └── redis/
│       └── redis.conf
├── migrations/
│   ├── 001_create_urls.up.sql
│   └── 001_create_urls.down.sql
├── loadtest/
│   ├── api/
│   ├── counter/
│   └── idempotency/
├── .env
└── go.mod
```

---

# What This Project Taught Me

### 1. Redis is not just a cache

Redis can be used for:

* Atomic counters
* Rate limiting
* Idempotency
* Caching
* Temporary state

### 2. Atomic operations matter

Operations such as:

```redis
INCR
SET NX
```

allow Redis to perform operations atomically without requiring application-level locking.

### 3. Redis and PostgreSQL have different responsibilities

A useful mental model from this project is:

```text
PostgreSQL
    ↓
Source of truth

Redis
    ↓
Fast state / coordination / optimization
```

The exact failure behavior depends on what Redis is being used for.

### 4. Failure behavior is part of system design

Testing the happy path is not enough.

Understanding:

```text
What happens when Redis dies?
What happens when cache data disappears?
What happens when concurrent requests arrive?
What happens when idempotency state is lost?
```

is an important part of backend engineering.

### 5. Load testing should test behavior, not just numbers

The purpose of the load tests was primarily to understand how the system behaves under concurrency and dependency failures rather than to optimize a particular benchmark number.

---

# Concepts Intentionally Not Implemented

The project deliberately avoids adding Redis features that are not currently relevant to the learning goals.

Not currently implemented:

* Redis Streams
* Redis Pub/Sub
* Distributed locks
* Redis sessions
* Redis Cluster
* Redis Sentinel

Queues and messaging will be explored separately through **NATS** rather than turning this project into a collection of unrelated Redis features.

---

# Next Steps

The Redis-specific learning objectives are now largely complete.

Potential future work:

* Improve observability
* Add metrics
* Improve error classification
* Explore Redis memory behavior
* Document experiments and findings

The main purpose of Redis Lab is to understand the concepts well enough to apply them to larger backend systems such as **SamPay**.
