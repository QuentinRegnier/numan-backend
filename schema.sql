/* =============================================================================
   NUMAN — POSTGRESQL L3 SCHEMA  (refactor v2, bootstrap / fresh-install form)
   =============================================================================
   Target        : PostgreSQL >= 14 (validated on 16.13). No extensions required.
   Role of file  : creates a NEW database. It is NOT an in-place production
                   upgrade script — see review.md §12 for the migration plan.
   Design axis   : the Go workers/repositories are frozen, so the schema is shaped
                   to the contracts they ACTUALLY exercise (see review.md §2/§10).

   KEY DECISIONS (details + evidence in review.md)
     D1  Epoch-millisecond columns are BIGINT, not timestamptz, for every column
         the workers write from an int64 payload field (pq.CopyIn sends int64 as
         decimal text; PostgreSQL rejects '1717600000000' for timestamptz).
         comments/likes keep timestamptz because their payloads carry RFC3339
         strings. Functions that Go scans into time.Time convert on output.
     D2  "Not deleted" is `visibility >= 0` for posts (-1 = soft delete). The old
         `visibility != 2` filter hid friends-only posts (2 = friends) and let the
         orphan GC delete their likes/saves.
     D3  Read functions keep the exact column lists the repositories scan; where
         the Go scan can never succeed (column-count mismatch) a *_json function
         returning the exact Go payload is provided instead.
     D4  Explicit lookup functions replace catch-all loaders; thin legacy shims
         remain ONLY for catch-alls that Go still calls (SECTION 9).
     D5  Keyset pagination functions are added next to the legacy OFFSET ones.
   ============================================================================= */

SET statement_timeout                   = 0;
SET lock_timeout                        = 0;
SET idle_in_transaction_session_timeout = 0;
SET client_encoding                     = 'UTF8';
SET standard_conforming_strings         = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies               = on;      -- surface SQL-function errors at load time
SET xmloption                           = content;
SET client_min_messages                 = warning;
SET row_security                        = off;

/* ---------------------------------------------------------------------------
   SECTION 1 — SCHEMAS
   --------------------------------------------------------------------------- */
CREATE SCHEMA auth;        -- identity, sessions, social graph
CREATE SCHEMA content;     -- posts, comments, media, likes, saved, tags
CREATE SCHEMA messaging;   -- conversations, members, messages, reactions
CREATE SCHEMA moderation;  -- reports, legal holds
CREATE SCHEMA views;       -- read-optimised projections + generic helpers
CREATE SCHEMA util;        -- time helpers shared by all domains

SET default_tablespace          = '';
SET default_table_access_method = heap;

/* ---------------------------------------------------------------------------
   SECTION 2 — UTIL (time helpers)
   Epoch-ms is the application's native time unit (domain.TimeToMillis).
   --------------------------------------------------------------------------- */
-- Transaction-stable "now" in epoch ms (STABLE so it can be a column DEFAULT and
-- is constant inside one statement, which keeps GC cut-offs consistent).
CREATE FUNCTION util.now_ms() RETURNS bigint
LANGUAGE sql STABLE PARALLEL SAFE
AS $$ SELECT (extract(epoch FROM pg_catalog.now()) * 1000)::bigint $$;

-- epoch-ms -> timestamptz (for Go scans into time.Time). NULL-safe.
CREATE FUNCTION util.ms_to_ts(p_ms bigint) RETURNS timestamptz
LANGUAGE sql IMMUTABLE PARALLEL SAFE
AS $$ SELECT pg_catalog.to_timestamp(p_ms / 1000.0) $$;

-- timestamptz -> epoch-ms (backs the optional ASSIGNMENT cast in SECTION 9b).
CREATE FUNCTION util.ts_to_ms(p_ts timestamptz) RETURNS bigint
LANGUAGE sql IMMUTABLE PARALLEL SAFE
AS $$ SELECT (extract(epoch FROM p_ts) * 1000)::bigint $$;

-- epoch-ms -> RFC3339 UTC text (matches the string payloads of comments/likes).
CREATE FUNCTION util.ms_to_rfc3339(p_ms bigint) RETURNS text
LANGUAGE sql STABLE PARALLEL SAFE
AS $$ SELECT pg_catalog.to_char(pg_catalog.timezone('UTC', pg_catalog.to_timestamp(p_ms / 1000.0)),
                                'YYYY-MM-DD"T"HH24:MI:SS.US"Z"') $$;

