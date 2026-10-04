# WhatyGo - Changelog

## Unreleased (lucasgiovannibr/whatygo)

Fixes and hardening on top of upstream v0.7.2. Full triage of the upstream issues
and pull requests in `FORK-TRIAGE.md`.

### Upgrade notes
- **Wording and telemetry notice.** The README, the guide and the Swagger description now say the
  project is "based on" the Evolution Go instead of calling it a fork (the credit in `LICENSE`,
  `NOTICE`, the README and the manager footer is unchanged). The README and chapter 8 of the guide
  used to say the server collects anonymous usage data; that was the license heartbeat, which no
  longer exists, so they now state that WhatyGo sends no telemetry. The GitHub description,
  homepage and topics of the repository were updated.
- **Repository layout.** The entry point moved from `cmd/evolution-go` to `cmd/whatygo`, so
  `go build ./cmd/whatygo`, `go run cmd/whatygo/main.go` and `swag init -g cmd/whatygo/main.go`
  replace the old paths (the `Makefile` and the `Dockerfile` already use them; the binary
  built by `make build` is now `build/whatygo`). In `docker/`, `fork-test/` became
  `test-stack/` (now with a README and a `.env.example`; the Compose project name is unchanged,
  so existing test volumes are kept) and `stack-evocrm.yml` was removed: it pulled a
  third-party image (`intrategica/evg:1`) and carried someone else's database credentials.
  The local test image is now tagged `whatygo:test`.
