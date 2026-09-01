# Redis Lab — Persistence Notes

## 1. What is Redis Persistence?

Redis is primarily an in-memory database, so data exists in RAM.

If the Redis process/container crashes, data that exists only in memory can be lost.

**Persistence** means Redis writes data to disk so it can recover the dataset after a restart/crash.

Redis provides two major persistence mechanisms:

* **RDB (Redis Database)**
* **AOF (Append Only File)**

They can also be used together.

---

# 2. RDB — Redis Database Snapshots

RDB works by periodically taking a **snapshot of the current dataset** and storing it on disk.

Think of it like:

```text
RAM
 ↓
Snapshot
 ↓
dump.rdb
```

The snapshot represents the state of Redis at a particular point in time.

### RDB configuration

We initially saw Redis's default configuration:

```conf
save 3600 1
save 300 100
save 60 10000
```

Meaning:

```text
60 seconds   → snapshot if 10,000 changes occurred
300 seconds  → snapshot if 100 changes occurred
3600 seconds → snapshot if 1 change occurred
```

The general format is:

```text
save <seconds> <number-of-changes>
```

---

## 3. Manual RDB Snapshot

Redis can manually create a snapshot using:

```redis
BGSAVE
```

`BGSAVE` performs the snapshot in the background.

We ran:

```redis
BGSAVE
```

and observed:

```text
rdb_saves:1
rdb_last_bgsave_status:ok
rdb_changes_since_last_save:0
```

This showed that the snapshot was successfully created.

The snapshot was stored as:

```text
/data/dump.rdb
```

---

# 4. What RDB Can Lose

We tested what happens when a write occurs **after the latest RDB snapshot**.

The experiment was:

```text
BGSAVE
   ↓
Snapshot created
   ↓
SET crash:test ...
   ↓
docker kill redis-lab
   ↓
Redis crashes
   ↓
Redis starts again
```

After restarting Redis:

```redis
GET crash:test
```

returned:

```text
(nil)
```

### Why?

Because the key was created **after the snapshot**.

The snapshot didn't contain the new key.

Therefore:

```text
Latest RDB snapshot
        ↓
   crash happens
        ↓
writes after snapshot → potentially LOST
```

This is one of the most important characteristics of RDB.

### Key takeaway

> **RDB gives you point-in-time snapshots, not a record of every individual write.**

The advantage is that RDB snapshots are compact and generally efficient.

The trade-off is that recent writes can be lost between snapshots.

---

# 5. Why `docker kill` Instead of `docker stop`?

For our crash experiments, we used:

```powershell
docker kill redis-lab
```

rather than:

```powershell
docker stop redis-lab
```

`docker kill` abruptly terminates the container's main process.

This is useful for simulating an unexpected Redis process termination.

A graceful shutdown is not the same experiment as an abrupt crash because Redis gets an opportunity to shut down normally.

So when testing durability, we want:

```text
WRITE
 ↓
ABRUPT TERMINATION
 ↓
RESTART
 ↓
CHECK WHAT SURVIVED
```

---

# 6. AOF — Append Only File

AOF takes a different approach.

Instead of periodically storing only a snapshot, Redis records write operations in an append-only log.

Conceptually:

```text
SET name Samarth
SET age 24
DEL age
SET city Delhi
```

becomes a sequence of commands stored on disk.

When Redis starts again, it can replay those operations to reconstruct the dataset.

Conceptually:

```text
AOF
 ↓
Replay commands
 ↓
Reconstruct Redis dataset
```

---

# 7. Our AOF Configuration

We created a dedicated Redis configuration file:

```conf
port 6379

appendonly yes
appendfsync everysec

save 60 10000
save 300 100
save 3600 1
```

The important AOF settings are:

```conf
appendonly yes
```

This enables AOF persistence.

And:

```conf
appendfsync everysec
```

This tells Redis to periodically flush/fsync the AOF approximately once per second.

---

# 8. Why We Created `redis.conf`

Initially, we were changing Redis configuration at runtime:

```redis
CONFIG SET appendonly yes
```

Those changes are runtime configuration changes.

They don't automatically become the permanent configuration used when Redis starts again.

We experienced this problem ourselves:

```text
CONFIG SET appendonly yes
        ↓
AOF enabled
        ↓
Redis restarted
        ↓
appendonly reverted to "no"
```

That caused our earlier AOF experiment to be misleading.

### Solution

We created:

```text
docker/
└── redis/
    └── redis.conf
```

and changed Docker Compose to:

```yaml
volumes:
  - redis_data:/data
  - ./docker/redis/redis.conf:/usr/local/etc/redis/redis.conf

command: redis-server /usr/local/etc/redis/redis.conf
```

Now Redis starts using our configuration file every time.

---

# 9. Verifying AOF

After starting Redis with the new configuration:

```redis
CONFIG GET appendonly
```

returned:

```text
appendonly
yes
```

