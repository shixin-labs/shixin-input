# M0 spike 4: user dictionary handoff between engine processes

Date: 2026-10-26 · Apple Silicon Mac, macOS 26.7 · librime 1.17.0
(Homebrew) · rime-ice without Lua · Go 1.27.3, `CGO_ENABLED=0`

Code: `spikes/m0-handoff`. One child process per engine (librime is
process-global), driven over stdin. Scenarios run on scratch copies of the
deployed user dir from spike 1. Run with `go run ./spikes/m0-handoff`.

## Result: handoff works, but only if the standby never touches the user dictionary first

| Scenario | Learning preserved | LevelDB repaired | Engine unavailable |
|---|---|---|---|
| **Danger:** standby creates a session while the old engine holds the user dictionary | ❌ **lost**, both before and after | **yes** | — |
| Cold standby: no session until release, then `create_session` + `select_schema` | ✅ | no | 41–43 ms |
| Warm standby: own generation that starts on `rime_ice_warm` (no user dictionary), promoted with `select_schema("rime_ice")` after release | ✅ | no | 28–32 ms |

"Unavailable" runs from the old engine's release to the standby's first
query result, including the orchestrator's pipe round trips. Three runs
each.

## Findings

1. **A second librime process can destroy a live user dictionary.**
   - If a session's user dictionary fails to open (here: the LevelDB lock is
     held by the other engine), `UserDictionary::Load` schedules librime's
     `userdb_recovery_task`. That runs `leveldb::RepairDB` on the database,
     which doesn't take the lock. If the repair fails, the task renames the
     database to `.old` and recreates it from the sync snapshot.
   - Observed: LevelDB's log says "Repaired leveldb … Some data may have
     been lost", and the old engine's write-ahead log and manifest moved to
     `lost/`.
   - The old engine kept running and kept seeing its own learning (狮心,
     试验). After both quit, a fresh engine saw **none of it**.
   - The task is registered in librime's core `dict` module, so it's always
     active.
2. **`create_session` opens the default schema first.** Selecting a safe
   schema afterwards is too late: the first open already triggered the
   repair. The initial schema is the first `schema_list` entry of the
   engine's `default.yaml`, or `var/previously_selected_schema` from
   `user.yaml` if that's in the list (`Switcher::CreateSchema`).
3. **A safe warm standby needs its own generation:** its own `staging_dir`,
   with `default.yaml` listing only a warm variant of the schema
   (`translator/enable_user_dict: false`, same dictionaries). Its sessions
   then never open the user dictionary. librime caches tables and prisms
   per process (`weak_ptr` maps), so the warm session keeps the
   dictionaries loaded. After the old engine releases, the engine selects
   the real schema explicitly.
4. **Releasing is fast.** Destroying the old engine's sessions closes the
   LevelDB in 1–3 ms. Probing LevelDB's `LOCK` with `fcntl(F_GETLK)`
   reliably shows when it's free.
5. **The 20 ms budget is missed.** The warm handoff takes 28–32 ms. Most of
   it is `select_schema` building the schema's components (translators,
   filters, OpenCC simplifiers), not the user dictionary. Options:
   - Patch our librime build: skip recovery when the open failure is a lock
     conflict, and retry opening the user dictionary lazily. A warm session
     on the real schema could then simply attach the user dictionary after
     release, leaving roughly the release time plus a LevelDB open.
   - Cache OpenCC converters across sessions.

   Either way, the handoff happens only at an idle boundary, and clients
   queue keys without blocking the app (R1). The cost is a late first
   preedit, not a stall.

## Rules for the product

- **One engine per user dir has sessions at any time.** Enforce it with our
  own exclusive lock file, taken before librime creates any session.
  LevelDB's own lock doesn't protect against librime's recovery task.
- **A standby never creates a session on a schema with a user dictionary**
  until the old engine has released. Its generation's `default.yaml` lists
  only warm variants.
- **Before starting a replacement engine, make sure the old one is gone or
  has released.** For a hung engine: kill it and wait for the process to
  exit, which releases its locks. Never start a second engine "just in
  case".
- **Patch librime's recovery** in our build so it doesn't repair or
  recreate a database that another process has locked, and report it
  upstream.

## Not covered yet

- Engines started on different generation dirs with *different* compiled
  dictionaries (a real settings change). Here both used the same data.
- The supervisor, `Migrate` message and client switch-over (M1/M2).
- The librime patches suggested in finding 5.