- **Own names instead of the upstream's** (the project is in development, so nothing was kept
  for compatibility): Go module `github.com/lucasgiovannibr/whatygo`; Prometheus metrics
  `evolution_*` are now `whatygo_*` (update dashboards and alerts); the media folder in the
  bucket is `whatygo-medias/` (it was `evolution-go-medias/`; media already stored under the old
  folder is not found); the instance-lock key, the NATS connection name and the outgoing
  `User-Agent` (`WhatyGo/1.0`) follow; the container user is `whatygo`; the databases in the
  examples, docs and test stack are `whatygo_auth` and `whatygo_users` (they were `evogo_*`;
  rename an existing one with `ALTER DATABASE evogo_auth RENAME TO whatygo_auth;`, or keep your
  own names in `POSTGRES_AUTH_DB` / `POSTGRES_USERS_DB`); the Compose service, container, volume
  and network names in `docker/` and in the docs are `whatygo*`, and the test stack's project is
  `whatygo-test`. The `public/` folder (the upstream's logos and donation QR codes, unused) was
  removed. The upstream is still credited in `LICENSE`, `NOTICE`, the README and the manager's
  footer.
- **The product is now called WhatyGo** (a fork of Evolution Go; the original's name, logo and
  colours are no longer used as this project's identity, see `TRADEMARKS.md` and
  `docs/guia/11-avisos-legais-e-creditos.md`). What changes for you:
  - **Docker image**: `ghcr.io/<owner>/whatygo` (it was `ghcr.io/<owner>/evolution-go`, which gets
    no more updates). The `docker/examples/*.yml` files point to the new image.
  - **Manager**: new name, logo and colours; the credit line "Baseado no Evolution Go © 2026
    Evolution Foundation" stays (menu and sign-in page). Browser storage keys are now
    `whatygo-*`, so the theme and the menu state are reset once; the Swagger title and the
    startup log say WhatyGo.
  - **Passkey Helper** extension renamed to "WhatyGo Passkey Helper".
  - Unchanged on purpose: the Go module path, the `cmd/evolution-go` folder, the `evolution_*`
    metric names and the `evolution` container user.
- **New files**: `CONTRIBUTING.md`, `SECURITY.md`, `docs/guia/` (a beginner's guide) and
  `docker/instalacao-simples/` (a ready Docker Compose).
- **The license gate is gone.** The server no longer answers `503 LICENSE_REQUIRED`, the
  manager no longer asks for a registration, and **nothing is sent** to the upstream's
  licensing service (no activation, no 30-minute heartbeat, no deactivation, no use of
  `GLOBAL_API_KEY` as a license key). `pkg/core` was removed and so were the routes
  `/license/status`, `/license/register` and `/license/activate` (they now answer 404), and
  the variable `EVOLUTION_OPERATOR_EMAIL` is ignored. Nothing else depended on the licence:
  instance tokens and sessions are not derived from it, so **existing instances keep the same
  tokens and sessions**. The table `runtime_configs` is no longer created or read; an existing
  one is left untouched (the previous image can still use it), and it holds the upstream's
  license key in clear text, so drop it when you no longer need to go back:
  `DROP TABLE runtime_configs;`. Analysis, options and legal review in
  `docs/LICENCA-ANALISE.md`.
- **whatsmeow updated** (30/06 → 29/09/2026, 72 commits) and **Go 1.26** is now
  required (Dockerfile updated). The whatsmeow schema moves from **v14 to v16**;
  the migrations are forward-only, so **back up `evogo_auth` before deploying** —
  the previous image cannot run on the upgraded database.
- Docker images are published to `ghcr.io/<owner>/<repo>` by the fork's workflow.
- **`poll_votes` migration** (idempotent, runs at startup): the unique constraint
  `(poll_message_id, voter_jid)` is replaced by a unique index that includes
  `instance_id`. Going back to an older image on that database breaks saving poll
  votes (its `ON CONFLICT` target no longer exists); everything else keeps working.
- New optional environment variables: `DISAPPEARING_AUTO_APPLY` (default on),
  `WEBHOOK_QUEUE_MAX_EVENTS` (1000), `WEBHOOK_QUEUE_MAX_MB` (64) and
  `WEBHOOK_QUEUE_WORKERS` (4); for calls, `CALL_MAX_CONCURRENT` (4), `CALL_RING_TIMEOUT`
  (90 s), `CALL_STREAM_GRACE` (10 s), `CALL_DIAL_LIMIT` (6 per minute) and
  `CALL_STREAM_ORIGINS`.
- **New database column** `instances.calls_enabled` (boolean, default false), added by
  the automatic migration at startup.

- **Breaking changes of the hardening round** (PRs #38–#72; the project is not in production,
  so the cleaner behaviour was chosen — read before upgrading):
  - **Event payloads no longer carry `instanceToken`** (it is the instance's API key and
    every consumer of the events could use it). `WEBHOOK_INCLUDE_TOKEN=true` brings it back
    for an integration that still reads it.
  - **`WADEBUG` and `LOGTYPE` are the real names now.** The code used to read `DEBUG_ENABLED`
    and `LOG_TYPE` while every example and the docs said `WADEBUG`/`LOGTYPE`; the old names
    stay as fallbacks. Debug lines are now written only with `LOG_LEVEL=debug` (they used to
    be written always).
  - **The server refuses to start with a published `GLOBAL_API_KEY`** (the values of the
    examples, `change-me`...). `ALLOW_INSECURE_API_KEY=true` starts anyway, for local use.
  - **Failed requests answer `{"error": "<text>", "code": "<code>"}`** and the status says
    what kind of failure it is (503 not connected, 409 not paired / disconnected through the
    API / running on another replica, 429 + `Retry-After`, 404, 504, 502, 400...). They used
    to be a `500` for nearly everything. `error` is unchanged; the codes are listed in
    `docs/wiki/referencia/error-codes.md`.
  - **`POST /instance/disconnect` really disconnects.** It used to restart the client (and
    emit a `LoggedOut` event that never happened). An instance disconnected through the API is
    no longer started again by the next request that needs a client (`409
    instance_disconnected_by_user`); `POST /instance/connect` starts it.
  - **URLs supplied in requests may only point to public addresses** (media, stickers, link
    previews, status): private networks, loopback and the cloud metadata endpoint are refused.
    `ALLOW_PRIVATE_URLS=true` allows the private ones; webhooks may always target the internal
    network.
  - **The MinIO bucket is no longer made public.** Media is stored under
    `evolution-go-medias/<instanceId>/` and served through presigned URLs
    (`MINIO_URL_TTL_HOURS`, default and maximum 168); it is removed with the instance.
    `MINIO_PUBLIC_BUCKET=true` restores the old public policy (it replaces the bucket's own).
  - **The Docker image runs as a non-root user** (uid 10001), has a `HEALTHCHECK` and is based
    on Alpine 3.24. The entrypoint fixes the ownership of an existing root-owned volume. Use
    `stop_grace_period: 30s` (the compose examples do).
  - **Request bodies are limited** (`MAX_BODY_MB`, default 4; routes that receive a file
    `MAX_MEDIA_BODY_MB`, default 150) and CORS follows `CORS_ORIGINS` (empty or `*` allows every
    origin).
  - **The proxy password is no longer returned** by the instance endpoints.
  - **The manager keeps its session in `sessionStorage`** (it ends with the tab; the old
    `localStorage` entry is removed) and is served with a Content-Security-Policy.
  - Messages table: `messages` gains `instance_id` and the unique key is now
    `(instance_id, message_id)`; the migration drops the old `message_id` constraint (older
    rows keep an empty `instance_id`). Indexes on `instances.client_name` and
    `labels.instance_id`.
- New optional environment variables (defaults in brackets):
  `WEBHOOK_INCLUDE_TOKEN` (false), `ALLOW_INSECURE_API_KEY`, `ALLOW_PRIVATE_URLS`,
  `CORS_ORIGINS`, `MAX_BODY_MB` (4), `MAX_MEDIA_BODY_MB` (150), `LOG_LEVEL` (info),
  `LOG_KEEP_DELETED` (false), `CHECK_USER_CACHE_TTL_MIN` (720), `MINIO_PUBLIC_BUCKET` (false),
  `MINIO_URL_TTL_HOURS` (168), `MAX_IMAGE_MEGAPIXELS` (50), `MAX_CONCURRENT_CONVERSIONS`
  (max(2, CPUs/2)), `DB_MAX_OPEN_CONNS` (25), `DB_MAX_IDLE_CONNS` (10),
  `DB_CONN_MAX_LIFETIME_MIN` (5), `DB_CONN_MAX_IDLE_MIN` (1), `SEND_MAX_CONCURRENT` (4),
  `SEND_RATE_PER_MIN` (off), `SEND_QUEUE_WAIT_SEC` (30), `MEDIA_WORKERS` (4),
  `MEDIA_WORKERS_PER_INSTANCE` (2), `MEDIA_ORDERED` (false), `STARTUP_STAGGER_MS` (300),
  `RECONNECT_BACKOFF_BASE_SEC` (5), `RECONNECT_BACKOFF_MAX_SEC` (300), `INSTANCE_LOCK` (on).
  The full table is in `docs/wiki/referencia/environment-variables.md`.

### Fixes
- **Process crashes**: shared instance maps are now synchronized (`fatal error:
  concurrent map writes`); WebSocket writes are serialized per connection;
  `events.Archive` no longer panics; duplicate concurrent reconnects are ignored;
  panics in service-owned goroutines are recovered and logged.
- **Postgres connection leak** (`too many clients already`): one shared whatsmeow
  store container on top of the bounded auth pool instead of a new pool per
  reconnect.
- **Lifecycle**: supervisor goroutines end when their client is replaced (one was
  leaked per reconnect); KeepAlive timeouts trigger a reconnect; instances caught
  mid-reconnect are restored on `CONNECT_ON_STARTUP`; the resolved WhatsApp version
  is applied to the handshake.
- **Security**: `/instance/{id}/advanced-settings` is scoped to the global key or
  the instance's own token; `ForceUpdateJid` uses a bound SQL parameter; global key
  comparison is constant-time and rejected WebSocket tokens are no longer logged.
- **Config**: `/instance/connect` and advanced-settings updates are partial (no
  more silent reset of events/RabbitMQ/flags); `Disconnect` keeps the event
  subscriptions.
- **Messages**: incoming edits and poll votes are decrypted before the LID/PN
  swap; `/group/participant` validation; mentionAll with documents; carousel
  button parameters; image `Width/Height`; animated stickers; avatar, edit and
  revoke with canonical JIDs; `/user/profileName`; history-sync request sent as a
  peer message; unpaired instances fail fast on send.
- **Events**: `Passkey*` follow the `QRCODE` subscription; `PICTURE`, `USER_ABOUT`
  and `BUTTON_CLICK` are published to NATS/AMQP; `KeepAliveTimeout/Restored`
  events under `CONNECTION`.

### Found in live testing (29/09/2026)
- **One runtime per instance**: `POST /instance/connect` followed by `GET /instance/qr`
  started the instance twice; the unpaired duplicate forced a logout and restarted the
  instance as a new device right after a successful pairing.
- **Deleted instances no longer restart** (a closed kill channel now means "stop for
  good"; a restart is skipped when the instance row is gone).
- `/user/info` timed out because of the `+` in the JID (now canonical, 0.3 s).
- `/group/participant` answered "success" when nothing was added; it now returns the
  per-participant result (`data`, `failed`).
- Poll votes from `@lid`-only voters store the real phone number; `viewOnce` and
  `quoted.text` also work in multipart `/send/media`.
- `POST /user/contacts`: optional `saveOnPrimaryAddressbook` (contacts cannot be
  removed through the API).

### Operational events from whatsmeow
- `ReachoutTimelock`, `StreamError` and `ClientOutdated` are now published under the
  `CONNECTION` subscription and shown in `GET /instance/{id}/runtime`.
- The bare "server returned error 463" of a send now explains the reachout restriction
  (and until when, if WhatsApp said so); the original error stays in the text.
- On `ClientOutdated` (405) the cached WhatsApp Web version is dropped so the next
  reconnection fetches the current one instead of retrying the refused version.

### Diagnostics
- `GET /health` (readiness: databases with a 2s bound, `slow`/`error` states, 503 when a
  database is down) next to the unchanged `GET /server/ok` (liveness).
- `GET /instance/{id}/runtime` and `GET /instance/runtimes`: what the process actually
  runs per instance vs the database, with coded warnings, plus process stats
  (goroutines, memory). `ENABLE_PPROF=true` exposes `/debug/pprof` behind the global key.
- The logger of a deleted instance is released (its log file descriptor was kept open
  for the life of the process); `qrcodeCount` is atomic.
- `docs/WHATSMEOW-CAPABILITIES.md`: what whatsmeow delivers, what the project uses
  (76 of 136 client methods, 70 of 75 event types, after the work listed below; the other five are never delivered on their own) and the
  hard limits of the library.

### Additions (small)
- Endpoints (details in `docs/wiki/guias-api/api-fork-additions.md`): `viewOnce` in
  `/send/media`; `POST /send/pollVote`; `POST /message/subscribe` (contact
  presence); `POST /user/lid`; `POST /user/contacts`; `PictureURL` in
  `/user/info`; `POST /group/requests` and `/group/requests/update`;
  `GET /instance/proxy/{id}` (runtime proxy status, no credentials) and
  `PROXY_FAIL_CLOSED`.
- `passkey-helper` 1.1.0: WebAuthn runs in the page's MAIN world so password
  managers (1Password, Bitwarden) work; requires Chrome/Edge 111+.
- `quoted.text` (optional) fills the quote card of replies.
- `/instance/qr` keeps returning the QR alongside the passkey fields.
- `REREQUEST_FROM_PHONE` (opt-in) re-requests undecryptable messages.
- CI (build, vet, `test -race`) and a Postgres integration test for the pool fix
  (`EVOGO_TEST_POSTGRES_DSN`).

### Pairing and chat-state events
- `PairError`, `QRScannedWithoutMultidevice` (under `QRCODE`) and `CATRefreshError`
  (under `CONNECTION`) are published, so a failed pairing is no longer silent.
- `Mute`, `Pin`, `Star`, `MarkChatAsRead`, `ClearChat`, `DeleteChat`, `DeleteForMe`,
  `UnarchiveChatsSetting` and `UserStatusMute` (changes made on another device) are
  published under `CHAT_PRESENCE`; the full sync after pairing is not.

### Disappearing messages (#79), group invites and channels
- Messages sent to a chat with disappearing messages now carry the chat's timer
  (learned from received messages, the `EPHEMERAL_SETTING` protocol message and group
  info; groups are re-read after a restart). `DISAPPEARING_AUTO_APPLY=false` turns it
  off. New `POST /chat/disappearing` and `POST /user/defaultDisappearing`.
- `POST /group/inviteinfo` (by link/code or invite card, without joining) and
  `POST /group/joininvite` (from an invite card).
- Channels: `POST /newsletter/{follow,unfollow,mute,markviewed,react}`.

### Messages that did not arrive
- The `UndecryptableMessage` event is published under `MESSAGE` (it used to be only a
  log line), and `POST /message/rerequest` asks the phone for another copy of it.

### Chat routes, presence and batch subscribe
- `/chat/pin|unpin|archive|unarchive|mute|unmute` (marked "not working" in the router)
  wrote the app-state patch under `+<number>@s.whatsapp.net`, a chat that does not
  exist, and the phone keys one-to-one chats by LID. The JID is now canonical and
  resolved to the LID; groups were never affected. Bad input is a 400 (was 500), the
  returned timestamp is real (was a zero time) and `/chat/mute` accepts an optional
  `duration` (`8h`, `1w`, `always`, `30m`; the default stays 1 hour). Chat and message
  labels had the same bug and now use the same helper.
- `POST /message/subscribe` accepts a list of numbers (up to 100) and reports each one
  (`data` / `failed`); a single string answers as before.
- Switching `alwaysOnline` at runtime takes effect immediately (presence mark and
  scheduler) instead of at the next reconnect; a guard prevents a second scheduler.

### Fixes from code-review rounds
- `POST /user/block|unblock` timed out because of the `+` in the JID.
- `GET /group/myall` always answered empty (the owner is a LID and was compared with a
  mangled own JID).
- `POST /community/add|remove` built the success/failed lists wrongly and sent
  unparseable groups as zero JIDs.
- A label deleted on the phone stayed in the local table and in `GET /label/list`.
- Outbound HTTP had no timeout (media URLs, link previews, the WhatsApp Web version
  lookup that holds a lock every instance start goes through, webhooks); everything now
  goes through clients with dial, header and total timeouts.
- An instance without proxy JSON made `/instance/connect` and the client start fail when
  a global proxy is configured in the environment.
- `/send/link`: a page that cannot be read, a relative `og:image` or a missing image no
  longer fail the send; values sent by the caller win over the scraped ones; `url` is
  honoured; the title is `og:title` or the first `<title>`; trailing punctuation is not
  part of the link.
- Media fetched from a URL rejects non-2xx answers (a 404 page was sent as the file) and
  is size-limited (100 MB media, 20 MB images, 2 MB link thumbnails); carousel and button
  header media that cannot be fetched are logged instead of vanishing silently.
- ffmpeg audio conversion times out after 3 minutes.
- `/send/poll`: 2 to 12 options, no empty or repeated option, `maxAnswer` within the
  options (a larger value was silently turned into "unlimited"). `/send/location`:
  latitude/longitude 0 are valid (only the pair 0,0 counts as missing) and ranges are
  checked.
- `SendMessage` looks the client up once, so an instance stopped mid-send no longer leaves
  a nil to dereference.
- Deleting an instance purges its stored device (session/identity/sender keys, cached
  contacts, LID map) and its poll votes; a paired instance that was not connected used to
  keep all of it in the auth database. The phone still lists the session as a linked
  device in that case.
- Poll votes are unique per instance: two instances in one group both receive a poll and
  the second used to overwrite the first's row.
- `POST /user/check` returns the error when the WhatsApp query fails (it answered
  `200` with `data: null`): 400 for an invalid number, 429/504 for rate limits and
  timeouts. `POST /message/delete` returns the real timestamp. `GET /user/contacts` is
  `[]` when empty and in a stable order.

### Remaining whatsmeow events
- `Blocklist`, `PrivacySettings`, `BusinessName` (under `CONTACT`); `CallPreAccept`,
  `CallTransport`, `CallReject`, `UnknownCallEvent` (`CALL`); `MediaRetry` (`MESSAGE`);
  `NewsletterLiveUpdate`, `NewsletterMuteChange` (`NEWSLETTER`) and `OfflineSyncPreview`
  (`CONNECTION`) are published instead of falling into the "Unhandled event" log line.
- `RotateADVSecret` no longer writes the session's old and new ADV secret into the
  instance log (the generic log line printed the whole event); it is handled without
  being published.

### Waiting for the connection, and three lookups
- Every service started an instance and then slept a fixed 2 s before checking that it
  had connected (3 s after a reconnect, 3 s + 2 s in `GET /instance/qr`, 2 s in
  `ForceReconnect`): a connection that took 2.1 s failed the request and one that took
  0.3 s still cost 2 s. They now wait for the connection itself (10 s upper bound), an
  unpaired instance still fails at once, and `GET /instance/qr` waits until a QR code,
  a passkey ceremony or a login exists (0.35 s instead of a fixed 3 s in the live test).
- `POST /user/devices` (linked devices of one or several users, device 0 is the phone),
  `GET /user/statusprivacy` (who sees my status) and `POST /user/business` (public profile
  of a Business account; 404 for an ordinary number), all bounded to 10 s.

### Webhook delivery queue
- Webhooks go through one bounded queue per destination URL instead of one goroutine per
  event: limits in events and bytes, at most a few workers, the OLDEST event is dropped
  (and counted) when full, retries back off 1 s / 5 s / 30 s / 2 min with jitter, and a
  destination whose event exhausted its retries gets a single attempt per event until one
  succeeds. `WEBHOOK_QUEUE_WORKERS=1` keeps strict order. The same URL as the global
  webhook is no longer delivered twice. `GET /instance/runtimes` reports the queues
  (`webhook`: pending, in flight, degraded destinations, sent, failed, dropped).
- `POST /instance/connect`: an empty `webhookUrl` still means "unchanged" (the bundled
  manager sends `""` on every reconnect); `"disabled"` or `"false"` now clears the
  webhook (stored empty; a legacy stored `"disabled"` is still ignored on delivery).

### `PUT /instance/{id}/integrations`
- Stores the webhook URL, the subscribed events and the RabbitMQ/WebSocket/NATS switches
  of an instance **without starting it** (`POST /instance/connect` did this but connected
  too). Takes effect at once when the instance runs, otherwise on the next connection.
  Empty fields keep their value, `webhookUrl: "disabled"` removes the webhook,
  `subscribe: ["ALL"]` selects every event.

### Manager panel rebuilt
- `/manager` was rebuilt from scratch (React 19, TypeScript, Vite, Tailwind v4; light and
  dark theme). The source is in `manager/`; the built `manager/dist` is versioned and
  what the Docker image ships (see `manager/README.md`).

### WhatsApp calls: answer, dial, video and a stream over WebSocket (experimental)
Until now the only call feature was `POST /call/reject`. Opt in per instance with
`callsEnabled` (`PUT /instance/{id}/advanced-settings`, **effective on the next
connection**). The media side uses the `purpshell/meowcaller` library, pinned to commit
`6d9b7b2c1807` (its `main` moved to a different whatsmeow fork).
- **Routes**: `GET /call/active`, `GET /call/{callId}`, `POST /call/answer`,
  `POST /call/dial`, `POST /call/hangup`, `POST /call/stream-ticket`,
  `GET /call/stream/{callId}` (WebSocket) and `POST /call/video`; `POST /call/reject`
  and `rejectCall` now go through the engine when it is on.
- **Stream**: audio is PCM 16-bit, 16 kHz mono in base64 JSON messages (Twilio Media
  Streams style); with `video: true` in the ticket, video is H.264 Annex-B, one access
  unit per message, with `keyframe_request` and `video_state` messages. Authentication is
  a one-time ticket (30 s) instead of the API key in the URL; browsers must come from an
  origin in `CALL_STREAM_ORIGINS`.
- **Video**: `start` asks the peer to turn an audio call into video (an iPhone accepts),
  `accept`, `stop`, `enable`/`disable` (mute and unmute), `orientation`. `enable` is refused
  with `409` on a call that never had video: the iPhone ignores "camera on" without the
  upgrade request. The rotation of each received picture is delivered as the clockwise
  quarter turns that make it upright (the library documents the RTP value as clockwise, but
  it counts counter-clockwise); `video_state.orientation` is the device's and must not be
  used to rotate.
- **Events** (`CALL`): `CallReady`, `CallEnded` (`reason`: `peer_hangup` when the other
  side hangs up, because WhatsApp sends no reason then; `hangup`, `rejected`,
  `rejected_busy`, `ring_timeout`, `stream_closed`, `server:<code>`) and `CallVideoState`
  (`state` names what the peer signalled: `enabled`, `disabled`, `stopped`,
  `upgrade_request`, `upgrade_accepted`, `upgrade_rejected`, `upgrade_cancelled`, `unknown`,
  plus the raw `stateCode`; the iPhone sends an unnamed code 2 right after pickup).
- **Safeguards**: instances with a **proxy** cannot use calls (the library opens a UDP
  socket that ignores the proxy, which would leak the IP): `state: "blocked_by_proxy"` and
  the `calls_blocked_by_proxy` warning; at most `CALL_MAX_CONCURRENT` calls per instance;
  `CALL_DIAL_LIMIT` calls placed per minute (failures count); a running call whose stream
  stays away for `CALL_STREAM_GRACE` is hung up; everything an operator can see is in
  `GET /instance/runtimes` (`runtime.calls` and warnings).
- **Side effect**: with the engine on, every incoming call is pre-accepted by the library.
- **Not done on purpose**: group calls and recovering a stream that dropped after the
  grace period. (A maximum duration now exists, off by default: see the next section.)
- **Tested live** with a real number and an iPhone: incoming and outgoing audio,
  incoming and outgoing video, audio-to-video upgrade in both directions, the phone turned
  through several positions, and a portrait (360x640) picture filling the phone's screen
  (a landscape one is letterboxed). `docker/fork-test/call-stream-test.py` repeats it.
- Guide, WebSocket protocol and examples: `docs/wiki/guias-api/api-call.md`.

### Calls: a better stream, safeguards, history and metrics (October 2026, PRs #75–#82)
Found by analysing the call stream against the library, Twilio Media Streams and the OpenAI
Realtime API. The WebSocket is only the client's leg: WhatsApp's media is SRTP over UDP
inside the call library. Everything below was tried on a real number and an iPhone except
where noted. The guide is `docs/wiki/guias-api/api-call.md`.

**The stream**
- **Audio format** (#77): `encoding` and `sampleRate` on `POST /call/stream-ticket` and on
  `POST /call/dial` with a stream: PCM 8/16/24 kHz, G.711 mu-law or A-law at 8 kHz. The call
  keeps running at 16 kHz; the stream converts (a resampler that keeps its state between
  chunks, no aliasing from 16 to 8 kHz). An unsupported format is a `400`, and on dial it is
  refused before the phone rings.
- **Binary frames and timestamps** (#78): `binary: true` moves audio and video out of base64
  JSON into binary WebSocket frames (a third smaller); control messages stay JSON. Every
  media frame carries `timestamp` (milliseconds from the opening of the stream to its
  arrival at the server), because a silent or muted peer sends only 2-3 frames a second: a
  recorder has to place frames by it and fill the gaps (a 68 s call gave a 29 s file).
- **`mark`** (#76): the server echoes it once the audio sent before it has been handed to the
  call (what an agent needs to know how much of its sentence was played when interrupted);
  `clear` returns every waiting mark. The queue of the peer's audio is **900 ms** (was 3 s)
  and a client that falls behind gets one `inbound_overflow`.
- **Speech events** (#81): `speechEvents: true` adds `speech_start` / `speech_end` (an
  energy detector that follows the room's noise: about 120 ms to start, 600 ms hangover;
  the end is also found by the clock). Not a speech recogniser.
- **Fix** (#82): the library asks the client's source for audio from the moment a call
  exists, ringing included, so a greeting queued while the phone rang was thrown away and a
  mark set after it came back as "played". The audio is now held until the call is active.
  Found while testing everything together.

**Safeguards**
- **Media stall** (#75): `CallMediaStalled` / `CallMediaResumed` (and `mediaStalled` on
  `GET /call/{callId}`) when an active call with a stream gets no audio for `CALL_MEDIA_STALL`
  seconds (default 15, `0` off); `CALL_MEDIA_STALL_HANGUP=true` also hangs up
  (`media_stalled`). A muted or silent iPhone keeps sending 2-3 frames a second, so it does
  not trigger it. The real stall the library's issues describe was **not reproduced**: only
  unit-tested.
- **Limits** (#79), both off by default: `CALL_MAX_DURATION` (reason `max_duration`) and
  `CALL_SILENCE_TIMEOUT` (reason `silence_timeout`, only for a call with a stream, neither
  side making a sound; comfort noise does not count, a soft voice does).

**History and metrics**
- **Call history** (#80): `CALL_HISTORY=true` keeps one row per call in `call_records`
  (peer and its phone number when the account can resolve the `@lid`, direction, video,
  outcome `answered/missed/rejected/cancelled/unanswered/busy/failed`, reason, times, ring and
  talk seconds). **Metadata only: calls are not recorded** (decided on purpose, legal and
  privacy risk). `GET /call/history` (filters, cursor pagination) and `DELETE /call/history`;
  records expire after `CALL_HISTORY_RETENTION_DAYS` (default 90, `0` = for ever). `unanswered`
  and `failed` were not seen on a device.
- **Metrics** (#75, #80): `evolution_calls_active`, `evolution_calls_started_total`,
  `evolution_calls_ended_total`, `evolution_call_talk_seconds`, `evolution_call_dials_total`,
  `evolution_call_engines`, stream frames and drops, keyframe requests, stalls and the history
  counters, with bounded labels.

**Manager**
- **Calls tab** on the instance: the calls the engine follows right now (answer, reject, hang
  up, timer, stream counters, "no audio" warning), a dial box and the call history (filters,
  paging, erase). A **browser phone** puts the page's microphone and speakers on a call
  through the stream (binary PCM 16 kHz, speech events, playout by timestamp); the stream is
  opened before answering, and a refused microphone does not answer the call. The microphone
  is read by an AudioWorklet emitted as its own file because the panel's CSP does not allow a
  Blob worklet. The Behavior tab gets the `callsEnabled` switch. Tried live in a desktop browser: answer
  from the page, mute, hang up, and dial from the page.
- **Video in the browser phone**: the other side's video is decoded with WebCodecs and drawn on
  the page (turned upright by the quarter turns the server reports); the camera is cropped to
  portrait 360x640, encoded to H.264 baseline and sent (the encoder's access unit delimiters are
  stripped). A "Videochamada" switch on the dial card, a camera button (on an audio call it asks
  the other side to upgrade), and the other side's request for video is **accepted at once** by
  default (a switch turns it off, leaving an Accept button): a phone withdraws its request after
  a few seconds, and measured with the live script, accepted at 0.5 s the video came, at 6 s the
  phone had already gone back to voice. Without a camera it keeps receiving. Receiving was tried with an
  iPhone; sending was tried with the browser's **fake camera** (the test computer has none): the
  iPhone showed the test pattern upright and filling the screen, 258 pictures in 16 s, none
  dropped; the iPhone's request for video was accepted at once on a computer without a camera
  and its video stayed (107 pictures). Not tried: a real camera, "Start video" on a voice call,
  the manual Accept button, and browsers other than Edge.

### Hardening and scale round (October 2026, PRs #38–#72)
Result of a full analysis of the system (security, scalability, memory, send speed, error
returns); every change below has tests, and the structural gains were measured.

**Security**
- Outbound HTTP policy enforced at the connection (SSRF): one client for media, stickers, link
  previews and status; webhooks keep their own client. Decompression limits before decoding an
  image (a crafted PNG bomb is refused) and ffmpeg/pdftoppm run behind a slot pool with timeout
  and output caps.
- Published API keys are refused at startup; access logs redact the query string; the log
  directory of a deleted instance is removed; the proxy password never leaves the API.
- `chai2010/webp` 1.4.0 (CVE-2023-4863 in the bundled libwebp, found by Trivy), pgx, x/image,
  pion/dtls and others updated; `govulncheck` runs in CI and `go 1.26.8` is required.
- Manager: session in `sessionStorage`, CSP (`script-src 'self'`, no framing, no `<base>`),
  `X-Frame-Options`, `nosniff`, `Referrer-Policy`.

**Speed and memory**
- Send path: the "is this number on WhatsApp" lookup is cached (12 h, 5 min for negatives,
  concurrent sends of one number share one query); `/user/check` accepts up to 100 numbers;
  retries redo only the send, never the download or the conversion; the `SendMessage` event is
  built after the API has answered and no longer downloads again the file just uploaded.
- Receive path: events nobody subscribed to are not built (no media download, no group query,
  no serialization: `CallWebhook` went from 21 MB to 440 B allocated per 20 MB event);
  `GetGroupInfo` is cached for 2 minutes and invalidated by group events; messages with media are
  processed by bounded workers (`MEDIA_WORKERS`) instead of blocking the handler.
- Bodies are read once and edited in place (a 100 MB upload: 393 MB → 136 MB peak); the logger
  writes from a goroutine per instance (2,965 → 1,106 ns/op on the logging goroutine).
- Message persistence: one batching writer (queue 8192, batches of 200 / 250 ms) instead of a
  goroutine per message; a late receipt can no longer turn a `Read` message back into
  `Delivered`.

**Reliability**
- RabbitMQ publishes through one confirmed channel (confirmations are read, queues declared
  once, no 4 s sleep in the send path); NATS keeps reconnecting; webhook, RabbitMQ, NATS and
  WebSocket outputs are independent (one down no longer blocks the others).
- Ordered shutdown: clients, message writer, webhook queues (drained until the deadline), WebSocket
  subscribers (close frame), brokers; the stored `connected` state is kept so
  `CONNECT_ON_STARTUP` restores the instances.
- Automatic reconnection is paced (0, 5, 10, 20 s ... up to 5 min, jitter, reset after a stable
  minute) and instances start one every ~300 ms; API-requested reconnects are not paced.
- One replica per instance (Postgres advisory locks, `INSTANCE_LOCK`), so two replicas can no
  longer run the same WhatsApp account.
- Targeted database updates instead of full-row `Save` (a QR code or connection state changed
  meanwhile was overwritten with stale values); one-statement `UpdateConnected`; configurable
  pool.
- Data races fixed (the instance record swapped while the handler reads it; whatsmeow's global
  identity rewritten by every start).

**API**
- Per-instance send limit (`SEND_MAX_CONCURRENT`, `SEND_RATE_PER_MIN`) answering `429` with
  `Retry-After`.
- Typed errors with a stable `code` in every handler and middleware (see the upgrade notes).
- `GET /metrics` (Prometheus, global key): HTTP traffic and latency, WhatsApp events, connected
  instances, webhook queues, database pools, dropped messages, batch sizes, throttled sends,
  media queue. `X-Request-ID` on every response and in the access log.
- `GET /instance/{id}/logs` returns the newest lines first.

**Engineering**
- CI: gofmt, golangci-lint (govet, staticcheck SA*, ineffassign, unused), `go test -race` with
  real RabbitMQ, NATS and Postgres service containers, manager typecheck/tests/build/`npm
  audit`, Trivy on the image, dependabot.
- `ensureClientConnected` (nine copies) is one `ClientProvider`; `handleEvent` went from ~1,300
  to ~640 lines (received message, receipt, logged out, pair success, media, JID clean-up, quoted
  context and button clicks are functions of their own).
- Dead code removed; the test logger is closed when a test ends.

### Interactive messages that render (October 2026)
Tested live on a WhatsApp Business account linked as a device, sending to an iPhone and to
WhatsApp Web (HTTP 200 only means the server accepted a message; clients silently drop what
they do not understand). Details and the method in `FORK-TRIAGE.md` §3.
- **`/send/button` fixed (#59, #110, #170, #204 of upstream).** Reply and CTA buttons
  (copy/url/call) are now a plain `InteractiveMessage` with a native flow announced as
  `<native_flow v="9" name="mixed"/>` plus `<bot biz_bot="1"/>`, with no
  `DocumentWithCaptionMessage` wrapper. The legacy `ButtonsMessage` was always refused (405)
  and the wrapped CTA was refused (473). Reply buttons take an image or video header again.
  `reply` mixed with CTA buttons is accepted now (the phone shows it, WhatsApp Web does not).
  Pix is unchanged. Tapping a button arrives as `ButtonClick`.
- **`/send/carousel` showed on WhatsApp Web but not on the iPhone**: an empty card header
  `title`/`subtitle` was sent as an empty string. Empty fields are left out now.
- **`/send/list` is refused by WhatsApp for linked devices** (405/479, Business accounts
  included; every format tried). A refused list is now sent as reply buttons (3 per message, at
  most 9 rows, the `rowId` is the button id); the response carries `Fallback: "buttons"` and
  `Parts`. `fallbackButtons: false` answers `502 whatsapp_rejected` with an explanation.
- **Media download errors are client errors**: an unusable URL (error status, unreachable, not
  http) is `400 invalid_media_url` and a file over the limit is `413 payload_too_large`; both
  were a `500 internal_error`.
- **Manager "Testar envio" tab** sends all 12 `/send/*` types (text, link, location, contact,
  poll, media, sticker, buttons, list, carousel, status text and status media) with a form per
  type, a typing delay, a cURL view, the attempts of the session and the remembered number.
  Status asks for a confirmation because it reaches the account's contacts.

### Documentation
- `docs/swagger.*` regenerated with swag v1.16.3 (`--parseDependency`; it had not been
  regenerated since the 0.7.2 sync): 28 routes added (calls, `/instance/{id}/integrations`,
  diagnostics, `/send/pollVote`, newsletters, etc.), none removed. (The `/license/*` routes
  that were declared in `pkg/core/license_swagger.go` were removed later, with the license gate.)
- A test that read `CallEnded` right after the call's `Done` channel closed could run
  before the event was published (and panic on the empty log); it now waits for the event.

## v0.7.2

**Docker:** `evoapicloud/evolution-go:0.7.2`

### 🆕 New Features
- **Passkey (WebAuthn) pairing** — support for linking accounts that the WhatsApp
  server locks behind a **passkey** (the *Shortcake* / CRSC flow). When the
  server demands a passkey, whatsmeow's `PairPasskeyRequest` is surfaced through a
  new ceremony flow: the backend mints a short-lived ceremony token, and a bundled
  browser extension (`passkey-helper`) runs the WebAuthn assertion on the
  `web.whatsapp.com` origin and posts it back. Three public endpoints drive it:
  `GET /passkey-ceremony/{token}`, `POST .../response`, `POST .../confirm`. The
  manager detects the passkey stage and shows an "Abrir WhatsApp Web" button.
  Confirmation is always manual (never auto-confirm on `SkipHandoffUX`).
  Configure the public API base via **`PASSKEY_PUBLIC_URL`**. Full guide:
  `docs/wiki/guias-api/passkey-pairing.md`. Note: there is no headless bypass —
  the ceremony requires the account owner's real authenticator; the extension is
  web-only.
- **Headless license auto-activation** — set `EVOLUTION_OPERATOR_EMAIL` to the
  email used in your first manual license registration; on startup the service
  silently calls `/v1/register/auto` and skips the browser flow (falls back to the
  manual flow if the email isn't registered yet).
- **Button message media support** — additional media handling for interactive
  button messages.

### 🔧 Improvements / CI
- **Dropped the whatsmeow fork — now uses official `go.mau.fi/whatsmeow`.** The
  project previously vendored a fork (`whatsmeow-lib` submodule) to carry a
  PostgreSQL pool patch; upstream rejected that patch in favor of `NewWithDB`
  (app-side config). Removing the fork also pulled in upstream's native passkey
  support. Pinned to the commit that adds passkeys
  (`v0.0.0-20260630180629-b572e5bcb92b`). The `sync-releases` workflow no longer
  re-adds the submodule.
- **QR pairing consumes `events.QR` directly** instead of `GetQRChannel`. The QR
  channel auto-confirms passkey on `SkipHandoffUX` and disconnects the socket when
  codes run out — both break an in-flight passkey ceremony. Connecting without it
  keeps the socket alive for as long as pairing (QR or passkey) needs. QR rotation
  now pauses while a passkey ceremony is active.
- **Public sync fixes** — the release workflow drops the obsolete whatsmeow-lib
  step, targets `evolution-foundation/*`, and now ships the `passkey-helper`
  extension to the public repo.

### 🐛 Bug Fixes
- **`POST /instance/pair` returned an empty `PairingCode`** — the handler
  swallowed `PairPhone` errors and returned HTTP 200 with `PairingCode: ""`, and
  the client wasn't connected/awaiting-auth before `PairPhone`. Now starts the
  instance, waits for the websocket, and surfaces real errors (#21).
- **`GET /instance/status` returned 400 after a manual disconnect** — now returns
  200 with the disconnected status instead of erroring until a container restart
  (#20).

### 🏷️ Org rename
- Repository references updated from **EvolutionAPI** to **evolution-foundation**
  (module path, imports, GitHub URLs, submodule URLs).

## v0.7.1

**Docker:** `evoapicloud/evolution-go:0.7.1`

### 🆕 New Features
- **Test-send modal in Manager** — new modal in the embedded manager UI to test message sending directly from the panel, covering text, media and interactive message types. Useful for validating an instance right after pairing without leaving the manager.

### 🔧 Improvements / CI
- **whatsmeow-lib SHA now pinned in the public sync** — the `sync-releases` workflow previously re-cloned whatsmeow `main` on every run, so the SHA listed in the CHANGELOG could drift from what the public repos actually built against. The workflow now captures the SHA from the dev submodule and checks out that exact commit in the target, restoring release reproducibility.
- **Repository cleanup** — dropped tracked binaries (`evolution-go`, `build/server`), IDE config (`.idea/`) and scratch files (`DIFF-COMPLETO.txt`, `API-INTERACTIVE-DOCS.txt`, `carousel-sender.html`). Expanded `.gitignore` to prevent reincidence.

### 📝 Docs
- **Postman collection** — added `Set Proxy` request and multipart hints on `/send/media`; collection file renamed from `Evolution GO.postman_collection (2).json` to `Evolution GO.postman_collection.json`.
- **Interactive messages docs** — additional examples and corrections.

## v0.7.0

**Docker:** `evoapicloud/evolution-go:0.7.0`

### 🆕 New Features
- **Multi-platform interactive messages** — Buttons, lists and carousel working on Android, iOS and WhatsApp Web/Desktop
  - **SendButton**: removed `ViewOnceMessage` wrapper that blocked rendering on iOS and WhatsApp Web; `Footer` and `Header` are now conditional
  - **SendList**: migrated from `InteractiveMessage`/`NativeFlowMessage` to legacy `ListMessage` (native protobuf) for broad compatibility
  - **SendCarousel**: new endpoint `POST /send/carousel` with cards (image, text, footer, buttons) and automatic JPEG thumbnail generation for instant image loading
  - `whatsmeow-lib`: added `biz` node for `InteractiveMessage` and pinned `product_list` type on the `biz` node for `ListMessage`
- **Base64 media support on `/send/media`** — The `url` field on `POST /send/media` now also accepts base64-encoded media. When the value does not start with `http://` or `https://`, it is treated as base64 and decoded; reuses the existing `SendMediaFile` flow
- **WhatsApp status endpoints** — new `POST /send/status/text` and `POST /send/status/media` publish text/image/video status to `status@broadcast`. Media endpoint supports both JSON (with URL) and multipart/form-data (file upload). Thanks @Eduardo-gato (#15)
- **Webhook routing for GROUP / NEWSLETTER** — when the primary `MESSAGE` / `SEND_MESSAGE` / `READ_RECEIPT` subscription is absent, events from `@g.us` chats are forwarded to `GROUP` subscribers and events from `@newsletter` chats to `NEWSLETTER` subscribers. Thanks @oismaelash (#18)

### 🔧 Improvements
- **Proxy protocol** — new optional `protocol` field (and `PROXY_PROTOCOL` env) supporting `http`, `https`, `socks5`. Replaces the hardcoded SOCKS5 dialer with `client.SetProxyAddress`, fixing HTTP-proxy QR pairing (#12). Thanks @TBDevMaster (#13)
- **WhatsApp Web version cache** — `fetchWhatsAppWebVersion` now caches the result for 1 hour with a mutex instead of issuing one request per instance startup. Thanks @VitorS0uza (#24)
- **Manager flicker fix** — instance page no longer replaces the list with skeleton cards on every 5s polling cycle (`hasLoaded` flag). Thanks @TBDevMaster (#14), closes #11
- **`WEBHOOKFILES` → `WEBHOOK_FILES`** — `.env.example`, docker-compose and docs aligned with the env var the runtime actually reads. Thanks @VitorS0uza (#22)
- **Dependency cleanup** — removed unused `github.com/evolution-foundation/evo-gate` from `go.mod`
- **whatsmeow-lib** bumped to `0923702fb`
- **Telemetry removed** — dropped legacy `pkg/telemetry`

### 🐛 Bug Fixes
- **`/message/edit`** — was silently ignored because the edit payload used `Conversation` while the original message was sent as `ExtendedTextMessage`. WhatsApp requires matching types; now the edit uses `ExtendedTextMessage` and the response returns the actual server timestamp instead of the zero value. Closes #16
- **Sticker upload to S3/MinIO** — when `webp.Decode` or `png.Encode` failed, the whole media pipeline aborted and the sticker was lost from the webhook. Now we log a warning and keep the raw `.webp` bytes so the sticker still reaches the bucket. Closes #5
- **Multipart `/send/media`** — the binary-upload branch silently dropped `mentionAll`, `mentionedJid` and `quoted`. These fields now parse from the form (with `mentionedJid` accepting repeated or comma-separated values) and reach the send service. Closes #2

### ⚠️ Breaking changes
- **Proxy** — previously all proxies were forced through SOCKS5. If you run SOCKS5 on a non-standard port (anything outside 1080/2080/42000-43000), set `PROXY_PROTOCOL=socks5` in the env or pass `"protocol": "socks5"` in the proxy body explicitly — otherwise the new protocol inference will fall back to HTTP.

### 📝 Docs
- **README** — updated WhatsApp support number and issue templates
- **Interactive messages guide** — new `docs/wiki/guias-api/api-interactive.md`
- **Proxy docs** — environment variables, configuration guide and API reference updated with the new `protocol` field

## v0.6.1

### 🆕 New Features
- **Group invite info endpoint** — `GET /group/invite-info` to get group details from invite link
- **Enhanced media sending** — GIF playback, video stickers, and transparent sticker support

### 🐛 Bug Fixes
- **Admin revoke** — Allow deleting messages from others in groups (admin revoke)

### 🔧 Improvements
- **Version management** — Reads version from `VERSION` file with ldflags fallback
- **CORS global middleware** — Applied before all routes
- **Makefile compatibility** — Fixed `$(shell)` syntax for GNU Make 3.81 (macOS default)
- **CI/CD cleanup** — Removed `develop` branch trigger and `homolog` tag from Docker workflow
- **README updated** — New links, documentation, and hosting info

## v0.6.0

### 🆕 New Features
- **Version from VERSION file** — Reads version from `VERSION` file at startup instead of hardcoded value

### 🔧 Improvements
- **Makefile compatibility** — Fixed `$(shell)` syntax for GNU Make 3.81 (macOS default)

## v0.5.4

### 🔧 Improvements
- **Update whatsmeow lib**

## v0.5.3

**Docker:** `evoapicloud/evolution-go:0.5.3`

### 🔧 Improvements

- **Update context handling in service methods** 
  - Refactored multiple service methods across various packages to include `context.Background()` as the first argument in client calls. This change ensures that all client interactions are properly context-aware, allowing for better cancellation and timeout management.
  - Updated methods in `call_service.go`, `community_service.go`, `group_service.go`, `message_service.go`, `newsletter_service.go`, `send_service.go`, `user_service.go`, and `whatsmeow.go` to enhance consistency and reliability in handling requests.
  - This adjustment improves the overall robustness of the API by ensuring that all client calls can leverage context for better control over execution flow and resource management.

## v0.5.2

**Docker:** `evoapicloud/evolution-go:0.5.2`

### 🆕 New Features
- **SetProxy Endpoint**: New endpoint `POST /instance/proxy/{instanceId}` to configure proxy for instances
  - Support for proxy with/without authentication
  - Validation of required fields (host, port)
  - Automatic cache update via reconnection
  - Integrated Swagger documentation

### 🔧 Improvements
- **CheckUser Fallback Logic**: Implemented intelligent fallback logic
  - If `formatJid=true` returns `IsInWhatsapp=false`, automatically retries with `formatJid=false`
  - Significant improvement in valid user detection
  - Added `RemoteJID` field to use WhatsApp-validated JID
- **LID/WhatsApp JID Swap**: Automatic handling of special cases
  - When `Sender` comes as `@lid` and `SenderAlt` comes as `@s.whatsapp.net`
  - Automatic inversion: `Sender` and `Chat` receive `@s.whatsapp.net`, `SenderAlt` receives `@lid`
  - Detailed logs for tracking swaps

### 🐛 Bug Fixes
- **SendMessage**: Standardization of WhatsApp-validated `remoteJID` usage
- **User Validation**: Improvement in phone number validation and formatting

---

## v0.5.1

**Docker:** `evoapicloud/evolution-go:0.5.1`

### 🔧 Improvements
- **Instance Deletion**: Enhance instance deletion and media storage path resolution
- **Media Storage**: Improvements in media storage and path resolution

---

## v0.5.0

**Docker:** `evoapicloud/evolution-go:0.5.0`

### 🔧 Improvements
- **Media Storage**: Enhance media storage and logging in Whatsmeow event handling
- **Retry Logic**: Implement retry logic for client connection and message sending
- **Media Handling**: Enhance media handling in event processing

---

## v0.4.9

**Docker:** `evoapicloud/evolution-go:0.4.9`

### 🔧 Improvements
- **Connection Handling**: Add instance update test scenarios and improve connection handling
- **FormatJid Field**: Update FormatJid field to pointer type for better handling in message structures
- **Dependencies**: Update dependencies and fix presence handling in Whatsmeow integration

---

## v0.4.8

**Docker:** `evoapicloud/evolution-go:0.4.8`

### 🔧 Improvements
- **Audio Duration**: Improve audio duration parsing in convertAudioToOpusWithDuration function

---

## v0.4.7

**Docker:** `evoapicloud/evolution-go:0.4.7`

### 🔧 Improvements
- **Phone Number Formatting**: Improve phone number formatting and validation in user service
- **Brazilian/Portuguese Numbers**: Update Brazilian and Portuguese number formatting in utils

### 🆕 New Features
- **Media Handling**: Enhance media handling in event processing

---

## v0.4.6

**Docker:** `evoapicloud/evolution-go:0.4.6`

### 🆕 New Features
- **User Existence Check**: Add user existence check configuration and JID validation middleware

---

## v0.4.5

**Docker:** `evoapicloud/evolution-go:0.4.5`

### 🔧 Improvements
- **Dependencies**: Update dependencies and enhance audio conversion functionality

---

## v0.4.4

**Docker:** `evoapicloud/evolution-go:0.4.4`

### 🆕 New Features
- **CLAUDE.md**: Add CLAUDE.md for project documentation and enhance RabbitMQ connection handling

---

## v0.4.3

**Docker:** `evoapicloud/evolution-go:0.4.3`

### 🔧 Improvements
- **PostgreSQL Connection**: Fix in PostgreSQL connection configuration for session auth
  - Controlled configuration of pool, idle, etc.
  - Adjustment on top of whatsmeow lib
- **User Endpoints**: Fix in 'User Info' and 'Check User' endpoints
  - Now return with contact's LID information

---

## v0.3.0

### 🆕 New Features
- **Own Message Reactions**: Additional 'fromMe' parameter using Chat id
- **CreatedAt Field**: CreatedAt field added to instances table

---

## v0.2.0

### 🆕 New Features
- **Advanced Settings**: Advanced configurations in instance creation
  - `alwaysOnline` (still to be implemented)
  - `rejectCall` - Automatically reject calls
  - `msgRejectCall` - Call rejection message
  - `readMessages` - Automatically mark messages as read
  - `ignoreGroups` - Ignore group messages
  - `ignoreStatus` - Ignore status messages
- **Advanced Settings Routes**: New routes for get and update of advanced settings
- **QR Code Control**: `QRCODE_MAX_COUNT` variable to control how many QR codes to generate before timeout
- **AMQP Events**: `AMQP_SPECIFIC_EVENTS` variable to individually select which events to receive in RabbitMQ

### 🔧 Improvements
- **Reconnect Endpoint**: Fix in reconnect endpoint
- **Sender Info**: `Sender` and `SenderAlt` no longer come with session id, only the id

### 🐛 Bug Fixes
- **QR Code Generation**: Fix to not generate QR code automatically after disconnection or logout

---

## v0.1.0

### 🆕 Initial Features
- Base implementation of Evolution API in Go
- WhatsApp integration via whatsmeow
- Instance system
- Basic message sending endpoints
- Webhook support
- RabbitMQ and NATS integration
- Authentication system
- Swagger documentation

---

## 📋 Migration Notes

### v0.5.2
- The new `SetProxy` endpoint requires admin permissions (`AuthAdmin`)
- The `CheckUser` fallback logic is automatic and transparent
- LID/WhatsApp JID handling is automatic

### v0.4.3
- Check PostgreSQL connection settings if using postgres auth

### v0.2.0
- Review advanced settings configurations if necessary
- Configure `QRCODE_MAX_COUNT` if you want to limit QR codes
- Configure `AMQP_SPECIFIC_EVENTS` for specific RabbitMQ events

---

## 🔗 Useful Links

- **Docker Hub**: `evoapicloud/evolution-go`
- **Documentation**: Swagger available at `/swagger/`
- **GitHub**: [Evolution API Go](https://github.com/evolution-foundation/evolution-go)

---

## 🤝 Contributing

To contribute to the project:
1. Fork the repository
2. Create a branch for your feature
3. Commit your changes
4. Open a Pull Request

---

*Last updated: October 2025*