-- timestamptz -> RFC3339 UTC text.
CREATE FUNCTION util.ts_to_rfc3339(p_ts timestamptz) RETURNS text
LANGUAGE sql STABLE PARALLEL SAFE
AS $$ SELECT pg_catalog.to_char(pg_catalog.timezone('UTC', p_ts), 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"') $$;

/* ---------------------------------------------------------------------------
   SECTION 3 — TABLES  (constraints/indexes follow in SECTIONS 4-5)
   NOT NULL/DEFAULT additions are limited to columns every writer always sets,
   so Go `Scan` into non-pointer types can never meet a NULL (see review §2).
   --------------------------------------------------------------------------- */

/* auth.users — central identity. birthdate/ban_expires_at/created_at/updated_at are
   epoch-ms (D1). profile_picture_id is a soft ref to content.media (cycle avoidance). */
CREATE TABLE auth.users (
    id                  bigint   NOT NULL,
    username            text     NOT NULL,
    email               text     NOT NULL,
    email_verified      boolean  NOT NULL DEFAULT false,
    phone               text,
    phone_verified      boolean  NOT NULL DEFAULT false,
    password_hash       text     NOT NULL,
    two_factor_enabled  boolean  NOT NULL DEFAULT false,
    two_factor_method   smallint NOT NULL DEFAULT 0, -- 0: Aucun, 1: Email, 2: TOTP, 3: Passkey
    totp_secret         text,
    first_name          text     NOT NULL,
    last_name           text     NOT NULL,
    birthdate           bigint,                       -- epoch ms (UTC midnight), NULL = unknown
    sex                 smallint NOT NULL DEFAULT 0,  -- 0 unspecified, 1 male, 2 female
    bio                 text,
    profile_picture_id  bigint,
    grade               smallint NOT NULL,
    location            text,
    school              text,
    work                text,
    badges              jsonb    NOT NULL DEFAULT '{}'::jsonb,
    desactivated        boolean          NOT NULL DEFAULT false,
    banned              boolean          NOT NULL DEFAULT false,
    ban_reason          text,
    ban_expires_at      bigint,                       -- epoch ms
    trust_score         double precision NOT NULL DEFAULT 0.20,
    last_infraction_at  bigint,                       -- epoch ms (utilisé par le cron mensuel +0.02)
    created_at          bigint           NOT NULL DEFAULT util.now_ms(),
    updated_at          bigint           NOT NULL DEFAULT util.now_ms(),
    CONSTRAINT users_trust_score_range CHECK (trust_score >= 0.00 AND trust_score <= 1.00)
);

/* auth.relations — directed social edge. state: 0 none, 1 follower, 2 friend, -1 blocked. */
CREATE TABLE auth.relations (
    id            bigint   NOT NULL,
    primary_id    bigint   NOT NULL,   -- initiator (follower / blocker)
    secondary_id  bigint   NOT NULL,   -- target
    state         smallint NOT NULL DEFAULT 0,
    created_at    bigint   NOT NULL DEFAULT util.now_ms(),
    updated_at    bigint   NOT NULL DEFAULT util.now_ms()
);

/* auth.user_settings — 1:1 with users; JSONB blobs allow additive evolution. */
CREATE TABLE auth.user_settings (
    id                  bigint NOT NULL,
    user_id             bigint NOT NULL,
    privacy             jsonb  NOT NULL DEFAULT '{}'::jsonb,
    notifications       jsonb  NOT NULL DEFAULT '{}'::jsonb,
    display_and_content jsonb  NOT NULL DEFAULT '{}'::jsonb,
    telemetry_vector    real[],
    telemetry_tags      text[],
    telemetry_timestamp bigint,            -- client vector version (ms); NULL until first sync
    created_at          bigint NOT NULL DEFAULT util.now_ms(),
    updated_at          bigint NOT NULL DEFAULT util.now_ms()
);

/* auth.sessions — one row per (user, device). Secrets rotate on refresh. */
CREATE TABLE auth.sessions (
    id                        bigint NOT NULL,
    user_id                   bigint NOT NULL,
    master_token              text   NOT NULL,
    firebase_installation_id  text   NOT NULL,
    device_info               jsonb,
    ip_history                inet[],
    current_secret            text,
    last_secret               text,
    last_jwt                  text,
    tolerance_time            bigint,      -- epoch ms
    created_at                bigint NOT NULL DEFAULT util.now_ms(),
    expires_at                bigint NOT NULL
);

/* auth.user_passkeys — one row per (id, credential_id, user_id). hardware keys FIDO2 / WebAuthn. */
CREATE TABLE auth.user_passkeys (
    id               bigint   NOT NULL,
    user_id          bigint   NOT NULL,
    credential_id    text     NOT NULL,
    public_key       bytea    NOT NULL,
    attestation_type text     NOT NULL DEFAULT 'none'::text,
    aaguid           bytea,
    sign_count       bigint   NOT NULL DEFAULT 0,
    device_name      text     NOT NULL DEFAULT ''::text,
    transports       text[],
    created_at       bigint   NOT NULL DEFAULT util.now_ms(),
    updated_at       bigint   NOT NULL DEFAULT util.now_ms(),
    CONSTRAINT user_passkeys_pkey PRIMARY KEY (id),
    CONSTRAINT user_passkeys_credential_id_key UNIQUE (credential_id),
    CONSTRAINT user_passkeys_user_id_fkey FOREIGN KEY (user_id) REFERENCES auth.users(id) ON DELETE CASCADE
);

/* content.posts — hot table. Visibility: 0 public, 1 followers, 2 friends, -1 soft-deleted.
   Counters are denormalised and maintained ATOMICALLY by the workers through
   content.func_increment_* (single owner of the state; no triggers). */
CREATE TABLE content.posts (
    id                  bigint   NOT NULL,
    user_id             bigint   NOT NULL,
    content             text,
    hashtags            text[],
    indirect_hashtags   text[],
    identifiers         bigint[],
    media_ids           bigint[],
    visibility          smallint NOT NULL DEFAULT 0,
    priority_level      smallint NOT NULL DEFAULT 0,
    location            text,
    like_count          integer  NOT NULL DEFAULT 0,
    comment_count       integer  NOT NULL DEFAULT 0,
    view_count          integer  NOT NULL DEFAULT 0,
    report_count        integer  NOT NULL DEFAULT 0,
    has_media           boolean  NOT NULL DEFAULT false,
    vector              real[],
    vector_version      integer  NOT NULL DEFAULT 1,
    telemetry_dwell_sum double precision NOT NULL DEFAULT 0.0,
    telemetry_dwell_sq  double precision NOT NULL DEFAULT 0.0,
    telemetry_clicks    integer  NOT NULL DEFAULT 0,
    created_at          bigint   NOT NULL DEFAULT util.now_ms(),
    updated_at          bigint   NOT NULL DEFAULT util.now_ms()
) WITH (fillfactor = 85, autovacuum_vacuum_scale_factor = 0.02, autovacuum_analyze_scale_factor = 0.02);

/* content.comments — payload carries RFC3339 strings => timestamptz is kept (D1).
   visibility 0 visible, -1 soft-deleted. score mirrors like_count (worker keeps both). */
CREATE TABLE content.comments (
    id          bigint  NOT NULL,
    post_id     bigint  NOT NULL,
    user_id     bigint  NOT NULL,
    content     text    NOT NULL,
    visibility  integer NOT NULL DEFAULT 0,
    like_count  integer NOT NULL DEFAULT 0,
    score       integer NOT NULL DEFAULT 0,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
) WITH (fillfactor = 85, autovacuum_vacuum_scale_factor = 0.05);

/* content.media — registry only (files live in S3/MinIO).
   visibility states: -2 rejected/moderated, -1 soft-deleted, 0 pending upload, 1 public/active, 2 quarantine (under review). */
CREATE TABLE content.media (
   id            bigint           NOT NULL,
   owner_id      bigint,
   storage_path  text,
   visibility    smallint         NOT NULL DEFAULT 0,
   ia_score      double precision NOT NULL DEFAULT 0.0,
   created_at    bigint           NOT NULL DEFAULT util.now_ms(),
   updated_at    bigint           NOT NULL DEFAULT util.now_ms(),
   CONSTRAINT media_ia_score_range CHECK (ia_score >= 0.00 AND ia_score <= 1.00)
);

/* content.likes — polymorphic: target_type 0 = post, 1 = comment (matches the
   workers and the like GC; the old "2 = comment" comment was stale). */
CREATE TABLE content.likes (
    id           bigint   NOT NULL,
    target_type  smallint NOT NULL,
    target_id    bigint   NOT NULL,
    user_id      bigint   NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now()
);

/* content.saved — bookmarks. */
CREATE TABLE content.saved (
    id         bigint NOT NULL,
    user_id    bigint NOT NULL,
    post_id    bigint NOT NULL,
    created_at bigint NOT NULL DEFAULT util.now_ms()
);

/* content.tags — canonical hashtag registry (worker inserts (slug,is_community) ON CONFLICT DO NOTHING). */
CREATE TABLE content.tags (
    slug         text PRIMARY KEY,
    created_at   timestamptz DEFAULT CURRENT_TIMESTAMP,
    is_community boolean     DEFAULT true
);

/* messaging.conversations — type 0 DM, 1 group, 2 private community, 3 public community. state 0 active, 1 archived. */
CREATE TABLE messaging.conversations (
    id              bigint   NOT NULL,
    type            smallint NOT NULL,
    title           text,
    description     text,
    avatar_id       bigint,
    last_message_id bigint,
    state           smallint NOT NULL DEFAULT 0,
    settings        jsonb    NOT NULL DEFAULT '{}'::jsonb,
    external_link   jsonb    NOT NULL DEFAULT '{}'::jsonb,
    created_at      bigint   NOT NULL DEFAULT util.now_ms(),
    updated_at      bigint   NOT NULL DEFAULT util.now_ms()
);

/* messaging.messages — hyper-growth table (see review §9 for the partitioning decision).
   message_type: 0 text 1 vocal 2 image 3 gif 4 video 5 publication 6 invite 7 link 8 system 9 survey 10 widget.
   visibility=false is a soft delete. attachments may be NULL (worker writes NULL for empty). */
CREATE TABLE messaging.messages (
    id                  bigint   NOT NULL,
    conversation_id     bigint   NOT NULL,
    sender_id           bigint   NOT NULL,
    message_type        smallint NOT NULL,
    visibility          boolean  NOT NULL DEFAULT true,
    content             text,
    attachments         jsonb    DEFAULT '{}'::jsonb,
    allow_new_reactions boolean  NOT NULL DEFAULT true,
    created_at          bigint   NOT NULL DEFAULT util.now_ms(),
    updated_at          bigint   NOT NULL DEFAULT util.now_ms()
);

/* messaging.message_reactions — one reaction per (message,user); worker UPSERTs on that pair. */
CREATE TABLE messaging.message_reactions (
    id         bigint NOT NULL,
    message_id bigint NOT NULL,
    user_id    bigint NOT NULL,
    reaction   text   NOT NULL,
    created_at bigint NOT NULL DEFAULT util.now_ms()
);

/* messaging.members — role: -2 banned, -1 left, 0 member, 1 admin, 2 owner.
   frozen_message_id = history ceiling for banned members; last_read_message_id = read watermark. */
CREATE TABLE messaging.members (
    id                   bigint   NOT NULL,
    conversation_id      bigint   NOT NULL,
    user_id              bigint   NOT NULL,
    role                 smallint NOT NULL DEFAULT 0,
    settings             jsonb    NOT NULL DEFAULT '{}'::jsonb,
    joined_at            bigint   NOT NULL DEFAULT util.now_ms(),
    unread_count         integer  NOT NULL DEFAULT 0,
    frozen_message_id    bigint,
    last_read_message_id bigint,
    created_at           bigint   NOT NULL DEFAULT util.now_ms(),
    updated_at           bigint   NOT NULL DEFAULT util.now_ms()
) WITH (fillfactor = 85, autovacuum_vacuum_scale_factor = 0.02);

/* moderation.reports — state 0 pending, 1 in progress, -1 closed.
   report_origin: 0 = USER (signalement humain), 1 = AUTO_AI (signalement robot serveur), 2 = HYBRID (Auto-IA + Humain). */
CREATE TABLE moderation.reports (
    id                       bigint           NOT NULL,
    reporter_id              bigint,
    report_origin            smallint         NOT NULL DEFAULT 0,
    target_type              smallint         NOT NULL,
    target_ids               bigint[]         NOT NULL,
    target_owner_id          bigint           NOT NULL DEFAULT 0,
    author_trust_score       double precision NOT NULL DEFAULT 0.20,
    ia_score_max             double precision NOT NULL DEFAULT 0.00,
    accumulated_report_score double precision NOT NULL DEFAULT 0.00,
    report_count             integer          NOT NULL DEFAULT 1,
    category                 smallint         NOT NULL,
    reason                   text,
    rationale                text,
    state                    smallint         NOT NULL DEFAULT 0,
    importance               double precision NOT NULL DEFAULT 0.0,
    created_at               bigint           NOT NULL DEFAULT util.now_ms(),
    updated_at               bigint           NOT NULL DEFAULT util.now_ms()
);

/* moderation.posts_moderation — Table d'indexation rapide par colonne générée STORED. */
CREATE TABLE moderation.posts_moderation (
     target_id                bigint        NOT NULL,
     target_type              smallint      NOT NULL DEFAULT 0, -- 0: Post, 3: Message
     owner_id                 bigint        NOT NULL DEFAULT 0,
     author_trust_score       numeric(4, 3) NOT NULL DEFAULT 0.200,
     report_origin            smallint      NOT NULL DEFAULT 0, -- 0: USER, 1: AUTO_AI, 2: HYBRID
     ia_score_max             numeric(4, 3) NOT NULL DEFAULT 0.000,
     accumulated_report_score numeric(8, 3) NOT NULL DEFAULT 0.000,
     report_count             integer       NOT NULL DEFAULT 1,
     priority_score           numeric(8, 3) GENERATED ALWAYS AS (
         ROUND(
                 ((1.0 + 0.50 * (1.0 - author_trust_score)) / (1.0 + 1.50 * author_trust_score * author_trust_score))
                     * ((ia_score_max * 2.0) + accumulated_report_score),
                 3
         )
         ) STORED,
     status                   varchar(20)   NOT NULL DEFAULT 'pending',
     created_at               bigint        NOT NULL DEFAULT util.now_ms(),
     updated_at               bigint        NOT NULL DEFAULT util.now_ms(),
     CONSTRAINT posts_moderation_pkey PRIMARY KEY (target_type, target_id)
);

/* moderation.stegano_bans — registre permanent des identifiants stéganographiques bannis (infraction majeure). */
CREATE TABLE moderation.stegano_bans (
    user_id        bigint NOT NULL,
    stegano_Basic  text   NOT NULL,
    reason         text   NOT NULL,
    source_post_id bigint,
    created_at     bigint NOT NULL DEFAULT util.now_ms(),
    CONSTRAINT stegano_bans_pkey PRIMARY KEY (user_id)
);
/* =============================================================================
   SECTION 4 — PRIMARY KEYS, UNIQUE & FOREIGN KEYS  (unchanged guarantees vs v1)
   Every UNIQUE the workers rely on for ON CONFLICT / idempotency is preserved:
     message_reactions(message_id,user_id) · likes(target_type,target_id,user_id)
     saved(user_id,post_id) · members(conversation_id,user_id) · relations(secondary_id,primary_id)
   ============================================================================= */
-- auth
ALTER TABLE ONLY auth.users          ADD CONSTRAINT users_pkey PRIMARY KEY (id);
ALTER TABLE ONLY auth.users          ADD CONSTRAINT users_username_key UNIQUE (username);
ALTER TABLE ONLY auth.users          ADD CONSTRAINT users_email_key    UNIQUE (email);
ALTER TABLE ONLY auth.users          ADD CONSTRAINT users_phone_key    UNIQUE (phone);
ALTER TABLE ONLY auth.relations      ADD CONSTRAINT relations_pkey PRIMARY KEY (id);
ALTER TABLE ONLY auth.relations      ADD CONSTRAINT relations_secondary_id_primary_id_key UNIQUE (secondary_id, primary_id);
ALTER TABLE ONLY auth.relations      ADD CONSTRAINT relations_primary_id_fkey   FOREIGN KEY (primary_id)   REFERENCES auth.users(id);
ALTER TABLE ONLY auth.relations      ADD CONSTRAINT relations_secondary_id_fkey FOREIGN KEY (secondary_id) REFERENCES auth.users(id);
ALTER TABLE ONLY auth.user_settings  ADD CONSTRAINT user_settings_pkey PRIMARY KEY (id);
ALTER TABLE ONLY auth.user_settings  ADD CONSTRAINT user_settings_user_id_key UNIQUE (user_id);
ALTER TABLE ONLY auth.user_settings  ADD CONSTRAINT user_settings_user_id_fkey FOREIGN KEY (user_id) REFERENCES auth.users(id) ON DELETE CASCADE;
ALTER TABLE ONLY auth.sessions       ADD CONSTRAINT sessions_pkey PRIMARY KEY (id);
ALTER TABLE ONLY auth.sessions       ADD CONSTRAINT sessions_user_id_fkey FOREIGN KEY (user_id) REFERENCES auth.users(id);
-- content
ALTER TABLE ONLY content.posts       ADD CONSTRAINT posts_pkey PRIMARY KEY (id);
ALTER TABLE ONLY content.posts       ADD CONSTRAINT posts_user_id_fkey FOREIGN KEY (user_id) REFERENCES auth.users(id);
ALTER TABLE ONLY content.comments    ADD CONSTRAINT comments_pkey PRIMARY KEY (id);
ALTER TABLE ONLY content.comments    ADD CONSTRAINT comments_post_id_fkey FOREIGN KEY (post_id) REFERENCES content.posts(id) ON DELETE CASCADE;
ALTER TABLE ONLY content.comments    ADD CONSTRAINT comments_user_id_fkey FOREIGN KEY (user_id) REFERENCES auth.users(id);
ALTER TABLE ONLY content.media       ADD CONSTRAINT media_pkey PRIMARY KEY (id);
ALTER TABLE ONLY content.media       ADD CONSTRAINT media_owner_id_fkey FOREIGN KEY (owner_id) REFERENCES auth.users(id);
ALTER TABLE ONLY content.likes       ADD CONSTRAINT likes_pkey PRIMARY KEY (id);
ALTER TABLE ONLY content.likes       ADD CONSTRAINT likes_target_type_target_id_user_id_key UNIQUE (target_type, target_id, user_id);
ALTER TABLE ONLY content.likes       ADD CONSTRAINT likes_user_id_fkey FOREIGN KEY (user_id) REFERENCES auth.users(id);
ALTER TABLE ONLY content.saved       ADD CONSTRAINT saved_pkey PRIMARY KEY (id);
ALTER TABLE ONLY content.saved       ADD CONSTRAINT saved_user_id_post_id_key UNIQUE (user_id, post_id);
ALTER TABLE ONLY content.saved       ADD CONSTRAINT saved_user_id_fkey FOREIGN KEY (user_id) REFERENCES auth.users(id) ON DELETE CASCADE;
ALTER TABLE ONLY content.saved       ADD CONSTRAINT saved_post_id_fkey FOREIGN KEY (post_id) REFERENCES content.posts(id) ON DELETE CASCADE;
-- messaging
ALTER TABLE ONLY messaging.conversations ADD CONSTRAINT conversations_pkey PRIMARY KEY (id);
ALTER TABLE ONLY messaging.conversations ADD CONSTRAINT conversations_last_message_id_key UNIQUE (last_message_id);
ALTER TABLE ONLY messaging.messages  ADD CONSTRAINT messages_pkey PRIMARY KEY (id);
ALTER TABLE ONLY messaging.messages  ADD CONSTRAINT messages_conversation_id_fkey FOREIGN KEY (conversation_id) REFERENCES messaging.conversations(id);
ALTER TABLE ONLY messaging.message_reactions ADD CONSTRAINT message_reactions_pkey PRIMARY KEY (id);
ALTER TABLE ONLY messaging.message_reactions ADD CONSTRAINT message_reactions_message_id_user_id_key UNIQUE (message_id, user_id);
ALTER TABLE ONLY messaging.message_reactions ADD CONSTRAINT message_reactions_message_id_fkey FOREIGN KEY (message_id) REFERENCES messaging.messages(id) ON DELETE CASCADE;
ALTER TABLE ONLY messaging.message_reactions ADD CONSTRAINT message_reactions_user_id_fkey FOREIGN KEY (user_id) REFERENCES auth.users(id);
ALTER TABLE ONLY messaging.members   ADD CONSTRAINT members_pkey PRIMARY KEY (id);
ALTER TABLE ONLY messaging.members   ADD CONSTRAINT members_conversation_id_user_id_key UNIQUE (conversation_id, user_id);
ALTER TABLE ONLY messaging.members   ADD CONSTRAINT members_conversation_id_fkey FOREIGN KEY (conversation_id) REFERENCES messaging.conversations(id);
ALTER TABLE ONLY messaging.members   ADD CONSTRAINT members_user_id_fkey FOREIGN KEY (user_id) REFERENCES auth.users(id);
-- moderation (legal_holds FK needs posts_pkey, hence declared here — v1 declared it inline and failed to load)
ALTER TABLE ONLY moderation.reports      ADD CONSTRAINT reports_pkey PRIMARY KEY (id);
ALTER TABLE ONLY moderation.reports      ADD CONSTRAINT reports_reporter_id_fkey FOREIGN KEY (reporter_id) REFERENCES auth.users(id);
ALTER TABLE ONLY moderation.legal_holds  ADD CONSTRAINT legal_holds_pkey PRIMARY KEY (post_id);
ALTER TABLE ONLY moderation.legal_holds  ADD CONSTRAINT fk_legal_holds_post FOREIGN KEY (post_id) REFERENCES content.posts(id) ON DELETE CASCADE;

/* =============================================================================
   SECTION 5 — INDEXES
   Each index names the access path it serves. Removed v1 indexes were exact
   duplicates (same leading columns as a UNIQUE/PK or as a new composite).
   ============================================================================= */
-- ---- auth -------------------------------------------------------------------
-- login by email: the lookup is case/space-insensitive, so the index must be on the same expression.
CREATE INDEX idx_users_email_norm ON auth.users (lower(btrim(email)));
-- badge containment (kept from v1; no SQL consumer in the supplied sources -> review §7 flags it for pg_stat check)
CREATE INDEX idx_users_badges ON auth.users USING gin (badges);
-- followers/friends/blocked lists: equality(user,state) + created_at DESC order + covering counterpart id.
CREATE INDEX idx_relations_out ON auth.relations (primary_id,   state, created_at DESC, id DESC) INCLUDE (secondary_id);
CREATE INDEX idx_relations_in  ON auth.relations (secondary_id, state, created_at DESC, id DESC) INCLUDE (primary_id);
-- one session per (user, device)
CREATE UNIQUE INDEX idx_sessions_user_device ON auth.sessions (user_id, firebase_installation_id);
-- per-request auth lookup by device id alone (not covered by the composite above) and by master token (equality only -> hash)
CREATE INDEX idx_sessions_fid          ON auth.sessions (firebase_installation_id, created_at DESC);
CREATE INDEX idx_sessions_master_token ON auth.sessions USING hash (master_token);
--
CREATE INDEX idx_user_passkeys_user_id ON auth.user_passkeys USING btree (user_id, created_at DESC);

-- ---- content ----------------------------------------------------------------
-- profile timeline (keyset on (created_at,id)); live rows only.
CREATE INDEX idx_posts_user_live    ON content.posts (user_id, created_at DESC, id DESC) WHERE visibility >= 0;
-- time-decay windows, recent-posts, global keyset; live rows only.
CREATE INDEX idx_posts_live_created ON content.posts (created_at DESC, id DESC)          WHERE visibility >= 0;
-- hashtag lookups use `col @> ARRAY[tag]` (indexable); `tag = ANY(col)` was not.
CREATE INDEX idx_posts_hashtags          ON content.posts USING gin (hashtags)          WHERE visibility >= 0;
CREATE INDEX idx_posts_indirect_hashtags ON content.posts USING gin (indirect_hashtags) WHERE visibility >= 0;
CREATE INDEX idx_posts_identifiers       ON content.posts USING gin (identifiers);
-- media GC "is this media referenced by a post?" probes
CREATE INDEX idx_posts_media_ids         ON content.posts USING gin (media_ids);
-- full-text search (French config, as in v1 func_search_post_ids_by_text)
CREATE INDEX idx_posts_content_fts       ON content.posts USING gin (to_tsvector('french', content)) WHERE visibility >= 0;
-- GC drivers: soft-deleted posts only (tiny relative to the table)
CREATE INDEX idx_posts_deleted           ON content.posts (id) WHERE visibility < 0;

-- comments: ranking order (score DESC, created_at ASC) + id tie-break for keyset.
CREATE INDEX idx_comments_post_score ON content.comments (post_id, score DESC, created_at ASC, id ASC) WHERE visibility <> -1;
-- likes of an entity (newest first) / likes by a user
CREATE INDEX idx_likes_target_created ON content.likes (target_type, target_id, created_at DESC) INCLUDE (user_id);
CREATE INDEX idx_likes_user           ON content.likes (user_id, target_type, created_at DESC)   INCLUDE (target_id);
-- saved: newest-first pages + reverse lookup (also serves the FK cascade from posts)
CREATE INDEX idx_saved_user_created ON content.saved (user_id, created_at DESC, id DESC);
CREATE INDEX idx_saved_post         ON content.saved (post_id);
-- media: FK support + GC candidate scan (pending=0 or soft-deleted=-1/-2) + quarantine queue index (visibility=2)
CREATE INDEX idx_media_owner      ON content.media (owner_id);
CREATE INDEX idx_media_gc         ON content.media (updated_at) WHERE visibility IN (-2, -1, 0);
CREATE INDEX idx_media_quarantine ON content.media (created_at ASC, id ASC) WHERE visibility = 2;

-- ---- messaging --------------------------------------------------------------
CREATE INDEX idx_conversations_created      ON messaging.conversations (created_at DESC);
CREATE INDEX idx_conversations_communities  ON messaging.conversations (id) WHERE type = 3 AND state = 0;
-- history pagination is keyed on the Snowflake id (func_load_message_ids_paginated)
CREATE INDEX idx_messages_conv_id_live  ON messaging.messages (conversation_id, id DESC) WHERE visibility = true;
-- chronological access (unread recount, conversation_summary view)
CREATE INDEX idx_messages_conv_created  ON messaging.messages (conversation_id, created_at DESC);
-- media GC probe: only messages that carry attachments.media_id are indexed
CREATE INDEX idx_messages_media_ref     ON messaging.messages ((attachments -> 'media_id')) WHERE attachments ? 'media_id';
-- reaction GC driver
CREATE INDEX idx_messages_invisible     ON messaging.messages (id) WHERE visibility = false;
CREATE INDEX idx_members_user_conv        ON messaging.members (user_id, conversation_id);
CREATE INDEX idx_members_conv_role_joined ON messaging.members (conversation_id, role, joined_at DESC);
-- muted-member listing: only rows with an active/previous restriction are indexed
CREATE INDEX idx_members_restricted ON messaging.members (conversation_id, ((settings ->> 'restricted_until')::bigint) DESC)
    WHERE (settings ->> 'restricted_until')::bigint > 0;
CREATE INDEX idx_message_reactions_msg_created ON messaging.message_reactions (message_id, created_at DESC, id DESC);
CREATE INDEX idx_message_reactions_user_id     ON messaging.message_reactions (user_id);

-- ---- moderation -------------------------------------------------------------
CREATE INDEX idx_reports_reporter ON moderation.reports (reporter_id);
CREATE INDEX idx_reports_targets  ON moderation.reports USING gin (target_ids);
CREATE INDEX idx_reports_created  ON moderation.reports (created_at);
CREATE INDEX idx_reports_state    ON moderation.reports (state);
CREATE INDEX idx_posts_priority ON moderation.posts_moderation (priority_score DESC, created_at ASC) WHERE status = 'pending';
CREATE INDEX idx_reports_pending_priority ON moderation.reports (importance DESC, created_at ASC) WHERE state = 0;
/* =============================================================================
   SECTION 6 — ROW-SHAPE VIEWS (internal; single source of the column lists that
   the Go `Scan` calls depend on, so each shape is defined once)
   ============================================================================= */

/* content.posts_l3 — the 22-column shape of calls.go:scanPosts (ORDER IS THE CONTRACT).
   created_at/updated_at are epoch-ms int64 (PostPayload), content is never NULL (Go scans into string). */
CREATE VIEW content.posts_l3 AS
SELECT p.id, p.user_id, COALESCE(p.content, '')::text AS content,
       p.hashtags, p.indirect_hashtags, p.identifiers, p.media_ids,
       p.visibility, p.priority_level, p.location,
       p.created_at, p.updated_at,
       p.like_count, p.comment_count, p.view_count, p.report_count, p.has_media,
       p.vector, p.vector_version,
       p.telemetry_dwell_sum, p.telemetry_dwell_sq, p.telemetry_clicks
FROM content.posts p;

/* auth.users_l3 — the 24-column shape scanned by func_load_user.go.
   birthdate is RFC3339 text (Go parses it), the three timestamps are timestamptz (Go scans time.Time/NullTime). */
CREATE OR REPLACE VIEW auth.users_l3 AS
SELECT u.id,
       u.username,
       u.email,
       u.email_verified,
       COALESCE(u.phone, '')::text AS phone,
    u.phone_verified,
       u.password_hash,
       u.first_name,
       u.last_name,
       CASE
           WHEN (u.birthdate IS NULL) THEN NULL::text
           ELSE util.ms_to_rfc3339(u.birthdate)
           END AS birthdate,
       u.sex,
       u.bio,
       u.profile_picture_id,
       u.grade,
       u.location,
       u.school,
       u.work,
       u.badges,
       u.desactivated,
       u.banned,
       u.ban_reason,
       util.ms_to_ts(u.ban_expires_at) AS ban_expires_at,
       util.ms_to_ts(u.created_at)     AS created_at,
       util.ms_to_ts(u.updated_at)     AS updated_at,
       u.two_factor_enabled,
       u.two_factor_method,
       COALESCE(u.totp_secret, '')::text AS totp_secret,
       u.trust_score,
       COALESCE(u.last_infraction_at, 0) AS last_infraction_at
FROM auth.users u;

/* auth.sessions_l3 — the 8 non-secret columns scanned by func_load_session(s).go (secrets are never returned). */
CREATE VIEW auth.sessions_l3 AS
SELECT s.id, s.user_id, s.master_token, s.firebase_installation_id,
       s.device_info, s.ip_history, s.created_at, s.expires_at
FROM auth.sessions s;

/* =============================================================================
   SECTION 7 — FUNCTIONS, by domain
   Conventions: STABLE for reads, schema-qualified references only, LIMIT clamps on
   user-facing pages, keyset functions end in _keyset, jsonb readers end in _json.
   Sentinel 9223372036854775807 = "first page" for DESC keyset cursors.
   ============================================================================= */

/* ---------------------------------------------------------------------------
   7.1 AUTH
   --------------------------------------------------------------------------- */

/* Explicit user lookups — one predicate each, each backed by exactly one index. */
CREATE FUNCTION auth.func_get_user_by_id(p_id bigint) RETURNS SETOF auth.users_l3
LANGUAGE sql STABLE AS $$ SELECT * FROM auth.users_l3 WHERE id = p_id $$;

-- stored usernames are `alphanum` (validated at signup) => btrim(param) preserves v1's TRIM(both sides) semantics
CREATE FUNCTION auth.func_get_user_by_username(p_username text) RETURNS SETOF auth.users_l3
LANGUAGE sql STABLE AS $$ SELECT * FROM auth.users_l3 WHERE username = btrim(p_username) $$;

-- v1 semantics TRIM(LOWER(email)) = TRIM(LOWER(param)); same expression as idx_users_email_norm
CREATE FUNCTION auth.func_get_user_by_email(p_email text) RETURNS SETOF auth.users_l3
LANGUAGE sql STABLE AS $$ SELECT * FROM auth.users_l3 WHERE lower(btrim(email)) = lower(btrim(p_email)) $$;

-- stored phones are E.164 (validated) => equality on users_phone_key; resolved through the PK so the COALESCE in the view is not in the predicate
CREATE FUNCTION auth.func_get_user_by_phone(p_phone text) RETURNS SETOF auth.users_l3
LANGUAGE sql STABLE AS $$
    SELECT v.* FROM auth.users_l3 v WHERE v.id IN (SELECT u.id FROM auth.users u WHERE u.phone = btrim(p_phone))
$$;

/* Explicit session lookups. v1's "fast path" returned LIMIT 1 for a device id while ignoring every other
   filter, although the unique index is (user_id, firebase_installation_id) — i.e. a device shared by two
   accounts returned an arbitrary one. Each lookup below states exactly which key it uses. */
CREATE FUNCTION auth.func_get_session_by_id(p_id bigint) RETURNS SETOF auth.sessions_l3
LANGUAGE sql STABLE AS $$ SELECT * FROM auth.sessions_l3 WHERE id = p_id $$;

CREATE FUNCTION auth.func_get_session_by_user_device(p_user_id bigint, p_firebase_installation_id text)
RETURNS SETOF auth.sessions_l3
LANGUAGE sql STABLE AS $$
    SELECT * FROM auth.sessions_l3 WHERE user_id = p_user_id AND firebase_installation_id = p_firebase_installation_id
$$;

CREATE FUNCTION auth.func_get_session_by_master_token(p_master_token text) RETURNS SETOF auth.sessions_l3
LANGUAGE sql STABLE AS $$ SELECT * FROM auth.sessions_l3 WHERE master_token = p_master_token $$;

-- device id alone is NOT unique across users: newest session wins (deterministic, indexed by idx_sessions_fid)
CREATE FUNCTION auth.func_get_latest_session_by_device(p_firebase_installation_id text) RETURNS SETOF auth.sessions_l3
LANGUAGE sql STABLE AS $$
    SELECT * FROM auth.sessions_l3 WHERE firebase_installation_id = p_firebase_installation_id
    ORDER BY created_at DESC, id DESC LIMIT 1
$$;

CREATE FUNCTION auth.func_list_sessions_by_user(p_user_id bigint) RETURNS SETOF auth.sessions_l3
LANGUAGE sql STABLE AS $$ SELECT * FROM auth.sessions_l3 WHERE user_id = p_user_id ORDER BY created_at DESC, id DESC $$;

/* Sanitised session list for the client (no tokens). COALESCE(device_info) prevents the empty-bytes
   json.Unmarshal failure in func_load_sessions.go when device_info is NULL. */
CREATE FUNCTION auth.func_get_active_sessions_view(p_user_id bigint)
RETURNS TABLE(id bigint, device_info jsonb, ip_history inet[], created_at bigint, expires_at bigint)
LANGUAGE sql STABLE AS $$
    SELECT s.id, COALESCE(s.device_info, '{}'::jsonb), s.ip_history, s.created_at, s.expires_at
    FROM auth.sessions s WHERE s.user_id = p_user_id ORDER BY s.created_at DESC, s.id DESC
$$;

/* Settings. telemetry_timestamp is NULL until the first telemetry sync; Go scans it into int64, so NULL -> 0. */
CREATE FUNCTION auth.func_get_user_settings(p_user_id bigint)
RETURNS TABLE(id bigint, user_id bigint, privacy jsonb, notifications jsonb, display_and_content jsonb,
              telemetry_vector real[], telemetry_tags text[], telemetry_timestamp bigint)
LANGUAGE sql STABLE AS $$
    SELECT s.id, s.user_id, s.privacy, s.notifications, s.display_and_content,
           s.telemetry_vector, s.telemetry_tags, COALESCE(s.telemetry_timestamp, 0)
    FROM auth.user_settings s WHERE s.user_id = p_user_id
$$;

/* Relation state of the directed edge caller -> target; 0 when absent. */
CREATE FUNCTION auth.func_get_relation_state(p_caller_id bigint, p_target_id bigint) RETURNS integer
LANGUAGE sql STABLE AS $$
    SELECT COALESCE((SELECT r.state::integer FROM auth.relations r
                     WHERE r.primary_id = p_caller_id AND r.secondary_id = p_target_id), 0)
$$;

/* Followers/friends (incoming) or blocked/following (outgoing), newest first. Branches (not UNION) so each
   one can stream an ordered index scan (idx_relations_in / idx_relations_out) and stop at LIMIT. */
CREATE FUNCTION auth.func_load_relations_paginated_by_direction(
    p_user_id bigint, p_state smallint, p_direction text, p_limit integer, p_offset integer)
RETURNS TABLE(matched_id bigint)
LANGUAGE plpgsql STABLE AS $$
BEGIN
    IF p_direction = 'incoming' THEN
        RETURN QUERY SELECT r.primary_id FROM auth.relations r
                     WHERE r.secondary_id = p_user_id AND r.state = p_state
                     ORDER BY r.created_at DESC, r.id DESC
                     LIMIT LEAST(GREATEST(p_limit, 1), 200) OFFSET GREATEST(p_offset, 0);
    ELSE
        RETURN QUERY SELECT r.secondary_id FROM auth.relations r
                     WHERE r.primary_id = p_user_id AND r.state = p_state
                     ORDER BY r.created_at DESC, r.id DESC
                     LIMIT LEAST(GREATEST(p_limit, 1), 200) OFFSET GREATEST(p_offset, 0);
    END IF;
END;
$$;

/* Keyset variant. Cursor = (created_at, relation_id) of the LAST row of the previous page (NULLs = first page). */
CREATE FUNCTION auth.func_load_relations_keyset(
    p_user_id bigint, p_state smallint, p_direction text,
    p_before_created_at bigint DEFAULT NULL, p_before_id bigint DEFAULT NULL, p_limit integer DEFAULT 50)
RETURNS TABLE(matched_id bigint, relation_id bigint, created_at bigint)
LANGUAGE plpgsql STABLE AS $$
DECLARE
    c_ts bigint := COALESCE(p_before_created_at, 9223372036854775807);
    c_id bigint := COALESCE(p_before_id,         9223372036854775807);
    n    integer := LEAST(GREATEST(p_limit, 1), 100);
BEGIN
    IF p_direction = 'incoming' THEN
        RETURN QUERY SELECT r.primary_id, r.id, r.created_at FROM auth.relations r
                     WHERE r.secondary_id = p_user_id AND r.state = p_state AND (r.created_at, r.id) < (c_ts, c_id)
                     ORDER BY r.created_at DESC, r.id DESC LIMIT n;
    ELSE
        RETURN QUERY SELECT r.secondary_id, r.id, r.created_at FROM auth.relations r
                     WHERE r.primary_id = p_user_id AND r.state = p_state AND (r.created_at, r.id) < (c_ts, c_id)
                     ORDER BY r.created_at DESC, r.id DESC LIMIT n;
    END IF;
END;
$$;

/* Followed/friended ids with state (1 follower, 2 friend) — covered by idx_relations_out. */
CREATE FUNCTION auth.func_load_addable_relations(p_user_id bigint) RETURNS TABLE(target_id bigint, state smallint)
LANGUAGE sql STABLE AS $$
    SELECT r.secondary_id, r.state FROM auth.relations r WHERE r.primary_id = p_user_id AND r.state IN (1, 2)
$$;

/* Cold-start seeders. v1 func_load_relations_paginated declared 4 columns but SELECTed 3 (always failed). */
CREATE FUNCTION auth.func_load_relations_paginated(p_limit integer, p_offset integer)
RETURNS TABLE(primary_id bigint, secondary_id bigint, state smallint, created_at timestamptz)
LANGUAGE sql STABLE AS $$
    SELECT r.primary_id, r.secondary_id, r.state, util.ms_to_ts(r.created_at)
    FROM auth.relations r ORDER BY r.id LIMIT p_limit OFFSET GREATEST(p_offset, 0)
$$;

CREATE FUNCTION auth.func_load_relations_seed_keyset(p_after_id bigint DEFAULT 0, p_limit integer DEFAULT 5000)
RETURNS TABLE(id bigint, primary_id bigint, secondary_id bigint, state smallint, created_at timestamptz)
LANGUAGE sql STABLE AS $$
    SELECT r.id, r.primary_id, r.secondary_id, r.state, util.ms_to_ts(r.created_at)
    FROM auth.relations r WHERE r.id > COALESCE(p_after_id, 0) ORDER BY r.id LIMIT LEAST(GREATEST(p_limit, 1), 50000)
$$;

/* User-lite seeder (inclut désormais trust_score pour le Speed Cache L1). */
CREATE FUNCTION auth.func_load_users_paginated(p_limit integer, p_offset integer)
RETURNS TABLE(id bigint, username text, first_name text, last_name text, profile_picture_id bigint, bio text,
              grade smallint, trust_score double precision, conversation_permission smallint,
              add_group_permission smallint, hide_connections smallint)
LANGUAGE sql STABLE AS $$
    SELECT  u.id, u.username, u.first_name, u.last_name, u.profile_picture_id, u.bio, u.grade, u.trust_score,
            COALESCE((s.privacy ->> 'conversation_permission')::smallint, 0::smallint),
            COALESCE((s.privacy ->> 'add_group_permission')::smallint,   0::smallint),
            COALESCE((s.privacy ->> 'hide_connections')::smallint,       0::smallint)
    FROM auth.users u LEFT JOIN auth.user_settings s ON s.user_id = u.id
    ORDER BY u.id LIMIT p_limit OFFSET GREATEST(p_offset, 0)
$$;

CREATE FUNCTION auth.func_load_users_keyset(p_after_id bigint DEFAULT 0, p_limit integer DEFAULT 5000)
RETURNS TABLE(id bigint, username text, first_name text, last_name text, profile_picture_id bigint, bio text,
              grade smallint, trust_score double precision, conversation_permission smallint,
              add_group_permission smallint, hide_connections smallint)
LANGUAGE sql STABLE AS $$
    SELECT u.id, u.username, u.first_name, u.last_name, u.profile_picture_id, u.bio, u.grade, u.trust_score,
           COALESCE((s.privacy ->> 'conversation_permission')::smallint, 0::smallint),
           COALESCE((s.privacy ->> 'add_group_permission')::smallint,   0::smallint),
           COALESCE((s.privacy ->> 'hide_connections')::smallint,       0::smallint)
    FROM auth.users u LEFT JOIN auth.user_settings s ON s.user_id = u.id
    WHERE u.id > COALESCE(p_after_id, 0) ORDER BY u.id LIMIT LEAST(GREATEST(p_limit, 1), 50000)
$$;

/* Mise à jour atomique par lot des Trust Scores et récompense mensuelle (+0.02). */
CREATE FUNCTION auth.func_apply_trust_score_delta(p_user_id bigint, p_delta double precision, p_is_infraction boolean,
                                                  p_is_major_ban boolean, p_ban_reason text DEFAULT NULL)
RETURNS TABLE(new_score double precision, is_banned boolean)
LANGUAGE plpgsql AS $$
DECLARE
v_score  double precision;
    v_banned boolean;
    v_now    bigint := util.now_ms();
BEGIN
    IF p_is_major_ban THEN
        UPDATE auth.users u
        SET trust_score        = 0.00,
            banned             = true,
            ban_reason         = COALESCE(p_ban_reason, 'Infraction majeure confirmée'),
            last_infraction_at = v_now,
            updated_at         = v_now
        WHERE u.id = p_user_id
            RETURNING u.trust_score, u.banned INTO v_score, v_banned;
    ELSE
        UPDATE auth.users u
        SET trust_score        = LEAST(1.00, GREATEST(0.00, u.trust_score + p_delta)),
            banned             = CASE WHEN LEAST(1.00, GREATEST(0.00, u.trust_score + p_delta)) <= 0.00 THEN true ELSE u.banned END,
            ban_reason         = CASE WHEN LEAST(1.00, GREATEST(0.00, u.trust_score + p_delta)) <= 0.00 THEN COALESCE(p_ban_reason, 'Trust Score tombé à 0.00') ELSE u.ban_reason END,
            last_infraction_at = CASE WHEN p_is_infraction THEN v_now ELSE u.last_infraction_at END,
            updated_at         = v_now
        WHERE u.id = p_user_id
            RETURNING u.trust_score, u.banned INTO v_score, v_banned;
    END IF;
RETURN QUERY SELECT COALESCE(v_score, 0.00), COALESCE(v_banned, false);
END;
$$;

/* Cron mensuel : +0.02 pour tous les utilisateurs actifs non bannis sans infraction depuis >= 30 jours. */
CREATE FUNCTION auth.func_increment_monthly_trust_scores()
RETURNS TABLE(user_id bigint, new_trust_score double precision)
LANGUAGE sql AS $$
    UPDATE auth.users u
    SET trust_score = LEAST(1.00, u.trust_score + 0.02),
        updated_at  = util.now_ms()
    WHERE u.banned = false
      AND u.desactivated = false
      AND u.trust_score > 0.00
      AND u.trust_score < 1.00
      AND u.created_at <= util.now_ms() - (30::bigint * 86400000)
      AND (u.last_infraction_at IS NULL OR u.last_infraction_at <= util.now_ms() - (30::bigint * 86400000))
        RETURNING u.id, u.trust_score
$$;

/* Cuckoo-filter warm-up (username/email/phone of every user). */
CREATE FUNCTION auth.func_load_all_user_identifiers() RETURNS TABLE(username text, email text, phone text)
LANGUAGE sql STABLE AS $$ SELECT u.username, u.email, u.phone FROM auth.users u $$;

/*  */
CREATE FUNCTION auth.func_get_user_passkeys(p_user_id bigint)
    RETURNS TABLE(
                     id bigint, user_id bigint, credential_id text, public_key bytea,
                     attestation_type text, aaguid bytea, sign_count bigint,
                     device_name text, transports text[], created_at bigint, updated_at bigint
                 )
    LANGUAGE sql STABLE AS $$
SELECT p.id, p.user_id, p.credential_id, p.public_key,
       p.attestation_type, p.aaguid, p.sign_count,
       p.device_name, p.transports, p.created_at, p.updated_at
FROM auth.user_passkeys p
WHERE p.user_id = p_user_id
ORDER BY p.created_at DESC, p.id DESC
$$;

/*  */
CREATE FUNCTION auth.func_get_passkey_by_credential_id(p_credential_id text)
    RETURNS TABLE(
                     id bigint, user_id bigint, credential_id text, public_key bytea,
                     attestation_type text, aaguid bytea, sign_count bigint,
                     device_name text, transports text[], created_at bigint, updated_at bigint
                 )
    LANGUAGE sql STABLE AS $$
SELECT p.id, p.user_id, p.credential_id, p.public_key,
       p.attestation_type, p.aaguid, p.sign_count,
       p.device_name, p.transports, p.created_at, p.updated_at
FROM auth.user_passkeys p
WHERE p.credential_id = btrim(p_credential_id)
$$;

/* ---------------------------------------------------------------------------
   7.2 CONTENT — posts
   All post readers return content.posts_l3 (22 columns, epoch-ms timestamps, report_count INCLUDED).
   v1 problems fixed here: (a) func_load_user_posts / _paginated / _recent returned 21 columns while
   scanPosts scans 22 -> every row skipped silently; (b) `visibility != 2` hid friends-only posts.
   --------------------------------------------------------------------------- */

-- Explicit batch hydration by id (getPostWithFallback). Live rows of ANY visibility class (0/1/2).
CREATE FUNCTION content.func_get_posts_by_ids(p_post_ids bigint[]) RETURNS SETOF content.posts_l3
LANGUAGE sql STABLE AS $$
    SELECT * FROM content.posts_l3 v
    WHERE v.id = ANY(p_post_ids) AND v.visibility >= 0
    ORDER BY v.created_at DESC, v.id DESC
$$;

-- Profile timeline, OFFSET form (kept for the current repository signature) and keyset form.
CREATE FUNCTION content.func_load_user_posts(p_user_id bigint, p_limit integer, p_offset integer)
RETURNS SETOF content.posts_l3
LANGUAGE sql STABLE AS $$
    SELECT * FROM content.posts_l3 v WHERE v.user_id = p_user_id AND v.visibility >= 0
    ORDER BY v.created_at DESC, v.id DESC
    LIMIT LEAST(GREATEST(p_limit, 1), 200) OFFSET GREATEST(p_offset, 0)
$$;

-- Cursor = (created_at, id) of the last row of the previous page; NULL/NULL = first page.
CREATE FUNCTION content.func_load_user_posts_keyset(
    p_user_id bigint, p_before_created_at bigint DEFAULT NULL, p_before_id bigint DEFAULT NULL, p_limit integer DEFAULT 50)
RETURNS SETOF content.posts_l3
LANGUAGE sql STABLE AS $$
    SELECT * FROM content.posts_l3 v
    WHERE v.user_id = p_user_id AND v.visibility >= 0
      AND (v.created_at, v.id) < (COALESCE(p_before_created_at, 9223372036854775807), COALESCE(p_before_id, 9223372036854775807))
    ORDER BY v.created_at DESC, v.id DESC
    LIMIT LEAST(GREATEST(p_limit, 1), 100)
$$;

-- Global post walker used by cold-start seeders (no clamp: operator-driven batch sizes).
CREATE FUNCTION content.func_load_posts_paginated(p_limit integer, p_offset integer) RETURNS SETOF content.posts_l3
LANGUAGE sql STABLE AS $$
    SELECT * FROM content.posts_l3 v WHERE v.visibility >= 0
    ORDER BY v.created_at DESC, v.id DESC LIMIT p_limit OFFSET GREATEST(p_offset, 0)
$$;

CREATE FUNCTION content.func_load_posts_keyset(
    p_before_created_at bigint DEFAULT NULL, p_before_id bigint DEFAULT NULL, p_limit integer DEFAULT 1000)
RETURNS SETOF content.posts_l3
LANGUAGE sql STABLE AS $$
    SELECT * FROM content.posts_l3 v
    WHERE v.visibility >= 0
      AND (v.created_at, v.id) < (COALESCE(p_before_created_at, 9223372036854775807), COALESCE(p_before_id, 9223372036854775807))
    ORDER BY v.created_at DESC, v.id DESC LIMIT LEAST(GREATEST(p_limit, 1), 50000)
$$;

CREATE FUNCTION content.func_load_recent_posts(p_days integer) RETURNS SETOF content.posts_l3
LANGUAGE sql STABLE AS $$
    SELECT * FROM content.posts_l3 v
    WHERE v.visibility >= 0 AND v.created_at >= util.now_ms() - (p_days::bigint * 86400000)
    ORDER BY v.created_at DESC, v.id DESC
$$;

/* Time-decay worker (runs every 2 min .. 6 h over 4 age tiers). Window is [now-max, now-min].
   Go scans created_at into time.Time, hence timestamptz on output. Served by idx_posts_live_created. */
CREATE FUNCTION content.func_load_posts_for_time_decay(p_min_age interval, p_max_age interval)
RETURNS TABLE(id bigint, like_count integer, comment_count integer, view_count integer, has_media boolean,
              created_at timestamptz, hashtags text[], indirect_hashtags text[], visibility smallint,
              priority_level smallint, report_count integer)
LANGUAGE sql STABLE AS $$
    SELECT p.id, p.like_count, p.comment_count, p.view_count, p.has_media, util.ms_to_ts(p.created_at),
           p.hashtags, p.indirect_hashtags, p.visibility, p.priority_level, p.report_count
    FROM content.posts p
    WHERE p.visibility >= 0
      AND p.created_at <= util.ts_to_ms(pg_catalog.now() - p_min_age)
      AND p.created_at >  util.ts_to_ms(pg_catalog.now() - p_max_age)
$$;

/* Markov-graph seeding, chronological. Condition now equals the live path (handleGraphUpdate):
   direct + indirect tags >= 2. v1 required `direct > 1 OR indirect > 0` and, as written twice with different
   return types, failed to load at all. Unbounded by design (one pass at start-up). */
CREATE FUNCTION content.func_load_posts_for_graph_seeding()
RETURNS TABLE(id bigint, hashtags text[], indirect_hashtags text[], created_at timestamptz)
LANGUAGE sql STABLE AS $$
    SELECT p.id, p.hashtags, p.indirect_hashtags, util.ms_to_ts(p.created_at)
    FROM content.posts p
    WHERE p.visibility >= 0
      AND COALESCE(cardinality(p.hashtags), 0) + COALESCE(cardinality(p.indirect_hashtags), 0) > 1
    ORDER BY p.created_at ASC, p.id ASC
$$;

CREATE FUNCTION content.func_load_timeline_seed_paginated(p_limit integer, p_offset integer)
RETURNS TABLE(id bigint, user_id bigint, created_at timestamptz)
LANGUAGE sql STABLE AS $$
    SELECT p.id, p.user_id, util.ms_to_ts(p.created_at) FROM content.posts p
    WHERE p.visibility >= 0 ORDER BY p.created_at DESC, p.id DESC LIMIT p_limit OFFSET GREATEST(p_offset, 0)
$$;

CREATE FUNCTION content.func_load_timeline_seed_keyset(
    p_before_created_at bigint DEFAULT NULL, p_before_id bigint DEFAULT NULL, p_limit integer DEFAULT 5000)
RETURNS TABLE(id bigint, user_id bigint, created_at bigint)
LANGUAGE sql STABLE AS $$
    SELECT p.id, p.user_id, p.created_at FROM content.posts p
    WHERE p.visibility >= 0
      AND (p.created_at, p.id) < (COALESCE(p_before_created_at, 9223372036854775807), COALESCE(p_before_id, 9223372036854775807))
    ORDER BY p.created_at DESC, p.id DESC LIMIT LEAST(GREATEST(p_limit, 1), 50000)
$$;

/* ---------------------------------------------------------------------------
   7.2b CONTENT — tag / user / text search
   `col @> ARRAY[tag]` is GIN-indexable; v1's `tag = ANY(col)` forced a scan (measured in review §11).
   Sorted-by-counter searches stay OFFSET-based: their sort keys mutate between requests, so a keyset
   cursor would not be stable either (review §8).
   --------------------------------------------------------------------------- */
CREATE FUNCTION content.func_load_post_ids_by_tag_paginated(p_tag text, p_offset integer, p_limit integer)
RETURNS TABLE(id bigint)
LANGUAGE sql STABLE AS $$
    SELECT p.id FROM content.posts p
    WHERE (p.hashtags @> ARRAY[p_tag] OR p.indirect_hashtags @> ARRAY[p_tag]) AND p.visibility >= 0
    ORDER BY p.created_at DESC, p.id DESC OFFSET GREATEST(p_offset, 0) LIMIT LEAST(GREATEST(p_limit, 1), 200)
$$;

CREATE FUNCTION content.func_load_post_ids_by_tag_keyset(
    p_tag text, p_before_created_at bigint DEFAULT NULL, p_before_id bigint DEFAULT NULL, p_limit integer DEFAULT 50)
RETURNS TABLE(id bigint, created_at bigint)
LANGUAGE sql STABLE AS $$
    SELECT p.id, p.created_at FROM content.posts p
    WHERE (p.hashtags @> ARRAY[p_tag] OR p.indirect_hashtags @> ARRAY[p_tag]) AND p.visibility >= 0
      AND (p.created_at, p.id) < (COALESCE(p_before_created_at, 9223372036854775807), COALESCE(p_before_id, 9223372036854775807))
    ORDER BY p.created_at DESC, p.id DESC LIMIT LEAST(GREATEST(p_limit, 1), 100)
$$;

-- order modes: 0 views, 1 likes, 2 comments, 3 recent, 4 oldest
CREATE FUNCTION content.func_search_post_ids_by_tag(p_tag text, p_order_mode smallint, p_offset integer, p_limit integer)
RETURNS TABLE(id bigint)
LANGUAGE sql STABLE AS $$
    SELECT p.id FROM content.posts p
    WHERE (p.hashtags @> ARRAY[p_tag] OR p.indirect_hashtags @> ARRAY[p_tag]) AND p.visibility >= 0
    ORDER BY CASE WHEN p_order_mode = 0 THEN p.view_count    END DESC NULLS LAST,
             CASE WHEN p_order_mode = 1 THEN p.like_count    END DESC NULLS LAST,
             CASE WHEN p_order_mode = 2 THEN p.comment_count END DESC NULLS LAST,
             CASE WHEN p_order_mode = 3 THEN p.created_at    END DESC NULLS LAST,
             CASE WHEN p_order_mode = 4 THEN p.created_at    END ASC  NULLS LAST,
             p.id DESC
    OFFSET GREATEST(p_offset, 0) LIMIT LEAST(GREATEST(p_limit, 1), 200)
$$;

CREATE FUNCTION content.func_search_post_ids_by_users(p_user_ids bigint[], p_order_mode smallint, p_offset integer, p_limit integer)
RETURNS TABLE(id bigint)
LANGUAGE sql STABLE AS $$
    SELECT p.id FROM content.posts p
    WHERE p.user_id = ANY(p_user_ids) AND p.visibility >= 0
    ORDER BY CASE WHEN p_order_mode = 0 THEN p.view_count    END DESC NULLS LAST,
             CASE WHEN p_order_mode = 1 THEN p.like_count    END DESC NULLS LAST,
             CASE WHEN p_order_mode = 2 THEN p.comment_count END DESC NULLS LAST,
             CASE WHEN p_order_mode = 3 THEN p.created_at    END DESC NULLS LAST,
             CASE WHEN p_order_mode = 4 THEN p.created_at    END ASC  NULLS LAST,
             p.id DESC
    OFFSET GREATEST(p_offset, 0) LIMIT LEAST(GREATEST(p_limit, 1), 200)
$$;

-- Go passes the order mode as ::integer here (see func_search_post_by_text.go); signature kept.
CREATE FUNCTION content.func_search_post_ids_by_text(p_query text, p_order_mode integer, p_offset integer, p_limit integer)
RETURNS TABLE(id bigint)
LANGUAGE sql STABLE AS $$
    SELECT p.id FROM content.posts p
    WHERE to_tsvector('french', p.content) @@ plainto_tsquery('french', p_query) AND p.visibility >= 0
    ORDER BY CASE WHEN p_order_mode = 0 THEN p.view_count    END DESC NULLS LAST,
             CASE WHEN p_order_mode = 1 THEN p.like_count    END DESC NULLS LAST,
             CASE WHEN p_order_mode = 2 THEN p.comment_count END DESC NULLS LAST,
             CASE WHEN p_order_mode = 3 THEN p.created_at    END DESC NULLS LAST,
             CASE WHEN p_order_mode = 4 THEN p.created_at    END ASC  NULLS LAST,
             p.id DESC
    OFFSET GREATEST(p_offset, 0) LIMIT LEAST(GREATEST(p_limit, 1), 200)
$$;

/* ---------------------------------------------------------------------------
   7.3 CONTENT — comments (timestamps stay timestamptz; Go scans them into string)
   --------------------------------------------------------------------------- */
CREATE FUNCTION content.func_get_comment(p_comment_id bigint)
RETURNS TABLE(id bigint, post_id bigint, user_id bigint, content text, visibility integer, like_count integer,
              score integer, created_at timestamptz, updated_at timestamptz)
LANGUAGE sql STABLE AS $$
    SELECT c.id, c.post_id, c.user_id, c.content, c.visibility, c.like_count, c.score, c.created_at, c.updated_at
    FROM content.comments c WHERE c.id = p_comment_id
$$;

-- Ranking order score DESC, created_at ASC (+ id tie-break). Served by idx_comments_post_score.
CREATE FUNCTION content.func_load_comments_paginated(p_post_id bigint, p_limit integer, p_offset integer)
RETURNS TABLE(id bigint, post_id bigint, user_id bigint, content text, visibility integer, like_count integer,
              score integer, created_at timestamptz, updated_at timestamptz)
LANGUAGE sql STABLE AS $$
    SELECT c.id, c.post_id, c.user_id, c.content, c.visibility, c.like_count, c.score, c.created_at, c.updated_at
    FROM content.comments c WHERE c.post_id = p_post_id AND c.visibility <> -1
    ORDER BY c.score DESC, c.created_at ASC, c.id ASC
    LIMIT LEAST(GREATEST(p_limit, 1), 100) OFFSET GREATEST(p_offset, 0)
$$;

/* Keyset: cursor = (score, created_at, id) of the last returned comment; all three NULL = first page.
   Mixed sort directions cannot use a row comparison, so the continuation predicate is expanded. */
CREATE FUNCTION content.func_load_comments_keyset(
    p_post_id bigint, p_after_score integer DEFAULT NULL, p_after_created_at timestamptz DEFAULT NULL,
    p_after_id bigint DEFAULT NULL, p_limit integer DEFAULT 50)
RETURNS TABLE(id bigint, post_id bigint, user_id bigint, content text, visibility integer, like_count integer,
              score integer, created_at timestamptz, updated_at timestamptz)
LANGUAGE plpgsql STABLE AS $$
DECLARE n integer := LEAST(GREATEST(p_limit, 1), 100);
BEGIN
    IF p_after_id IS NULL THEN
        RETURN QUERY SELECT c.id, c.post_id, c.user_id, c.content, c.visibility, c.like_count, c.score, c.created_at, c.updated_at
                     FROM content.comments c WHERE c.post_id = p_post_id AND c.visibility <> -1
                     ORDER BY c.score DESC, c.created_at ASC, c.id ASC LIMIT n;
    ELSE
        RETURN QUERY SELECT c.id, c.post_id, c.user_id, c.content, c.visibility, c.like_count, c.score, c.created_at, c.updated_at
                     FROM content.comments c
                     WHERE c.post_id = p_post_id AND c.visibility <> -1
                       AND (c.score < p_after_score
                            OR (c.score = p_after_score AND c.created_at > p_after_created_at)
                            OR (c.score = p_after_score AND c.created_at = p_after_created_at AND c.id > p_after_id))
                     ORDER BY c.score DESC, c.created_at ASC, c.id ASC LIMIT n;
    END IF;
END;
$$;

CREATE FUNCTION content.func_increment_comment_metrics(p_comment_id bigint, p_delta integer) RETURNS void
LANGUAGE sql AS $$
    UPDATE content.comments SET like_count = GREATEST(0, like_count + p_delta), score = score + p_delta WHERE id = p_comment_id
$$;

/* ---------------------------------------------------------------------------
   7.4 CONTENT — likes (explicit readers; the catch-all func_load_likes stays in SECTION 9)
   Returned columns/types are those scanned by func_load_likes.go (created_at stays timestamptz).
   --------------------------------------------------------------------------- */
CREATE FUNCTION content.func_get_like(p_target_type smallint, p_target_id bigint, p_user_id bigint)
RETURNS TABLE(id bigint, target_type smallint, target_id bigint, user_id bigint, created_at timestamptz)
LANGUAGE sql STABLE AS $$
    SELECT l.id, l.target_type, l.target_id, l.user_id, l.created_at FROM content.likes l
    WHERE l.target_type = p_target_type AND l.target_id = p_target_id AND l.user_id = p_user_id
$$;

CREATE FUNCTION content.func_list_likes_by_target(p_target_type smallint, p_target_id bigint, p_limit integer DEFAULT 100)
RETURNS TABLE(id bigint, target_type smallint, target_id bigint, user_id bigint, created_at timestamptz)
LANGUAGE sql STABLE AS $$
    SELECT l.id, l.target_type, l.target_id, l.user_id, l.created_at FROM content.likes l
    WHERE l.target_type = p_target_type AND l.target_id = p_target_id
    ORDER BY l.created_at DESC LIMIT LEAST(GREATEST(p_limit, 1), 500)
$$;

CREATE FUNCTION content.func_list_likes_by_user(p_user_id bigint, p_target_type smallint, p_limit integer DEFAULT 100)
RETURNS TABLE(id bigint, target_type smallint, target_id bigint, user_id bigint, created_at timestamptz)
LANGUAGE sql STABLE AS $$
    SELECT l.id, l.target_type, l.target_id, l.user_id, l.created_at FROM content.likes l
    WHERE l.user_id = p_user_id AND l.target_type = p_target_type
    ORDER BY l.created_at DESC LIMIT LEAST(GREATEST(p_limit, 1), 500)
$$;

/* ---------------------------------------------------------------------------
   7.5 CONTENT — media
   --------------------------------------------------------------------------- */
-- name kept from v1 (content.get_media). storage_path NULL -> '' because Go scans it into string.
CREATE FUNCTION content.get_media(p_media_id bigint)
RETURNS TABLE(id bigint, owner_id bigint, storage_path text, visibility smallint, ia_score double precision,
              created_at bigint, updated_at bigint)
LANGUAGE sql STABLE AS $$
    SELECT m.id, m.owner_id, COALESCE(m.storage_path, ''), m.visibility, COALESCE(m.ia_score, 0.0), m.created_at, m.updated_at
    FROM content.media m
    WHERE m.id = p_media_id
$$;

/* ---------------------------------------------------------------------------
   7.6 CONTENT — saved
   --------------------------------------------------------------------------- */
CREATE FUNCTION content.func_save_post(p_id bigint, p_user_id bigint, p_post_id bigint) RETURNS void
LANGUAGE sql AS $$
    INSERT INTO content.saved (id, user_id, post_id, created_at) VALUES (p_id, p_user_id, p_post_id, util.now_ms())
    ON CONFLICT (user_id, post_id) DO NOTHING
$$;

CREATE FUNCTION content.func_unsave_post(p_user_id bigint, p_post_id bigint) RETURNS void
LANGUAGE sql AS $$ DELETE FROM content.saved WHERE user_id = p_user_id AND post_id = p_post_id $$;

CREATE FUNCTION content.func_load_saved_posts(p_user_id bigint, p_limit integer, p_offset integer)
RETURNS TABLE(id bigint, user_id bigint, post_id bigint, created_at bigint)
LANGUAGE sql STABLE AS $$
    SELECT s.id, s.user_id, s.post_id, s.created_at FROM content.saved s WHERE s.user_id = p_user_id
    ORDER BY s.created_at DESC, s.id DESC LIMIT LEAST(GREATEST(p_limit, 1), 100) OFFSET GREATEST(p_offset, 0)
$$;

CREATE FUNCTION content.func_load_saved_posts_keyset(
    p_user_id bigint, p_before_created_at bigint DEFAULT NULL, p_before_id bigint DEFAULT NULL, p_limit integer DEFAULT 50)
RETURNS TABLE(id bigint, user_id bigint, post_id bigint, created_at bigint)
LANGUAGE sql STABLE AS $$
    SELECT s.id, s.user_id, s.post_id, s.created_at FROM content.saved s
    WHERE s.user_id = p_user_id
      AND (s.created_at, s.id) < (COALESCE(p_before_created_at, 9223372036854775807), COALESCE(p_before_id, 9223372036854775807))
    ORDER BY s.created_at DESC, s.id DESC LIMIT LEAST(GREATEST(p_limit, 1), 100)
$$;

/* ---------------------------------------------------------------------------
   7.7 CONTENT — tags + atomic counters
   --------------------------------------------------------------------------- */
CREATE FUNCTION content.func_load_all_tags() RETURNS TABLE(slug text)
LANGUAGE sql STABLE AS $$ SELECT t.slug FROM content.tags t $$;

-- Replacement for the raw INSERT in worker/hashtag_canon.go (bulk, idempotent). Returns rows inserted.
CREATE FUNCTION content.func_upsert_community_tags(p_slugs text[]) RETURNS integer
LANGUAGE sql AS $$
    WITH ins AS (
        INSERT INTO content.tags (slug, is_community) SELECT DISTINCT s, true FROM unnest(p_slugs) AS s
        ON CONFLICT (slug) DO NOTHING RETURNING 1)
    SELECT count(*)::integer FROM ins
$$;

-- Atomic, duplicate-free append (single UPDATE; the row lock serialises concurrent commenters).
CREATE FUNCTION content.func_add_indirect_tag_to_post(p_post_id bigint, p_tag text) RETURNS void
LANGUAGE sql AS $$
    UPDATE content.posts SET indirect_hashtags = array_append(COALESCE(indirect_hashtags, '{}'), p_tag)
    WHERE id = p_post_id AND NOT (COALESCE(indirect_hashtags, '{}') @> ARRAY[p_tag]::text[])
$$;

CREATE FUNCTION content.func_increment_post_like(p_post_id bigint, p_delta integer) RETURNS void
LANGUAGE sql AS $$ UPDATE content.posts SET like_count = GREATEST(0, like_count + p_delta) WHERE id = p_post_id $$;
CREATE FUNCTION content.func_increment_post_comment(p_post_id bigint, p_delta integer) RETURNS void
LANGUAGE sql AS $$ UPDATE content.posts SET comment_count = GREATEST(0, comment_count + p_delta) WHERE id = p_post_id $$;
CREATE FUNCTION content.func_increment_post_view(p_post_id bigint, p_delta integer) RETURNS void
LANGUAGE sql AS $$ UPDATE content.posts SET view_count = GREATEST(0, view_count + p_delta) WHERE id = p_post_id $$;
CREATE FUNCTION content.func_increment_post_report(p_post_id bigint, p_delta integer) RETURNS void
LANGUAGE sql AS $$ UPDATE content.posts SET report_count = GREATEST(0, report_count + p_delta) WHERE id = p_post_id $$;
CREATE FUNCTION content.func_increment_post_telemetry(p_post_id bigint, p_dwell_sum double precision,
                                                      p_dwell_sq double precision, p_clicks integer) RETURNS void
LANGUAGE sql AS $$
    UPDATE content.posts SET telemetry_dwell_sum = telemetry_dwell_sum + p_dwell_sum,
                             telemetry_dwell_sq  = telemetry_dwell_sq  + p_dwell_sq,
                             telemetry_clicks    = telemetry_clicks    + p_clicks
    WHERE id = p_post_id
$$;

/* Counter reconciliation (MAINTENANCE — not called by any current worker). The workers apply deltas for every
   event of a batch even when the row insert later fails (duplicate -> DLQ), and treat comment UPDATE events as
   +1 (review §10, WB-5/6), so stored counters can drift. This recomputes like/comment counts from the source
   rows for an explicit id list (bounded) and returns how many posts were corrected. Run off-peak. */
CREATE FUNCTION content.func_reconcile_post_counters(p_post_ids bigint[]) RETURNS integer
LANGUAGE plpgsql AS $$
DECLARE v_fixed integer;
BEGIN
    IF cardinality(p_post_ids) > 10000 THEN
        RAISE EXCEPTION 'func_reconcile_post_counters: at most 10000 ids per call' USING ERRCODE = '22023';
    END IF;
    WITH real AS (
        SELECT p.id,
               (SELECT count(*) FROM content.likes l    WHERE l.target_type = 0 AND l.target_id = p.id)::integer AS lc,
               (SELECT count(*) FROM content.comments c WHERE c.post_id = p.id AND c.visibility <> -1)::integer  AS cc
        FROM content.posts p WHERE p.id = ANY(p_post_ids))
    UPDATE content.posts p SET like_count = r.lc, comment_count = r.cc
    FROM real r WHERE p.id = r.id AND (p.like_count <> r.lc OR p.comment_count <> r.cc);
    GET DIAGNOSTICS v_fixed = ROW_COUNT;
    RETURN v_fixed;
END;
$$;

CREATE FUNCTION content.func_reconcile_comment_likes(p_comment_ids bigint[]) RETURNS integer
LANGUAGE plpgsql AS $$
DECLARE v_fixed integer;
BEGIN
    IF cardinality(p_comment_ids) > 10000 THEN
        RAISE EXCEPTION 'func_reconcile_comment_likes: at most 10000 ids per call' USING ERRCODE = '22023';
    END IF;
    WITH real AS (
        SELECT c.id, (SELECT count(*) FROM content.likes l WHERE l.target_type = 1 AND l.target_id = c.id)::integer AS lc
        FROM content.comments c WHERE c.id = ANY(p_comment_ids))
    UPDATE content.comments c SET like_count = r.lc, score = r.lc
    FROM real r WHERE c.id = r.id AND (c.like_count <> r.lc OR c.score <> r.lc);
    GET DIAGNOSTICS v_fixed = ROW_COUNT;
    RETURN v_fixed;
END;
$$;

/* ---------------------------------------------------------------------------
   7.8 CONTENT — garbage collectors (called by worker cleanup workers)
   Same signatures and result shapes as v1. Changes:
     * D2: `visibility != 2` removed — v1 DELETED the likes/saves of friends-only posts (data loss).
     * Likes/saved/reactions are now DRIVEN FROM THE SOFT-DELETED PARENTS (small, partial-indexed set)
       instead of anti-joining every like/save/reaction row on every run. Hard-deleted parents are covered by
       FK cascades (saved, reactions) or by the *_full maintenance variant (likes, polymorphic => no FK).
   --------------------------------------------------------------------------- */
CREATE FUNCTION content.func_delete_orphan_likes() RETURNS TABLE(target_type smallint, target_id bigint)
LANGUAGE plpgsql AS $$
BEGIN
    RETURN QUERY
    WITH del AS (
        DELETE FROM content.likes l USING content.posts p
        WHERE l.target_type = 0 AND l.target_id = p.id AND p.visibility < 0
        RETURNING l.target_type, l.target_id
    ), del_c AS (
        DELETE FROM content.likes l USING content.comments c
        WHERE l.target_type = 1 AND l.target_id = c.id AND c.visibility < 0
        RETURNING l.target_type, l.target_id
    )
    SELECT DISTINCT d.target_type, d.target_id FROM del d
    UNION
    SELECT DISTINCT d.target_type, d.target_id FROM del_c d;
END;
$$;

-- Full anti-join (catches likes whose target row no longer exists at all). Scans all likes: run rarely, off-peak.
CREATE FUNCTION content.func_delete_orphan_likes_full() RETURNS TABLE(target_type smallint, target_id bigint)
LANGUAGE sql AS $$
    WITH del AS (
        DELETE FROM content.likes l
        WHERE (l.target_type = 0 AND NOT EXISTS (SELECT 1 FROM content.posts p    WHERE p.id = l.target_id AND p.visibility >= 0))
           OR (l.target_type = 1 AND NOT EXISTS (SELECT 1 FROM content.comments c WHERE c.id = l.target_id AND c.visibility >= 0))
        RETURNING l.target_type, l.target_id)
    SELECT DISTINCT d.target_type, d.target_id FROM del d
$$;

-- saved.post_id has FK ON DELETE CASCADE, so the only orphans are soft-deleted posts: exact equivalent of v1.
CREATE FUNCTION content.func_delete_orphan_saved() RETURNS TABLE(post_id bigint)
LANGUAGE sql AS $$
    WITH del AS (
        DELETE FROM content.saved s USING content.posts p WHERE s.post_id = p.id AND p.visibility < 0
        RETURNING s.post_id)
    SELECT DISTINCT d.post_id FROM del d
$$;

/* Media GC. Shields and lifecycle windows are unchanged from v1:
     shield 1 legal holds (forever) · shield 2 open reports (state < 2)
     A) pending/orphan, unreferenced, idle > 6 h   B) soft-deleted, idle > 48 h
   Rewritten so every reference probe is index-assisted: `m.id = ANY(p.media_ids)` -> `p.media_ids @> ARRAY[m.id]` (GIN),
   `r.target_ids @> ARRAY[m.id]` (GIN), messages.attachments media_id via idx_messages_media_ref.
   storage_path NULL -> '' (Go scans into string; a NULL used to make the row vanish AFTER it was deleted => S3 leak). */
CREATE FUNCTION content.func_delete_orphan_media() RETURNS TABLE(id bigint, storage_path text)
LANGUAGE plpgsql AS $$
DECLARE v_now bigint := util.now_ms();
BEGIN
RETURN QUERY
    WITH del AS (
        DELETE FROM content.media m
        WHERE m.visibility IN (-2, -1, 0)
          AND NOT EXISTS (SELECT 1 FROM moderation.legal_holds lh JOIN content.posts p ON p.id = lh.post_id
                          WHERE p.media_ids @> ARRAY[m.id])
          AND NOT EXISTS (SELECT 1 FROM moderation.reports r
                          WHERE r.state < 2
                            AND ((r.target_type = 3 AND r.target_ids @> ARRAY[m.id])
                              OR (r.target_type = 1 AND EXISTS (SELECT 1 FROM content.posts p
                                                                WHERE p.id = ANY(r.target_ids) AND p.media_ids @> ARRAY[m.id]))))
          AND (
                m.visibility = -2
                OR (m.visibility = 0 AND m.updated_at < v_now - 6 * 3600000
                    AND NOT EXISTS (SELECT 1 FROM content.posts p WHERE p.media_ids @> ARRAY[m.id])
                    AND NOT EXISTS (SELECT 1 FROM messaging.messages msg
                                    WHERE msg.attachments ? 'media_id' AND msg.attachments -> 'media_id' = to_jsonb(m.id))
                    AND NOT EXISTS (SELECT 1 FROM auth.users u WHERE u.profile_picture_id = m.id))
                OR (m.visibility = -1 AND m.updated_at < v_now - 48 * 3600000)
              )
        RETURNING m.id, m.storage_path)
SELECT d.id, COALESCE(d.storage_path, '')::text FROM del d;
END;
$$;
/* ---------------------------------------------------------------------------
   7.9 MESSAGING — JSON payload builders (IMMUTABLE: pure functions of their row argument)
   Keys equal the Go json tags of conversation_models.ConversationPayload, lite_models.ConvLiteRequest and
   lite_models.MemberLiteRequest. Missing member-setting keys fall back to member_models.DefaultMemberSettings
   (pinned = -1, not Go's zero value 0 which would mean "pinned at index 0").
   --------------------------------------------------------------------------- */
CREATE FUNCTION messaging.conv_json(c messaging.conversations) RETURNS jsonb
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
    SELECT jsonb_build_object(
        'id', c.id, 'type', c.type, 'title', COALESCE(c.title, ''), 'description', COALESCE(c.description, ''),
        'avatar_id', COALESCE(c.avatar_id, 0), 'last_message_id', COALESCE(c.last_message_id, 0), 'state', c.state,
        'settings', c.settings, 'external_link', c.external_link,
        'created_at', c.created_at, 'updated_at', c.updated_at)
$$;

CREATE FUNCTION messaging.conv_lite_json(c messaging.conversations) RETURNS jsonb
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
    SELECT jsonb_build_object(
        'id', c.id, 'type', c.type, 'title', COALESCE(c.title, ''), 'description', COALESCE(c.description, ''),
        'avatar_id', COALESCE(c.avatar_id, 0), 'last_message_id', COALESCE(c.last_message_id, 0),
        'settings', c.settings, 'external_links', c.external_link)
$$;

CREATE FUNCTION messaging.member_lite_json(m messaging.members) RETURNS jsonb
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
    SELECT jsonb_build_object(
        'conversation_id', m.conversation_id, 'user_id', m.user_id, 'role', m.role,
        'settings', jsonb_build_object('is_muted', 0, 'mute_expire_at', 0, 'pinned', -1,
                                       'media_auto_download', true, 'restricted_until', 0) || m.settings,
        'unread_count', m.unread_count, 'frozen_message_id', COALESCE(m.frozen_message_id, 0),
        'last_read_message_id', COALESCE(m.last_read_message_id, 0), 'joined_at', m.joined_at)
$$;

/* Conversation by id -> conversation_models.ConversationPayload. SQL NULL when the row does not exist. */
CREATE FUNCTION messaging.func_get_conversation_json(p_conv_id bigint) RETURNS jsonb
LANGUAGE sql STABLE AS $$
    SELECT messaging.conv_json(c) FROM messaging.conversations c WHERE c.id = p_conv_id
$$;

/* Active 1:1 conversation of two users -> ConversationPayload (NULL if none). Both member probes use idx_members_user_conv. */
CREATE FUNCTION messaging.func_get_direct_conversation_json(p_user1 bigint, p_user2 bigint) RETURNS jsonb
LANGUAGE sql STABLE AS $$
    SELECT messaging.conv_json(c)
    FROM messaging.conversations c
    WHERE c.type = 0 AND c.state = 0
      AND EXISTS (SELECT 1 FROM messaging.members m1 WHERE m1.conversation_id = c.id AND m1.user_id = p_user1)
      AND EXISTS (SELECT 1 FROM messaging.members m2 WHERE m2.conversation_id = c.id AND m2.user_id = p_user2)
    LIMIT 1
$$;

/* Inbox fallback for specific conversation ids -> JSON array (never NULL) of
   {"conversation": ConvLiteRequest, "member": MemberLiteRequest}, newest conversation first. Bounded input. */
CREATE FUNCTION messaging.func_load_conversation_fallback_json(p_user_id bigint, p_conv_ids bigint[]) RETURNS jsonb
LANGUAGE plpgsql STABLE AS $$
BEGIN
    IF cardinality(p_conv_ids) > 200 THEN
        RAISE EXCEPTION 'func_load_conversation_fallback_json: at most 200 conversation ids' USING ERRCODE = '22023';
    END IF;
    RETURN (SELECT COALESCE(jsonb_agg(jsonb_build_object('conversation', messaging.conv_lite_json(c),
                                                         'member',       messaging.member_lite_json(m))
                                      ORDER BY COALESCE(c.last_message_id, 0) DESC, c.id DESC), '[]'::jsonb)
            FROM messaging.members m JOIN messaging.conversations c ON c.id = m.conversation_id
            WHERE m.user_id = p_user_id AND m.conversation_id = ANY(p_conv_ids) AND c.state <> 1);
END;
$$;

/* ---------------------------------------------------------------------------
   7.10 MESSAGING — messages
   --------------------------------------------------------------------------- */
-- History keyed on the Snowflake id. Served by idx_messages_conv_id_live. frozen_id = ceiling for banned members.
CREATE FUNCTION messaging.func_load_message_ids_paginated(
    p_conv_id bigint, p_offset_id bigint, p_limit integer, p_direction text, p_frozen_id bigint DEFAULT NULL)
RETURNS TABLE(id bigint)
LANGUAGE plpgsql STABLE AS $$
DECLARE n integer := LEAST(GREATEST(p_limit, 1), 500);
BEGIN
    IF p_direction = 'top' THEN
        RETURN QUERY SELECT m.id FROM messaging.messages m
                     WHERE m.conversation_id = p_conv_id AND m.visibility = true
                       AND (p_offset_id = 0 OR m.id < p_offset_id)
                       AND (p_frozen_id IS NULL OR m.id <= p_frozen_id)
                     ORDER BY m.id DESC LIMIT n;
    ELSE
        RETURN QUERY SELECT m.id FROM messaging.messages m
                     WHERE m.conversation_id = p_conv_id AND m.visibility = true
                       AND (p_offset_id = 0 OR m.id > p_offset_id)
                       AND (p_frozen_id IS NULL OR m.id <= p_frozen_id)
                     ORDER BY m.id ASC LIMIT n;
    END IF;
END;
$$;

/* L3 hydration by ids. Includes soft-deleted rows (as v1). content NULL -> '' : Go scans it into string and a NULL
   silently dropped every media/attachment-only message (rows.Scan error swallowed). */
CREATE FUNCTION messaging.func_load_messages_by_ids(p_message_ids bigint[])
RETURNS TABLE(id bigint, conversation_id bigint, sender_id bigint, message_type smallint, visibility boolean,
              content text, attachments jsonb, allow_new_reactions boolean, created_at bigint, updated_at bigint)
LANGUAGE sql STABLE AS $$
    SELECT m.id, m.conversation_id, m.sender_id, m.message_type, m.visibility, COALESCE(m.content, ''),
           m.attachments, m.allow_new_reactions, m.created_at, m.updated_at
    FROM messaging.messages m WHERE m.id = ANY(p_message_ids) ORDER BY m.id ASC
$$;

/* Same rows as ONE JSON array of message_models.MessagePayload, attachments fully parsed (the typed Go path never parsed them). */
CREATE FUNCTION messaging.func_load_messages_by_ids_json(p_message_ids bigint[]) RETURNS jsonb
LANGUAGE plpgsql STABLE AS $$
BEGIN
    IF cardinality(p_message_ids) > 500 THEN
        RAISE EXCEPTION 'func_load_messages_by_ids_json: at most 500 ids' USING ERRCODE = '22023';
    END IF;
    RETURN (SELECT COALESCE(jsonb_agg(jsonb_build_object(
                'attachments', COALESCE(m.attachments, '{}'::jsonb), 'content', COALESCE(m.content, ''),
                'id', m.id, 'conversation_id', m.conversation_id, 'sender_id', m.sender_id,
                'message_type', m.message_type, 'created_at', m.created_at, 'updated_at', m.updated_at,
                'visibility', m.visibility, 'allow_new_reactions', m.allow_new_reactions) ORDER BY m.id ASC), '[]'::jsonb)
            FROM messaging.messages m WHERE m.id = ANY(p_message_ids));
END;
$$;

/* ---------------------------------------------------------------------------
   7.11 MESSAGING — reactions
   --------------------------------------------------------------------------- */
CREATE FUNCTION messaging.func_get_message_reactions_paginated(p_message_id bigint, p_limit integer, p_offset integer)
RETURNS TABLE(id bigint, message_id bigint, user_id bigint, reaction text, created_at bigint)
LANGUAGE sql STABLE AS $$
    SELECT r.id, r.message_id, r.user_id, r.reaction, r.created_at FROM messaging.message_reactions r
    WHERE r.message_id = p_message_id ORDER BY r.created_at DESC, r.id DESC
    LIMIT LEAST(GREATEST(p_limit, 1), 100) OFFSET GREATEST(p_offset, 0)
$$;

CREATE FUNCTION messaging.func_get_message_reactions_keyset(
    p_message_id bigint, p_before_created_at bigint DEFAULT NULL, p_before_id bigint DEFAULT NULL, p_limit integer DEFAULT 50)
RETURNS TABLE(id bigint, message_id bigint, user_id bigint, reaction text, created_at bigint)
LANGUAGE sql STABLE AS $$
    SELECT r.id, r.message_id, r.user_id, r.reaction, r.created_at FROM messaging.message_reactions r
    WHERE r.message_id = p_message_id
      AND (r.created_at, r.id) < (COALESCE(p_before_created_at, 9223372036854775807), COALESCE(p_before_id, 9223372036854775807))
    ORDER BY r.created_at DESC, r.id DESC LIMIT LEAST(GREATEST(p_limit, 1), 100)
$$;

/* Applies {"emoji": delta} to attachments.reaction_counts. v1 read the counts and wrote them back without a row lock
   (lost update if two flushes interleaved); SELECT ... FOR UPDATE makes the read-modify-write atomic. */
CREATE FUNCTION messaging.func_apply_reaction_delta(p_message_id bigint, p_deltas jsonb) RETURNS void
LANGUAGE plpgsql AS $$
DECLARE
    v_counts jsonb;
    v_key text; v_val text; v_new integer;
BEGIN
    SELECT COALESCE(msg.attachments -> 'reaction_counts', '{}'::jsonb) INTO v_counts
    FROM messaging.messages msg WHERE msg.id = p_message_id FOR UPDATE;
    IF NOT FOUND THEN RETURN; END IF;

    FOR v_key, v_val IN SELECT * FROM jsonb_each_text(p_deltas) LOOP
        v_new := COALESCE((v_counts ->> v_key)::integer, 0) + v_val::integer;
        IF v_new <= 0 THEN v_counts := v_counts - v_key;
        ELSE v_counts := jsonb_set(v_counts, ARRAY[v_key], to_jsonb(v_new), true);
        END IF;
    END LOOP;

    UPDATE messaging.messages msg
    SET attachments = jsonb_set(COALESCE(msg.attachments, '{}'::jsonb), '{reaction_counts}', v_counts, true)
    WHERE msg.id = p_message_id;
END;
$$;

CREATE FUNCTION messaging.func_update_message_reaction_counts(p_message_id bigint) RETURNS void
LANGUAGE sql AS $$
    UPDATE messaging.messages msg
    SET attachments = jsonb_set(COALESCE(msg.attachments, '{}'::jsonb), '{reaction_counts}',
        (SELECT COALESCE(jsonb_object_agg(s.reaction, s.n), '{}'::jsonb)
         FROM (SELECT r.reaction, count(*) AS n FROM messaging.message_reactions r
               WHERE r.message_id = p_message_id GROUP BY r.reaction) s), true)
    WHERE msg.id = p_message_id
$$;

-- Driven from invisible messages (idx_messages_invisible); FK ON DELETE CASCADE already covers hard-deleted messages.
CREATE FUNCTION messaging.func_delete_orphan_reactions() RETURNS TABLE(message_id bigint)
LANGUAGE sql AS $$
    WITH del AS (
        DELETE FROM messaging.message_reactions r USING messaging.messages m
        WHERE r.message_id = m.id AND m.visibility = false RETURNING r.message_id)
    SELECT DISTINCT d.message_id FROM del d
$$;

/* ---------------------------------------------------------------------------
   7.12 MESSAGING — members / inbox
   --------------------------------------------------------------------------- */
/* Member row with SMART RE-COUNT (behaviour kept from v1 — the Go interaction worker relies on it to heal lost
   unread increments). v1 used unqualified column names inside a RETURNS TABLE plpgsql function => "ambiguous column"
   at run time; everything is qualified now. NOTE (review §14): the recount adds messages newer than updated_at to the
   stored counter and is not persisted; kept as-is because its intent cannot be verified from the supplied sources. */
CREATE FUNCTION messaging.func_get_member(p_conv_id bigint, p_user_id bigint)
RETURNS TABLE(id bigint, conversation_id bigint, user_id bigint, role smallint, settings jsonb, joined_at bigint,
              unread_count integer, frozen_message_id bigint, last_read_message_id bigint, created_at bigint, updated_at bigint)
LANGUAGE plpgsql STABLE AS $$
DECLARE
    v_mem messaging.members%ROWTYPE;
    v_new integer;
BEGIN
    SELECT m.* INTO v_mem FROM messaging.members m WHERE m.conversation_id = p_conv_id AND m.user_id = p_user_id;
    IF NOT FOUND THEN RETURN; END IF;

    IF v_mem.unread_count = 0 OR v_mem.updated_at < util.now_ms() - 5000 THEN
        SELECT count(*)::integer INTO v_new FROM messaging.messages msg
        WHERE msg.conversation_id = p_conv_id AND msg.created_at > v_mem.updated_at AND msg.sender_id <> p_user_id;
        v_mem.unread_count := v_mem.unread_count + v_new;
    END IF;

    RETURN QUERY SELECT v_mem.id, v_mem.conversation_id, v_mem.user_id, v_mem.role, v_mem.settings, v_mem.joined_at,
                        v_mem.unread_count, v_mem.frozen_message_id, v_mem.last_read_message_id, v_mem.created_at, v_mem.updated_at;
END;
$$;

CREATE FUNCTION messaging.func_get_conversation_participant_ids(p_conv_id bigint) RETURNS TABLE(user_id bigint)
LANGUAGE sql STABLE AS $$ SELECT m.user_id FROM messaging.members m WHERE m.conversation_id = p_conv_id $$;

-- Pin positions of a user: replacement for the raw SELECT in repository/postgres/func_get_pinned_indices.go
CREATE FUNCTION messaging.func_get_pinned_indices(p_user_id bigint) RETURNS TABLE(pinned integer)
LANGUAGE sql STABLE AS $$
    SELECT (m.settings ->> 'pinned')::integer FROM messaging.members m
    WHERE m.user_id = p_user_id AND (m.settings ->> 'pinned')::integer >= 0
$$;

-- Cold-start seeders. v1 func_load_active_conversations had an untyped `external_link` column => schema failed to load.
CREATE FUNCTION messaging.func_load_active_conversations()
RETURNS TABLE(id bigint, type smallint, title text, description text, avatar_id bigint, last_message_id bigint,
              settings jsonb, external_link jsonb)
LANGUAGE sql STABLE AS $$
    SELECT c.id, c.type, c.title, c.description, c.avatar_id, c.last_message_id, c.settings, c.external_link
    FROM messaging.conversations c WHERE c.state = 0
$$;

CREATE FUNCTION messaging.func_load_active_communities()
RETURNS TABLE(id bigint, name text, description text, avatar_id bigint, member_count integer)
LANGUAGE sql STABLE AS $$
    SELECT c.id, COALESCE(c.title, ''), COALESCE(c.description, ''), COALESCE(c.avatar_id, 0),
           (SELECT count(*)::integer FROM messaging.members m WHERE m.conversation_id = c.id AND m.role >= 0)
    FROM messaging.conversations c WHERE c.type = 3 AND c.state = 0
$$;

-- joined_at is timestamptz on output: func_load_active_members.go scans it into time.Time
CREATE FUNCTION messaging.func_load_active_members()
RETURNS TABLE(conversation_id bigint, user_id bigint, role smallint, settings jsonb, unread_count integer,
              frozen_message_id bigint, last_read_message_id bigint, last_message_id bigint, joined_at timestamptz)
LANGUAGE sql STABLE AS $$
    SELECT m.conversation_id, m.user_id, m.role, m.settings, m.unread_count, m.frozen_message_id,
           m.last_read_message_id, c.last_message_id, util.ms_to_ts(m.joined_at)
    FROM messaging.members m JOIN messaging.conversations c ON c.id = m.conversation_id WHERE c.state = 0
$$;

/* User inbox, newest conversation first. Sort key = COALESCE(last_message_id,0) DESC, conversation id DESC
   (v1: last_message_id DESC NULLS LAST; identical order, now with a deterministic tie-break). */
CREATE FUNCTION messaging.func_load_conversation_paginated(p_user_id bigint, p_limit integer, p_offset integer)
RETURNS TABLE(conv_id bigint, conv_type smallint, conv_title text, conv_description text, conv_avatar_id bigint,
              conv_last_msg_id bigint, conv_state smallint, conv_settings jsonb, external_link jsonb,
              conv_created bigint, conv_updated bigint,
              mem_id bigint, mem_conv_id bigint, mem_user_id bigint, mem_role smallint, mem_settings jsonb,
              mem_joined bigint, mem_frozen_id bigint, mem_last_read_message_id bigint, mem_unread integer,
              mem_created bigint, mem_updated bigint)
LANGUAGE sql STABLE AS $$
    SELECT c.id, c.type, c.title, c.description, c.avatar_id, c.last_message_id, c.state, c.settings, c.external_link,
           c.created_at, c.updated_at,
           m.id, m.conversation_id, m.user_id, m.role, m.settings, m.joined_at, m.frozen_message_id,
           m.last_read_message_id, m.unread_count, m.created_at, m.updated_at
    FROM messaging.members m JOIN messaging.conversations c ON c.id = m.conversation_id
    WHERE m.user_id = p_user_id AND c.state <> 1
    ORDER BY COALESCE(c.last_message_id, 0) DESC, c.id DESC
    LIMIT LEAST(GREATEST(p_limit, 1), 200) OFFSET GREATEST(p_offset, 0)
$$;

-- Keyset: cursor = (sort key, conversation id) of the last row, where sort key = COALESCE(conv_last_msg_id, 0).
CREATE FUNCTION messaging.func_load_conversation_keyset(
    p_user_id bigint, p_before_key bigint DEFAULT NULL, p_before_conv_id bigint DEFAULT NULL, p_limit integer DEFAULT 50)
RETURNS TABLE(conv_id bigint, conv_type smallint, conv_title text, conv_description text, conv_avatar_id bigint,
              conv_last_msg_id bigint, conv_state smallint, conv_settings jsonb, external_link jsonb,
              conv_created bigint, conv_updated bigint,
              mem_id bigint, mem_conv_id bigint, mem_user_id bigint, mem_role smallint, mem_settings jsonb,
              mem_joined bigint, mem_frozen_id bigint, mem_last_read_message_id bigint, mem_unread integer,
              mem_created bigint, mem_updated bigint)
LANGUAGE sql STABLE AS $$
    SELECT c.id, c.type, c.title, c.description, c.avatar_id, c.last_message_id, c.state, c.settings, c.external_link,
           c.created_at, c.updated_at,
           m.id, m.conversation_id, m.user_id, m.role, m.settings, m.joined_at, m.frozen_message_id,
           m.last_read_message_id, m.unread_count, m.created_at, m.updated_at
    FROM messaging.members m JOIN messaging.conversations c ON c.id = m.conversation_id
    WHERE m.user_id = p_user_id AND c.state <> 1
      AND (COALESCE(c.last_message_id, 0), c.id) < (COALESCE(p_before_key, 9223372036854775807), COALESCE(p_before_conv_id, 9223372036854775807))
    ORDER BY COALESCE(c.last_message_id, 0) DESC, c.id DESC
    LIMIT LEAST(GREATEST(p_limit, 1), 100)
$$;

CREATE FUNCTION messaging.func_load_members_by_role_paginated(p_conv_id bigint, p_role smallint, p_limit integer, p_offset integer)
RETURNS TABLE(id bigint, conversation_id bigint, user_id bigint, role smallint, joined_at bigint, unread_count integer,
              frozen_message_id bigint, last_read_message_id bigint, created_at bigint, updated_at bigint)
LANGUAGE sql STABLE AS $$
    SELECT m.id, m.conversation_id, m.user_id, m.role, m.joined_at, m.unread_count, m.frozen_message_id,
           m.last_read_message_id, m.created_at, m.updated_at
    FROM messaging.members m WHERE m.conversation_id = p_conv_id AND m.role = p_role
    ORDER BY m.joined_at DESC, m.id DESC LIMIT LEAST(GREATEST(p_limit, 1), 100) OFFSET GREATEST(p_offset, 0)
$$;

-- Both `> 0` and `> now` appear so the planner can prove the partial-index predicate of idx_members_restricted.
CREATE FUNCTION messaging.func_load_muted_members_paginated(p_conv_id bigint, p_limit integer, p_offset integer)
RETURNS TABLE(user_id bigint, restricted_until bigint)
LANGUAGE sql STABLE AS $$
    SELECT m.user_id, (m.settings ->> 'restricted_until')::bigint
    FROM messaging.members m
    WHERE m.conversation_id = p_conv_id
      AND (m.settings ->> 'restricted_until')::bigint > 0
      AND (m.settings ->> 'restricted_until')::bigint > util.now_ms()
    ORDER BY (m.settings ->> 'restricted_until')::bigint DESC, m.user_id
    LIMIT LEAST(GREATEST(p_limit, 1), 100) OFFSET GREATEST(p_offset, 0)
$$;

/* ---------------------------------------------------------------------------
   7.13 MODERATION
   v1 proc_create_report inserted into non-existent columns (actor_id, target_id): it could never run.
   --------------------------------------------------------------------------- */
CREATE PROCEDURE moderation.proc_create_report(
    IN p_id bigint, IN p_reporter_id bigint, IN p_target_type smallint, IN p_target_ids bigint[],
    IN p_category smallint, IN p_reason text, IN p_state smallint DEFAULT 0, IN p_importance double precision DEFAULT 0.0)
LANGUAGE sql AS $$
    INSERT INTO moderation.reports (id, reporter_id, target_type, target_ids, category, reason, rationale, state, importance)
    VALUES (p_id, p_reporter_id, p_target_type, p_target_ids, p_category, p_reason, NULL, p_state, p_importance)
$$;

CREATE PROCEDURE moderation.proc_update_report(IN p_report_id bigint, IN p_new_state smallint, IN p_new_rationale text)
LANGUAGE sql AS $$
    UPDATE moderation.reports SET state = p_new_state, rationale = p_new_rationale, updated_at = util.now_ms() WHERE id = p_report_id
$$;

CREATE FUNCTION moderation.func_archive_closed_report(
    p_report_id bigint,
    p_state smallint,
    p_rationale text
)
RETURNS boolean
LANGUAGE plpgsql AS $$
BEGIN
    UPDATE moderation.reports
    SET state = p_state,
        rationale = p_rationale,
        updated_at = util.now_ms()
    WHERE id = p_report_id;
    RETURN FOUND;
END;
$$;

CREATE FUNCTION moderation.func_register_stegano_ban(p_user_id bigint, p_stegano_id text, p_reason text,
                                                     p_source_post_id bigint DEFAULT NULL)
RETURNS void
LANGUAGE sql AS $$
    INSERT INTO moderation.stegano_bans (user_id, stegano_id, reason, source_post_id, created_at)
    VALUES (p_user_id, p_stegano_id, p_reason, p_source_post_id, util.now_ms())
    ON CONFLICT (user_id) DO UPDATE
      SET stegano_id = EXCLUDED.stegano_id,
      reason     = EXCLUDED.reason,
      created_at = EXCLUDED.created_at
      $$;

CREATE FUNCTION moderation.func_is_stegano_banned(p_user_id bigint)
RETURNS boolean
LANGUAGE sql STABLE AS $$
    SELECT EXISTS(SELECT 1 FROM moderation.stegano_bans b WHERE b.user_id = p_user_id)
$$;

CREATE FUNCTION moderation.func_load_quarantined_media_paginated(p_limit integer, p_offset integer)
RETURNS TABLE(id bigint, owner_id bigint, storage_path text, visibility smallint, ia_score double precision, created_at bigint, updated_at bigint)
LANGUAGE sql STABLE AS $$
    SELECT m.id, m.owner_id, COALESCE(m.storage_path, ''), m.visibility, COALESCE(m.ia_score, 0.0), m.created_at, m.updated_at
    FROM content.media m
    WHERE m.visibility = 2
    ORDER BY m.ia_score DESC, m.created_at ASC, m.id ASC
    LIMIT LEAST(GREATEST(p_limit, 1), 100) OFFSET GREATEST(p_offset, 0)
$$;

CREATE FUNCTION moderation.func_load_pending_reports_paginated(
    p_limit integer,
    p_offset integer,
    p_origin_filter smallint DEFAULT NULL,
    p_target_type_filter smallint DEFAULT NULL
)
RETURNS TABLE(
    id bigint, reporter_id bigint, report_origin smallint, target_type smallint, target_ids bigint[],
    target_owner_id bigint, author_trust_score double precision, ia_score_max double precision,
    accumulated_report_score double precision, report_count integer, category smallint,
    reason text, rationale text, state smallint, importance double precision,
    created_at bigint, updated_at bigint
)
LANGUAGE sql STABLE AS $$
    SELECT r.id, COALESCE(r.reporter_id, 0), r.report_origin, r.target_type, r.target_ids,
           r.target_owner_id, r.author_trust_score, r.ia_score_max,
           r.accumulated_report_score, r.report_count, r.category,
           COALESCE(r.reason, ''), COALESCE(r.rationale, ''), r.state, r.importance,
           r.created_at, r.updated_at
    FROM moderation.reports r
    WHERE r.state = 0
      AND (p_origin_filter IS NULL OR r.report_origin = p_origin_filter)
      AND (p_target_type_filter IS NULL OR r.target_type = p_target_type_filter)
    ORDER BY r.importance DESC, r.created_at ASC
    LIMIT LEAST(GREATEST(p_limit, 1), 100) OFFSET GREATEST(p_offset, 0)
$$;

/* ---------------------------------------------------------------------------
   7.14 VIEWS schema — read projections
   post_engagement_view now READS the denormalised counters instead of re-aggregating likes/comments per row.
   Consistency model: counters are worker-maintained (eventually consistent, drift repairable with
   content.func_reconcile_post_counters). conversation_summary lost a comma in v1 (syntax error).
   --------------------------------------------------------------------------- */
CREATE VIEW views.user_public_profile AS
SELECT u.id AS user_id, u.username, u.first_name, u.last_name, u.birthdate, u.sex, u.bio, u.profile_picture_id,
       u.grade, u.location, u.school, u.work, u.badges
FROM auth.users u;

CREATE VIEW views.user_relations_view AS
SELECT r.id AS relation_id, r.primary_id AS follower_id, u1.username AS follower_username,
       r.secondary_id AS followed_id, u2.username AS followed_username, r.state, r.created_at
FROM auth.relations r JOIN auth.users u1 ON r.primary_id = u1.id JOIN auth.users u2 ON r.secondary_id = u2.id;

CREATE VIEW views.conversation_participants_view AS
SELECT cm.conversation_id, cm.user_id, u.username, u.first_name, u.last_name, cm.role, cm.settings, cm.joined_at,
       cm.unread_count, cm.last_read_message_id, conv.type AS conversation_type, conv.title AS conversation_title,
       conv.description AS conversation_description, conv.avatar_id AS conversation_avatar_id,
       conv.state AS conversation_state, conv.created_at AS conversation_created_at
FROM messaging.members cm JOIN messaging.conversations conv ON cm.conversation_id = conv.id JOIN auth.users u ON cm.user_id = u.id;

CREATE VIEW views.conversation_summary AS
SELECT cm.conversation_id, cm.user_id, cm.role, cm.settings, cm.joined_at, cm.unread_count,
       last_msg.id AS last_message_id, last_msg.sender_id AS last_sender_id, last_msg.message_type AS last_message_type,
       last_msg.content AS last_message_content, last_msg.created_at AS last_message_time
FROM messaging.members cm
LEFT JOIN LATERAL (
    SELECT m.id, m.sender_id, m.message_type, m.content, m.created_at
    FROM messaging.messages m
    WHERE m.conversation_id = cm.conversation_id AND (cm.frozen_message_id IS NULL OR m.id <= cm.frozen_message_id)
    ORDER BY m.created_at DESC LIMIT 1) last_msg ON true;

CREATE VIEW views.conversation_user_view AS
SELECT cm.user_id, array_agg(cm.conversation_id ORDER BY last_msg.created_at DESC NULLS LAST) AS conversation_ids
FROM messaging.members cm
LEFT JOIN LATERAL (
    SELECT m.created_at FROM messaging.messages m
    WHERE m.conversation_id = cm.conversation_id AND m.sender_id <> cm.user_id
      AND (cm.frozen_message_id IS NULL OR m.id <= cm.frozen_message_id)
    ORDER BY m.created_at DESC LIMIT 1) last_msg ON true
GROUP BY cm.user_id;

CREATE VIEW views.post_engagement_view AS
SELECT p.id AS post_id, p.user_id, p.content, p.media_ids, p.visibility, p.location, p.created_at, p.updated_at,
       p.vector, p.vector_version, p.report_count,
       p.like_count::bigint AS like_count, p.comment_count::bigint AS comment_count
FROM content.posts p;

/* Dynamic uniqueness probe. %I quoting makes it injection-safe; kept as-is (callers pass an entity->table map). */
CREATE FUNCTION views.func_check_unique(p_schema text, p_table text, p_field text, p_value text) RETURNS boolean
LANGUAGE plpgsql STABLE AS $$
DECLARE v_exists boolean;
BEGIN
    EXECUTE format('SELECT EXISTS(SELECT 1 FROM %I.%I WHERE %I = $1)', p_schema, p_table, p_field) INTO v_exists USING p_value;
    RETURN v_exists;
END;
$$;

/* =============================================================================
   SECTION 8 — views.func_* wrappers (unreferenced by the supplied Go sources; retained, types aligned)
   ============================================================================= */
CREATE FUNCTION views.func_load_user_public_profile(p_user_id bigint DEFAULT NULL)
RETURNS TABLE(user_id bigint, username text, first_name text, last_name text, birthdate bigint, sex smallint, bio text,
              profile_picture_id bigint, grade smallint, location text, school text, work text, badges jsonb)
LANGUAGE sql STABLE AS $$ SELECT * FROM views.user_public_profile v WHERE p_user_id IS NULL OR v.user_id = p_user_id $$;

CREATE FUNCTION views.func_load_user_relations_view(p_follower_id bigint DEFAULT NULL, p_followed_id bigint DEFAULT NULL)
RETURNS TABLE(relation_id bigint, follower_id bigint, follower_username text, followed_id bigint, followed_username text,
              state smallint, created_at bigint)
LANGUAGE sql STABLE AS $$
    SELECT * FROM views.user_relations_view v
    WHERE (p_follower_id IS NULL OR v.follower_id = p_follower_id) AND (p_followed_id IS NULL OR v.followed_id = p_followed_id)
$$;

CREATE FUNCTION views.func_load_conversation_participants_view(p_conversation_id bigint DEFAULT NULL, p_user_id bigint DEFAULT NULL)
RETURNS TABLE(conversation_id bigint, user_id bigint, username text, first_name text, last_name text, role smallint,
              settings jsonb, joined_at bigint, unread_count integer, last_read_message_id bigint, conversation_type smallint,
              conversation_title text, conversation_description text, conversation_avatar_id bigint,
              conversation_state smallint, conversation_created_at bigint)
LANGUAGE sql STABLE AS $$
    SELECT * FROM views.conversation_participants_view v
    WHERE (p_conversation_id IS NULL OR v.conversation_id = p_conversation_id) AND (p_user_id IS NULL OR v.user_id = p_user_id)
$$;

CREATE FUNCTION views.func_load_conversation_summary(p_conversation_id bigint DEFAULT NULL, p_user_id bigint DEFAULT NULL)
RETURNS TABLE(conversation_id bigint, user_id bigint, role smallint, settings jsonb, joined_at bigint, unread_count integer,
              last_message_id bigint, last_sender_id bigint, last_message_type smallint, last_message_content text,
              last_message_time bigint)
LANGUAGE sql STABLE AS $$
    SELECT * FROM views.conversation_summary s
    WHERE (p_conversation_id IS NULL OR s.conversation_id = p_conversation_id) AND (p_user_id IS NULL OR s.user_id = p_user_id)
$$;

CREATE FUNCTION views.func_load_conversation_user_view(p_user_id bigint DEFAULT NULL)
RETURNS TABLE(user_id bigint, conversation_ids bigint[])
LANGUAGE sql STABLE AS $$ SELECT * FROM views.conversation_user_view v WHERE p_user_id IS NULL OR v.user_id = p_user_id $$;

CREATE FUNCTION views.func_load_post_engagement_view(p_post_id bigint DEFAULT NULL, p_user_id bigint DEFAULT NULL)
RETURNS TABLE(post_id bigint, user_id bigint, content text, media_ids bigint[], visibility smallint, location text,
              created_at bigint, updated_at bigint, vector real[], vector_version integer, report_count integer,
              like_count bigint, comment_count bigint)
LANGUAGE sql STABLE AS $$
    SELECT * FROM views.post_engagement_view v
    WHERE (p_post_id IS NULL OR v.post_id = p_post_id) AND (p_user_id IS NULL OR v.user_id = p_user_id)
$$;
/* =============================================================================
   SECTION 9 — LEGACY COMPATIBILITY (remove after the Go repositories adopt the explicit API; review §12 step 12)
   9a  Catch-all entry points STILL CALLED by the current repository. Same name/args, deterministic dispatch to the
       explicit functions above. Behaviour change common to all: when EVERY filter is NULL they return NO ROWS
       (v1 returned the entire table).
   ============================================================================= */

/* func_load_user.go: SELECT * FROM auth.func_load_user($1,$2,$3,$4). First non-NULL key picks the index, the rest filter. */
CREATE FUNCTION auth.func_load_user(
    p_id bigint DEFAULT NULL, p_username text DEFAULT NULL, p_email text DEFAULT NULL, p_phone text DEFAULT NULL)
RETURNS SETOF auth.users_l3
LANGUAGE plpgsql STABLE AS $$
BEGIN
    IF p_id IS NOT NULL THEN
        RETURN QUERY SELECT u.* FROM auth.func_get_user_by_id(p_id) u
                     WHERE (p_username IS NULL OR u.username = btrim(p_username))
                       AND (p_email IS NULL OR lower(btrim(u.email)) = lower(btrim(p_email)))
                       AND (p_phone IS NULL OR u.phone = btrim(p_phone));
    ELSIF p_username IS NOT NULL THEN
        RETURN QUERY SELECT u.* FROM auth.func_get_user_by_username(p_username) u
                     WHERE (p_email IS NULL OR lower(btrim(u.email)) = lower(btrim(p_email)))
                       AND (p_phone IS NULL OR u.phone = btrim(p_phone));
    ELSIF p_email IS NOT NULL THEN
        RETURN QUERY SELECT u.* FROM auth.func_get_user_by_email(p_email) u
                     WHERE (p_phone IS NULL OR u.phone = btrim(p_phone));
    ELSIF p_phone IS NOT NULL THEN
        RETURN QUERY SELECT u.* FROM auth.func_get_user_by_phone(p_phone) u;
    END IF;
END;
$$;

/* func_load_session.go / func_load_all_user_sessions.go. Unlike v1, a device id no longer bypasses the other filters. */
CREATE FUNCTION auth.func_load_sessions(
    p_id bigint DEFAULT NULL, p_user_id bigint DEFAULT NULL,
    p_firebase_installation_id text DEFAULT NULL, p_master_token text DEFAULT NULL)
RETURNS SETOF auth.sessions_l3
LANGUAGE plpgsql STABLE AS $$
BEGIN
    IF p_id IS NOT NULL THEN
        RETURN QUERY SELECT s.* FROM auth.func_get_session_by_id(p_id) s
                     WHERE (p_user_id IS NULL OR s.user_id = p_user_id)
                       AND (p_firebase_installation_id IS NULL OR s.firebase_installation_id = p_firebase_installation_id)
                       AND (p_master_token IS NULL OR s.master_token = p_master_token);
    ELSIF p_user_id IS NOT NULL AND p_firebase_installation_id IS NOT NULL THEN
        RETURN QUERY SELECT s.* FROM auth.func_get_session_by_user_device(p_user_id, p_firebase_installation_id) s
                     WHERE (p_master_token IS NULL OR s.master_token = p_master_token);
    ELSIF p_master_token IS NOT NULL THEN
        RETURN QUERY SELECT s.* FROM auth.func_get_session_by_master_token(p_master_token) s
                     WHERE (p_user_id IS NULL OR s.user_id = p_user_id)
                       AND (p_firebase_installation_id IS NULL OR s.firebase_installation_id = p_firebase_installation_id);
    ELSIF p_firebase_installation_id IS NOT NULL THEN
        RETURN QUERY SELECT s.* FROM auth.func_get_latest_session_by_device(p_firebase_installation_id) s;
    ELSIF p_user_id IS NOT NULL THEN
        RETURN QUERY SELECT s.* FROM auth.func_list_sessions_by_user(p_user_id) s;
    END IF;
END;
$$;

/* func_load_user_settings.go: called as (NULL, user_id). */
CREATE FUNCTION auth.func_load_user_settings(p_id bigint DEFAULT NULL, p_user_id bigint DEFAULT NULL)
RETURNS TABLE(id bigint, user_id bigint, privacy jsonb, notifications jsonb, display_and_content jsonb,
              telemetry_vector real[], telemetry_tags text[], telemetry_timestamp bigint)
LANGUAGE plpgsql STABLE AS $$
BEGIN
    IF p_user_id IS NOT NULL THEN
        RETURN QUERY SELECT g.* FROM auth.func_get_user_settings(p_user_id) g WHERE (p_id IS NULL OR g.id = p_id);
    ELSIF p_id IS NOT NULL THEN
        RETURN QUERY SELECT s.id, s.user_id, s.privacy, s.notifications, s.display_and_content,
                            s.telemetry_vector, s.telemetry_tags, COALESCE(s.telemetry_timestamp, 0)
                     FROM auth.user_settings s WHERE s.id = p_id;
    END IF;
END;
$$;

/* func_load_posts.go calls (NULL, $ids, NULL, 0) and expects "all hydratable posts". In v1 an explicit NULL
   p_visibility did NOT select the DEFAULT array: `visibility = ANY(NULL)` is NULL, so the call returned ZERO rows.
   Now NULL = any live visibility (0/1/2). Default of p_visibility changed ARRAY[0,1] -> NULL accordingly. */
CREATE FUNCTION content.func_load_posts(
    p_user_id bigint DEFAULT NULL, p_post_ids bigint[] DEFAULT NULL,
    p_visibility smallint[] DEFAULT NULL, p_order_mode integer DEFAULT 0)
RETURNS SETOF content.posts_l3
LANGUAGE plpgsql STABLE AS $$
BEGIN
    IF p_order_mode = 1 THEN
        RETURN QUERY SELECT * FROM content.posts_l3 v
                     WHERE v.visibility >= 0 AND (p_user_id IS NULL OR v.user_id = p_user_id)
                       AND (p_post_ids IS NULL OR v.id = ANY(p_post_ids))
                       AND (p_visibility IS NULL OR v.visibility = ANY(p_visibility))
                     ORDER BY v.created_at ASC, v.id ASC;
    ELSE
        RETURN QUERY SELECT * FROM content.posts_l3 v
                     WHERE v.visibility >= 0 AND (p_user_id IS NULL OR v.user_id = p_user_id)
                       AND (p_post_ids IS NULL OR v.id = ANY(p_post_ids))
                       AND (p_visibility IS NULL OR v.visibility = ANY(p_visibility))
                     ORDER BY v.created_at DESC, v.id DESC;
    END IF;
END;
$$;

/* func_load_likes.go: filters by target_type/target_id/user_id, newest(0)/oldest(1) first. created_at stays timestamptz. */
CREATE FUNCTION content.func_load_likes(
    p_target_type smallint DEFAULT NULL, p_target_id bigint DEFAULT NULL, p_user_id bigint DEFAULT NULL,
    p_limit integer DEFAULT 100, p_order_mode smallint DEFAULT 0)
RETURNS TABLE(id bigint, target_type smallint, target_id bigint, user_id bigint, created_at timestamptz)
LANGUAGE plpgsql STABLE AS $$
BEGIN
    RETURN QUERY
    SELECT l.id, l.target_type, l.target_id, l.user_id, l.created_at FROM content.likes l
    WHERE (p_target_type IS NULL OR l.target_type = p_target_type)
      AND (p_target_id   IS NULL OR l.target_id   = p_target_id)
      AND (p_user_id     IS NULL OR l.user_id     = p_user_id)
    ORDER BY CASE WHEN p_order_mode = 0 THEN l.created_at END DESC, CASE WHEN p_order_mode = 1 THEN l.created_at END ASC
    LIMIT LEAST(GREATEST(p_limit, 1), 1000);
END;
$$;

/* ---------------------------------------------------------------------------
   9a' Functions with NO caller in the supplied Go sources. Retained (a service outside the supplied tree may use
   them); types aligned with the new storage. Candidates for removal after `track_functions = 'all'` shows zero calls.
   --------------------------------------------------------------------------- */
CREATE FUNCTION auth.func_load_relations(
    p_id bigint DEFAULT NULL, p_primary_id bigint DEFAULT NULL, p_secondary_id bigint DEFAULT NULL, p_state smallint DEFAULT NULL)
RETURNS TABLE(id bigint, primary_id bigint, secondary_id bigint, state smallint, created_at bigint)
LANGUAGE sql STABLE AS $$
    SELECT r.id, r.primary_id, r.secondary_id, r.state, r.created_at FROM auth.relations r
    WHERE (p_id IS NULL OR r.id = p_id) AND (p_primary_id IS NULL OR r.primary_id = p_primary_id)
      AND (p_secondary_id IS NULL OR r.secondary_id = p_secondary_id) AND (p_state IS NULL OR r.state = p_state)
$$;

CREATE FUNCTION content.func_load_comments(
    p_post_id bigint DEFAULT NULL, p_user_id bigint DEFAULT NULL, p_limit integer DEFAULT 100, p_order_mode smallint DEFAULT 0)
RETURNS TABLE(id bigint, post_id bigint, user_id bigint, content text, visibility integer, like_count integer,
              score integer, created_at timestamptz, updated_at timestamptz)
LANGUAGE sql STABLE AS $$
    SELECT c.id, c.post_id, c.user_id, c.content, c.visibility, c.like_count, c.score, c.created_at, c.updated_at
    FROM content.comments c
    WHERE c.visibility = 0 AND (p_post_id IS NULL OR c.post_id = p_post_id) AND (p_user_id IS NULL OR c.user_id = p_user_id)
    ORDER BY CASE WHEN p_order_mode = 0 THEN c.created_at END DESC, CASE WHEN p_order_mode = 1 THEN c.score END DESC,
             CASE WHEN p_order_mode = 2 THEN c.created_at END ASC
    LIMIT LEAST(GREATEST(p_limit, 1), 1000)
$$;

CREATE FUNCTION content.func_load_media(p_owner_id bigint DEFAULT NULL, p_media_ids bigint[] DEFAULT NULL,
                                        p_order_mode smallint DEFAULT 0)
RETURNS TABLE(id bigint, owner_id bigint, storage_path text, visibility smallint, created_at bigint)
LANGUAGE sql STABLE AS $$
    SELECT m.id, m.owner_id, m.storage_path, m.visibility, m.created_at FROM content.media m
    WHERE m.visibility = 1 AND (p_owner_id IS NULL OR m.owner_id = p_owner_id)
                           AND (p_media_ids IS NULL OR m.id = ANY(p_media_ids))
    ORDER BY CASE WHEN p_order_mode = 0 THEN m.created_at END DESC, CASE WHEN p_order_mode = 1 THEN m.created_at END ASC
$$;

CREATE FUNCTION messaging.func_load_conversation(p_user_id bigint)
RETURNS TABLE(conversation_id bigint, title text, description text, avatar_id bigint, type smallint, conversation_settings jsonb,
              external_link jsonb, state smallint, created_at bigint, last_message_id bigint, joined_at bigint,
              frozen_message_id bigint, last_read_message_id bigint, role smallint, settings jsonb, unread_count integer)
LANGUAGE sql STABLE AS $$
    SELECT c.id, c.title, c.description, c.avatar_id, c.type, c.settings, c.external_link, c.state, c.created_at,
           c.last_message_id, m.joined_at, m.frozen_message_id, m.last_read_message_id, m.role, m.settings, m.unread_count
    FROM messaging.members m JOIN messaging.conversations c ON c.id = m.conversation_id
    WHERE m.user_id = p_user_id AND c.state <> 1
$$;

CREATE FUNCTION messaging.func_load_members(p_conversation_id bigint)
RETURNS TABLE(user_id bigint, username text, grade smallint, banned boolean, desactivated boolean,
              profile_picture_storage_path text, role smallint, settings jsonb, joined_at bigint, unread_count integer)
LANGUAGE sql STABLE AS $$
    SELECT u.id, u.username, u.grade, u.banned, u.desactivated, med.storage_path, m.role, m.settings, m.joined_at, m.unread_count
    FROM messaging.members m JOIN auth.users u ON u.id = m.user_id LEFT JOIN content.media med ON med.id = u.profile_picture_id
    WHERE m.conversation_id = p_conversation_id
$$;

CREATE FUNCTION messaging.func_load_messages(p_conversation_id bigint) RETURNS SETOF messaging.messages
LANGUAGE sql STABLE AS $$
    SELECT * FROM messaging.messages WHERE conversation_id = p_conversation_id AND visibility = true ORDER BY created_at ASC
$$;

/* =============================================================================
   SECTION 9b — OPTIONAL: ASSIGNMENT CAST timestamptz -> bigint
   worker/postgres_batch.go runs `UPDATE content.media|messaging.messages SET updated_at = NOW()` while the very same
   columns are COPY-loaded with epoch-ms integers (D1). A plain column type cannot satisfy both. ASSIGNMENT casts apply
   only to INSERT/UPDATE target conversion (never to expressions/comparisons), so this makes NOW() land as epoch-ms.
   Needs superuser (owner of the built-in types). On managed PostgreSQL where it is refused, the block only RAISES A
   NOTICE: message soft-delete and the media cascade then require the Go change documented in review §10 (WB-2).
   ============================================================================= */
DO $$
BEGIN
    CREATE CAST (timestamptz AS bigint) WITH FUNCTION util.ts_to_ms(timestamptz) AS ASSIGNMENT;
EXCEPTION
    WHEN insufficient_privilege THEN
        RAISE NOTICE 'numan: CREATE CAST (timestamptz AS bigint) refused (%). See review.md WB-2.', SQLERRM;
    WHEN duplicate_object THEN
        NULL;
END;
$$;

/* =============================================================================
   END OF SCHEMA
   ============================================================================= */