And:

```redis
CONFIG GET appendfsync
```

returned:

```text
appendfsync
everysec
```

So we confirmed that our configuration was actually being loaded.

---

# 10. AOF Experiment

We created:

```redis
SET aof:test "hello-aof"
```

Then verified:

```redis
GET aof:test
```

which returned:

```text
"hello-aof"
```

Redis reported:

```text
aof_enabled:1
aof_current_size:65
aof_base_size:0
```

The important point was:

```text
aof_current_size > 0
```

meaning the AOF contained data.

---

# 11. AOF Crash Recovery Test

We then performed the important experiment:

```text
SET aof:test "hello-aof"
        ↓
AOF records the write
        ↓
docker kill redis-lab
        ↓
Redis crashes
        ↓
docker start redis-lab
        ↓
Redis loads/replays persistence data
        ↓
GET aof:test
```

The result was:

```text
"hello-aof"
```

### What did we prove?

The write survived an abrupt Redis termination.

So unlike our RDB test, the write did not depend solely on the latest RDB snapshot.

---

# 12. RDB vs AOF

The core difference we've learned:

### RDB

```text
RAM
 ↓
Periodic snapshot
 ↓
dump.rdb
```

If Redis crashes:

```text
Load latest snapshot
```

Therefore:

```text
writes after snapshot
        ↓
potentially LOST
```

### AOF

```text
WRITE
 ↓
AOF log
 ↓
Redis restarts
 ↓
Replay AOF
```

Therefore, writes that have been safely persisted to the AOF can be recovered.

---

# 13. `appendfsync everysec`

Our current configuration is:

```conf
appendfsync everysec
```

This provides a useful balance between performance and durability.

Conceptually:

```text
WRITE
 ↓
AOF buffer
 ↓
periodic fsync
 ↓
disk
```

There can be a small durability window.

If Redis crashes before a very recent write has been fsynced, that write may potentially be lost.

So:

```text
everysec
    ↓
better performance
    +
small potential data-loss window
```

It is **not equivalent to zero-loss durability**.

---

# 14. Current Redis Lab Architecture

Our project is completely isolated from the existing application.

```text
redis-lab/
│
├── main.go
├── docker-compose.yml
├── .env
├── go.mod
│
├── docker/
│   └── redis/
│       └── redis.conf
│
├── internal/
│   ├── handler/
│   ├── service/
│   └── repository/
│
├── migrations/
└── loadtest/
```

Docker:

```text
RedisLab Redis
localhost:6380
       │
       ▼
Redis container :6379

RedisLab Postgres
localhost:5434
       │
       ▼
Postgres container :5432
```

---

# 15. Current Redis Configuration

Our current `redis.conf`:

```conf
port 6379

appendonly yes
appendfsync everysec

save 60 10000
save 300 100
save 3600 1
```

Therefore we currently have:

```text
                Redis
                  │
        ┌─────────┴─────────┐
        │                   │
       RDB                 AOF
   snapshots            write log
        │                   │
   dump.rdb          appendonly files
```

Both persistence mechanisms are enabled.

---

# 16. Important Commands We Learned

### Check RDB configuration

```redis
CONFIG GET save
```

### Check AOF configuration

```redis
CONFIG GET appendonly
```

### Check AOF fsync mode

```redis
CONFIG GET appendfsync
```

### Check persistence statistics

```redis
INFO persistence
```

### Manually create an RDB snapshot

```redis
BGSAVE
```

### Check a key

```redis
GET key
```

### Abruptly kill Redis

```powershell
docker kill redis-lab
```

### Start Redis again

```powershell
docker start redis-lab
```

---

# 17. The Main Mental Model

The most important thing to remember is:

```text
RDB = "What did my database look like at this snapshot?"

AOF = "What operations happened that I can replay?"
```

Or even shorter:

```text
RDB → Snapshot
AOF → Log
```

RDB is generally useful when you want compact snapshots and efficient backups.

AOF is useful when you want a more continuous record of writes and better recovery granularity.

Using both gives you two complementary persistence mechanisms.

---

# 18. What We Have NOT Tested Yet

The next persistence experiments will be:

1. `appendfsync everysec` vs `always`
2. Demonstrate the durability window of `everysec`
3. Understand Redis 8's multi-part AOF structure
4. Understand:

   * base RDB
   * incremental AOF
   * manifest
5. AOF rewrite
6. Why AOF files don't grow forever
7. RDB + AOF interaction during recovery
8. Persistence performance under load
9. Automated crash/recovery tests

After persistence, we'll move into:

```text
Memory
   ↓
Eviction
   ↓
Cache avalanche
   ↓
Cache stampede
   ↓
Rate limiting
   ↓
Distributed locks
   ↓
Streams
   ↓
Load testing
```

The goal is not just to memorize Redis commands, but to **create failures and observe Redis behavior under controlled experiments**.
