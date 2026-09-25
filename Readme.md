# Redis Lab

A hands-on Redis learning project built around a **Go URL shortener** backed by PostgreSQL and Redis.

The goal of this project is not to build a production-ready URL shortener. The URL shortener acts as a practical sandbox for understanding Redis concepts that are relevant to backend and fintech systems.

## 🎯 Goals

The project focuses on learning Redis through realistic backend scenarios:

* Redis fundamentals
* Redis persistence
* Counters
* Rate limiting
* Idempotency
* Concurrency and failure behavior
* API load testing

The approach is experiment-driven: **build → test under realistic conditions → observe behavior → understand the underlying concept.**

---

## 🏗️ Architecture

```text
                    ┌──────────────┐
                    │    Client    │
                    └──────┬───────┘
                           │
                           ▼
                    ┌──────────────┐
                    │   Go / Gin   │
                    │  Controller  │
                    └──────┬───────┘
                           │
                           ▼
                    ┌──────────────┐
                    │   Service    │
                    └──────┬───────┘
                           │
                    ┌──────┴───────┐
                    │              │
                    ▼              ▼
             ┌────────────┐ ┌────────────┐
             │   Redis    │ │ Repository │
             │            │ │            │
             │ Rate Limit │ │ PostgreSQL │
             │ Idempotency│ │            │
             │ Counters   │ │            │
             └────────────┘ └────────────┘
```

### Stack

* **Go**
* **Gin**
* **PostgreSQL**
* **Redis 8**
* **GORM**
* **Docker Compose**

---

## 📁 Project Structure

```text
redis-lab/
├── main.go
├── controller/
│   └── url.go
├── service/
│   └── url.go
├── repository/
│   └── url.go
├── model/
│   └── url.go
├── loadtest/
│   ├── api/
│   ├── counter/
│   └── idempotency/
├── docker-compose.yml
├── docker/
│   └── redis/
│       └── redis.conf
├── migrations/
│   ├── 001_create_urls.up.sql
│   └── 001_create_urls.down.sql
├── .env
└── go.mod
```

---

# Redis Concepts

## 1. Redis Fundamentals

Started with the basic Redis operations:

```text
SET
GET
DEL
INCR
TTL
EXPIRE
SETNX
```

The main concept learned here was that Redis operations can be atomic and extremely useful for coordinating application state.

---

## 2. Redis Persistence

Redis primarily operates in memory, but persistence allows data to survive Redis restarts.

Two persistence mechanisms were explored:

### RDB

RDB creates point-in-time snapshots of Redis data.

Example configuration:

```conf
save 60 10000
save 300 100
save 3600 1
```

The format is:

```text
save <seconds> <number-of-changes>
```

A manual snapshot can also be triggered with:

```text
BGSAVE
```

An experiment was performed by:

1. Creating a key.
2. Creating an RDB snapshot.
3. Writing another key.
4. Abruptly terminating Redis.
5. Restarting Redis.

The key written after the latest snapshot was lost.

This demonstrated the core RDB trade-off:

> **RDB provides snapshots, not a record of every write.**

### AOF

AOF records write operations so Redis can reconstruct its state.

The project uses:

```conf
appendonly yes
appendfsync everysec
```

An abrupt Redis restart was performed after writing a key, and the key survived.

This demonstrated the core distinction:

```text
RDB → Snapshot
AOF → Write log
```

`appendfsync everysec` provides better durability than relying only on periodic snapshots, while still allowing a small window of potential data loss.

---

# 3. Counters

Redis `INCR` was used to implement counters.

For example, URL short codes are generated using:

```text
INCR url:id
```

This is preferable to:

```text
GET counter
      ↓
increment in application
      ↓
SET counter
```

because concurrent requests could race with the GET → increment → SET approach.

Redis performs the increment atomically.

---

# 4. Rate Limiting

A simple fixed-window rate limiter was implemented using Redis.

Each client gets a counter:

```text
rate-limit:<client_id>
```

The request flow is:

```text
INCR rate-limit:<client_id>
        ↓
If count == 1
        ↓
Set 60 second TTL
        ↓
If count > 5
        ↓
Reject request
```

The configured limit is:

```text
5 requests / 60 seconds / client
```

### Load Test

100 concurrent requests were sent through the actual API.

Result:

```text
Total requests: 100
Concurrency:    100
Successful:       5
Rate limited:    95
Errors:           0
```

This demonstrated that Redis can act as a shared counter for enforcing a request limit.

### Redis Failure

Redis was then abruptly stopped and the same API load test was executed.

Result:

```text
Successful:     0
Rate limited:   0
Errors:       100
```

The API failed closed because rate limiting depends on Redis.

This also demonstrated an important architectural decision:

> When Redis is unavailable, the application does not bypass the rate limiter and continue processing requests.

---

# 5. Idempotency

Idempotency was implemented to simulate a common payment-system requirement:

> Retrying the same logical request should not create duplicate work or duplicate resources.

Each request uses:

```text
idempotency:<client_id>:<idempotency_key>
```

The key initially stores:

```text
processing
```

and after successful processing stores the resulting short code:

```text
url18
```

The initial claim uses:

```text
SET key processing NX EX 60
```

`NX` ensures that only one concurrent request can initially claim the idempotency key.

### Flow

```text
Request
   │
   ▼
Check Redis
   │
   ├── Completed → return existing result
   │
   ├── Processing → check DB for recovery
   │
   └── Missing
          │
          ▼
      SET NX
          │
          ├── Failed → request already processing
          │
          └── Claimed
                 │
                 ▼
              Create URL
                 │
                 ▼
          Save completed result
```

### Concurrent Test

100 concurrent requests were sent with:

* Same client
* Same URL
* Same idempotency key

Result:

```text
Total requests: 100
Successful:     100
Conflicts:        0
Errors:           0
```

The database contained only one URL for the client and original URL.

The test was repeated after manually deleting the Redis idempotency key.

Result:

```text
Successful: 100
Conflicts:    0
Errors:       0
```

The database uniqueness constraint allowed the application to recover safely even when Redis state was missing.

### Important Learning

Idempotency does not necessarily mean that only one request touches the database.

Concurrent requests may still perform reads while another request is processing.

The important property is:

> **The same logical request does not create duplicate resources.**

Redis provides fast coordination, while PostgreSQL provides durable correctness.

---

# 6. API Load Testing

The final experiment tested the **actual HTTP API**, rather than interacting with Redis directly.

The load test used:

```text
Requests:    1000
Concurrency: 100
```

### Baseline Result

```text
Total requests:    1000
Concurrency:       100
Successful:        1000
Errors:               0
Total duration:    6.98s
Requests/sec:     143.35
Average latency:  672.37ms
Minimum latency:  15.64ms
Maximum latency:   3.46s
```

The test demonstrated that the API could successfully process the complete concurrent workload without application-level errors.

Redis was also inspected during the load test using Redis monitoring and statistics.

The observed Redis operations included:

```text
GET
SET NX EX
INCR
SET
```

Redis reported:

```text
rejected_connections: 0
evicted_keys:         0
```

The experiment therefore provided a practical baseline for the current local setup.

The latency measurements are **environment-specific** and are not intended to represent production performance.

---

# 🔬 What This Project Demonstrated

| Concept                           | Demonstrated |
| --------------------------------- | ------------ |
| Redis commands                    | ✅            |
| Counters                          | ✅            |
| TTL / expiration                  | ✅            |
| RDB persistence                   | ✅            |
| AOF persistence                   | ✅            |
| Rate limiting                     | ✅            |
| Redis failure behavior            | ✅            |
| Idempotency                       | ✅            |
| Concurrent requests               | ✅            |
| Database uniqueness as safety net | ✅            |
| API load testing                  | ✅            |

---

# 🧠 Key Takeaways

### Redis is not just a cache

Redis can be used for:

* Fast counters
* Rate limiting
* Request coordination
* Idempotency state
* Temporary state with expiration

### Atomic operations matter

Operations such as:

```text
INCR
SET NX
```

are useful because Redis performs them atomically, reducing race conditions between concurrent application instances.

### Redis and PostgreSQL serve different purposes

In this project:

```text
Redis
→ fast, temporary coordination/state

PostgreSQL
→ durable source of truth
```

The combination is more useful than trying to make either system responsible for everything.

### Failure testing is as important as happy-path testing

Stopping Redis during the API load test showed how application behavior changes when a dependency becomes unavailable.

### Load testing should be done against the real system

Testing Redis in isolation would not reveal how:

```text
HTTP
 ↓
Gin
 ↓
Service
 ↓
Redis
 ↓
PostgreSQL
```

behaves as a complete system.

---

# 🚫 Concepts Intentionally Not Implemented

Not every Redis feature is useful for the current learning objective.

The following were intentionally left out:

* Redis caching
* Sessions
* Distributed locks
* Redis queues

Queues will be explored separately when working with **NATS**, rather than adding another messaging system to this project.

---

# 🚀 Future Scope

Possible extensions if they become relevant:

* More realistic load profiles
* Redis metrics and observability
* Expiration and recovery experiments
* More detailed latency measurements
* Comparing Redis behavior under different persistence configurations

The project will remain focused on **understanding Redis through backend problems**, rather than implementing Redis features for their own sake.
