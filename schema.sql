/* =============================================================================
   PROJECT DATABASE SCHEMA — FULL REFACTORED & DOCUMENTED VERSION
   =============================================================================
   Source dump  : pg_dump 16.10  /  PostgreSQL 16.11 (Debian)
   Refactored on: 2026-06-05
   Description  : Social-platform database covering user authentication,
                  social-graph relations, content publishing, direct messaging,
                  content moderation, and aggregated read-optimised views.

   EXECUTION ORDER (dependency-safe):
     1. Session configuration
     2. Schema declarations
     3. Tables           (base → dependent, respects all FK chains)
     4. Functions        (STABLE read functions + mutable procedures)
     5. Views            (depend on tables; functions that wrap views come last)
     6. Constraints      (PKs, UNIQUEs, FKs — after all objects exist)
     7. Indexes          (last, for fastest initial bulk load)

   BUGS FIXED VS ORIGINAL DUMP:
     • auth.func_get_relation_state — stray '<' character removed from DECLARE
       block; column names corrected: caller_id→primary_id, target_id→secondary_id
       (matching the actual auth.relations schema).
     • content.func_load_comments — return column type corrected:
       visibility was declared BOOLEAN but the table column is INTEGER.
     • views.func_load_user_public_profile — return column type corrected:
       theme was declared TEXT but auth.user_settings.theme is SMALLINT.
     • All functions that referenced tables/views before those objects were
       created have been repositioned to the correct section.
   ============================================================================= */


/* =============================================================================
   SECTION 0 — SESSION CONFIGURATION
   Restore the pg_dump session settings for a clean, predictable import.
   ============================================================================= */

SET statement_timeout                 = 0;
SET lock_timeout                      = 0;
SET idle_in_transaction_session_timeout = 0;
SET client_encoding                   = 'UTF8';
SET standard_conforming_strings       = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies             = false;
SET xmloption                         = content;
SET client_min_messages               = warning;
SET row_security                      = off;


/* =============================================================================
   SECTION 1 — SCHEMAS
   Five logical namespaces that partition the application domain:
     • auth       — identity, authentication, sessions, social-graph relations
     • content    — posts, comments, media, likes, hashtag taxonomy
     • messaging  — real-time conversations, participants, messages
     • moderation — abuse reports, legal-hold records
     • views      — pre-joined, read-optimised aggregation views
   ============================================================================= */

-- Identity and authentication namespace
CREATE SCHEMA auth;

-- User-generated content namespace (posts, comments, media, likes, tags)
CREATE SCHEMA content;

-- Direct messaging namespace (conversations, members, messages)
CREATE SCHEMA messaging;

-- Trust & safety / moderation namespace
CREATE SCHEMA moderation;

-- Aggregated read views (joins across schemas, exposed via thin wrapper functions)
CREATE SCHEMA views;

-- Set the default storage method for all subsequent tables
SET default_tablespace        = '';
SET default_table_access_method = heap;


/* =============================================================================
   SECTION 2 — TABLES
   Created in strict dependency order so that every referenced table exists
   before the referencing table is declared.  Foreign-key constraints are
   deferred to Section 5 to keep this section clean and to allow circular or
   cross-schema references without ordering problems.
   ============================================================================= */

/* ---------------------------------------------------------------------------
   2.1  AUTH SCHEMA — Users, relations, settings, sessions
   --------------------------------------------------------------------------- */

/*
 * auth.users
 * ----------
 * Central identity record for every platform account.  All other tables that
 * reference a person ultimately point back here via a bigint foreign key.
 *
 * Notable design decisions:
 *   • username, email, phone each carry a UNIQUE constraint (enforced below).
 *   • profile_picture_id is a soft reference to content.media; we leave it as
 *     a plain bigint (no FK) to avoid a cross-schema circular dependency with
 *     content.media, which itself references auth.users.
 *   • grade    — user tier / trust level (application-defined enum encoded as
 *                smallint: e.g. 0=basic, 1=verified, 2=moderator …).
 *   • badges   — jsonb map of awarded achievement slugs to structured metadata.
 *   • ban_*    — soft-ban support: banned flag + optional expiry timestamp.
 *   • desactivated — soft-delete / account suspension flag.
 */
CREATE TABLE auth.users (
    id                  bigint                   NOT NULL,
    username            text                     NOT NULL,
    email               text                     NOT NULL,
    email_verified      boolean,
    phone               text,
    phone_verified      boolean,
    password_hash       text                     NOT NULL,
    first_name          text                     NOT NULL,
    last_name           text                     NOT NULL,
    birthdate           date,
    sex                 smallint,                -- e.g. 0=unspecified, 1=male, 2=female
    bio                 text,
    profile_picture_id  bigint,                  -- soft ref → content.media.id (no FK; see note)
    grade               smallint                 NOT NULL,
    location            text,
    school              text,
    work                text,
    badges              jsonb                    DEFAULT '{}'::jsonb,
    desactivated        boolean,
    banned              boolean,
    ban_reason          text,
    ban_expires_at      timestamp with time zone,
    created_at          timestamp with time zone,
    updated_at          timestamp with time zone
);


/*
 * auth.relations
 * --------------
 * Directed social-graph edge between two users (follow / friend request).
 *
 *   primary_id   — the user who initiated the relation (follower / requester).
 *   secondary_id — the user who is the target (followed / requested).
 *   state        — relationship lifecycle: e.g. 0=pending, 1=accepted,
 *                  2=blocked, 3=rejected (application-defined).
 *   UNIQUE(secondary_id, primary_id) prevents duplicate edges.
 */
CREATE TABLE auth.relations (
    id            bigint                   NOT NULL,
    primary_id    bigint,                  -- FK → auth.users.id
    secondary_id  bigint,                  -- FK → auth.users.id
    state         smallint,
    created_at    timestamp with time zone,
    updated_at    timestamp with time zone
);


/*
 * auth.user_settings
 * ------------------
 * One-to-one extension of auth.users storing personalisation and privacy
 * preferences as JSONB blobs, allowing flexible schema evolution without
 * schema migrations for every new setting key.
 *
 *   privacy       — JSONB: who can see profile, posts, online status, etc.
 *   notifications — JSONB: push / email / SMS toggle preferences.
 *   theme         — UI colour theme (0=system, 1=light, 2=dark, …).
 */
CREATE TABLE auth.user_settings (
    id                  bigint                   NOT NULL,
    user_id             bigint,                  -- FK → auth.users.id (1-to-1, UNIQUE enforced below)
    privacy             jsonb                    DEFAULT '{}'::jsonb,
    notifications       jsonb                    DEFAULT '{}'::jsonb,
    display_and_content jsonb                    DEFAULT '{}'::jsonb, -- ✅ REMPLACE language et theme
    telemetry_vector    real[],                  -- NOUVEAU : Vecteur d'affinité
    telemetry_tags      text[],                  -- NOUVEAU : Top tags de l'utilisateur
    telemetry_timestamp bigint,                  -- NOUVEAU : Version du vecteur (Client Timestamp)
    created_at          timestamp with time zone,
    updated_at          timestamp with time zone
);


/*
 * auth.sessions
 * -------------
 * Active device sessions for authenticated users.  A user may hold multiple
 * concurrent sessions (one per device), identified by the (user_id, firebase_installation_id)
 * pair which carries a UNIQUE index.
 *
 *   master_token  — long-lived refresh credential (stored hashed server-side).
 *   firebase_installation_id  — stable per-device identifier sent by the client.
 *   device_info   — JSONB: OS, app version, hardware model, etc.
 *   ip_history    — inet[] rolling log of source IP addresses.
 *   current_secret / last_secret — rotating JWT signing secrets.
 *   last_jwt      — cached last-issued JWT for quick re-validation.
 *   tolerance_time — deadline after which the old secret is no longer accepted
 *                    during a rotation grace period.
 */
CREATE TABLE auth.sessions (
    id                              bigint                   NOT NULL,
    user_id                         bigint                   NOT NULL,  -- FK → auth.users.id
    master_token                    text                     NOT NULL,
    firebase_installation_id        text                     NOT NULL,
    device_info                     jsonb,
    ip_history                      inet[],
    current_secret                  text,
    last_secret                     text,
    last_jwt                        text,
    tolerance_time                  timestamp with time zone,
    created_at                      timestamp with time zone,
    expires_at                      timestamp with time zone
);


/* ---------------------------------------------------------------------------
   2.2  CONTENT SCHEMA — Posts, comments, media, likes, tag taxonomy
   --------------------------------------------------------------------------- */

/*
 * content.posts
 * -------------
 * The primary unit of user-generated content on the platform.
 *
 *   hashtags      — text[] of normalised tag slugs (also indexed via GIN).
 *   identifiers   — bigint[] of tagged user IDs within the post (mentions).
 *   media_ids     — bigint[] of attached content.media IDs (ordered).
 *   visibility    — 0=public, 1=followers-only, 2=deleted (soft-delete sentinel).
 *   like_count    — denormalised counter, updated by application logic or triggers.
 *   comment_count — denormalised counter.
 *   view_count    — denormalised impression counter.
 *   has_media     — pre-computed boolean flag to avoid array-length checks.
 *   vector        — real[] embedding vector for similarity / recommendation.
 *   vector_version — schema version of the embedding model used.
 */
CREATE TABLE content.posts (
   id              bigint                   NOT NULL,
   user_id         bigint                   NOT NULL,
   content         text,
   hashtags        text[],
   indirect_hashtags text[],                -- ✅ NOUVEAU
   identifiers     bigint[],
   media_ids       bigint[],
   visibility      smallint,
   priority_level  smallint                DEFAULT 0 NOT NULL,
   location        text,
   like_count      integer                  DEFAULT 0,
   comment_count   integer                  DEFAULT 0,
   view_count      integer                  DEFAULT 0,
   report_count    integer                  DEFAULT 0,
   has_media       boolean                  DEFAULT false,
   vector          real[],
   vector_version  integer                  DEFAULT 1,
   telemetry_dwell_sum double precision         DEFAULT 0.0,
   telemetry_dwell_sq  double precision         DEFAULT 0.0,
   telemetry_clicks    integer                  DEFAULT 0,
   created_at      timestamp with time zone,
   updated_at      timestamp with time zone
);


/*
 * content.comments
 * ----------------
 * User comments attached to a specific post.
 *
 *   visibility — 0=visible, -1=deleted (soft-delete), other values reserved.
 *   like_count — denormalised counter maintained by the application.
 *   ON DELETE CASCADE on post_id: deleting a post auto-removes its comments.
 */
CREATE TABLE content.comments (
    id          bigint                   NOT NULL,
    post_id     bigint                   NOT NULL,  -- FK → content.posts.id  (CASCADE)
    user_id     bigint                   NOT NULL,  -- FK → auth.users.id
    content     text                     NOT NULL,
    visibility  integer                  DEFAULT 0,
    like_count  integer                  DEFAULT 0,
    score       integer                  DEFAULT 0,
    created_at  timestamp with time zone DEFAULT now(),
    updated_at  timestamp with time zone DEFAULT now()
);


/*
 * content.media
 * -------------
 * Binary asset registry.  Actual files are stored in an external object-store
 * (e.g. S3 / GCS); this table holds only the metadata and the storage path.
 *
 *   storage_path — relative or absolute path within the object store bucket.
 *   visibility   — false = soft-deleted / hidden; queries filter on TRUE.
 *   owner_id     — FK → auth.users.id (uploader).
 */
CREATE TABLE content.media (
    id            bigint                   NOT NULL,
    owner_id      bigint,                  -- FK → auth.users.id
    storage_path  text,
    visibility    boolean,
    created_at    timestamp with time zone,
    updated_at    timestamp with time zone
);


/*
 * content.likes
 * -------------
 * Polymorphic "like" / reaction table.  A single table handles reactions
 * on multiple entity types using a type discriminator.
 *
 *   target_type — 0 = post, 1 = media, 2 = comment (application-defined enum).
 *   target_id   — PK of the liked entity within its own table.
 *   UNIQUE(target_type, target_id, user_id) — one like per user per entity.
 */
CREATE TABLE content.likes (
    id           bigint                   NOT NULL,
    target_type  smallint                 NOT NULL,
    target_id    bigint                   NOT NULL,
    user_id      bigint,                  -- FK → auth.users.id
    created_at   timestamp with time zone
);

/*
 * content.saved
 * -------------
 * Registre des posts sauvegardés (signets/favoris) par les utilisateurs.
 * UNIQUE(user_id, post_id) garantit qu'un utilisateur ne peut sauvegarder un post qu'une seule fois.
 */
CREATE TABLE content.saved (
   id         bigint                   NOT NULL,
   user_id    bigint                   NOT NULL,
   post_id    bigint                   NOT NULL,
   created_at timestamp with time zone DEFAULT now()
);


/*
 * content.tags
 * ------------
 * Canonical hashtag registry ("source of truth" for all slugs used in posts).
 * The slug is the PRIMARY KEY (declared inline — no separate constraint needed).
 *
 *   slug         — normalised hashtag string (e.g. "travel", "foodie").
 *   is_community — true = community-created tag; false = system/curated tag.
 */
CREATE TABLE content.tags (
    slug         text                     PRIMARY KEY,
    created_at   timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    is_community boolean                  DEFAULT true
);


/* ---------------------------------------------------------------------------
   2.3  MESSAGING SCHEMA — Conversations, members, messages
   --------------------------------------------------------------------------- */

/*
 * messaging.conversations
 * -----------------------
 * A conversation thread, which may be a 1-to-1 DM or a group chat.
 *
 *   type  — 0=direct, 1=group (application-defined).
 *   laws  — smallint[] array of permission/moderation rule flags for the group.
 *   state — 0=active, 1=archived/deleted (queries filter out state=1).
 *   last_message_id           — denormalised pointer to the newest message.
 *   last_read_by_all_message_id — highest message seen by all participants
 *                                 (used to render "everyone has read" receipts).
 */
CREATE TABLE messaging.conversations (
     id                           bigint                   NOT NULL,
     type                         smallint,
     title                        text,
     description                  text,
     avatar_id                    bigint,
     last_message_id              bigint,
     state                        smallint,
     settings                     jsonb                    DEFAULT '{}'::jsonb,
     external_link                jsonb                    DEFAULT '{}'::jsonb, -- ✅ NOUVEAU
     created_at                   timestamp with time zone,
     updated_at                   timestamp with time zone
);


/*
 * messaging.messages
 * ------------------
 * Individual messages within a conversation.
 *
 *   message_type — 0=text, 1=media, 2=system-event, … (application-defined).
 *   visibility   — false = soft-deleted; queries filter on TRUE only.
 *   attachments  — JSONB array of rich attachment metadata (URLs, thumbnails,
 *                  link previews, etc.).
 *
 * NOTE: This table is created before messaging.members intentionally because
 * the views.conversation_summary view uses a LATERAL join that references
 * messaging.messages, and messaging.members (which also pre-dates some views)
 * references messaging.conversations — the order is correct as written.
 */
CREATE TABLE messaging.messages (
    id               bigint                   NOT NULL,
    conversation_id  bigint,                  -- FK → messaging.conversations.id
    sender_id        bigint                   NOT NULL,  -- soft ref → auth.users.id
    message_type     smallint                 NOT NULL, -- 0=text, 1=vocal, 2=image, 3=gif, 4=video, 5=publication, 6=invite, 7=link, 8=system-event, 9=survey, 10=widget
    visibility       boolean,
    content          text,
    attachments      jsonb                    DEFAULT '{}'::jsonb,
    allow_new_reactions boolean               NOT NULL DEFAULT TRUE,
    created_at       timestamp with time zone,
    updated_at       timestamp with time zone
);

/*
 * messaging.message_reactions
 * ---------------------------
 * Nouvelle table dédiée aux réactions sur les messages pour éviter le bloat JSONB.
 *
 *   message_id - FK → messaging.messages.id (ON DELETE CASCADE)
 *   user_id    - FK → auth.users.id
 *   reaction   - L'emoji ou la chaîne de la réaction (ex: "❤️")
 *
 * Contrainte UNIQUE(message_id, user_id) garantit qu'un utilisateur n'a qu'une réaction par message.
 */
CREATE TABLE messaging.message_reactions (
    id         bigint                   NOT NULL,
    message_id bigint                   NOT NULL,
    user_id    bigint                   NOT NULL,
    reaction   text                     NOT NULL,
    created_at timestamp with time zone DEFAULT now()
);


/*
 * messaging.members
 * -----------------
 * Junction table linking users to the conversations they participate in.
 *
 *   role              — 0=member, 1=admin, 2=owner. -1=quit, -2=banned.
 *   frozen_message_id — (NOUVEAU) Plafond temporel absolu de lecture pour les membres bannis.
 */
CREATE TABLE messaging.members (
   id                   bigint                   NOT NULL,
   conversation_id      bigint,                  -- FK → messaging.conversations.id
   user_id              bigint,                  -- FK → auth.users.id
   role                 smallint,                -- -2=banned, -1=quit, 0=member, 1=admin, 2=owner
   settings             jsonb                    DEFAULT '{}'::jsonb,
   joined_at            timestamp with time zone,
   unread_count         integer,
   frozen_message_id    bigint,                  -- Plafond d'historique (Snowflake ID)
   last_read_message_id bigint,                  -- ✅ NOUVEAU : Curseur de lecture (Watermark)
   created_at           timestamp with time zone,
   updated_at           timestamp with time zone
);


/* ---------------------------------------------------------------------------
   2.4  MODERATION SCHEMA — Reports, legal holds
   --------------------------------------------------------------------------- */

/*
 * moderation.reports
 * ------------------
 * Abuse / policy-violation reports submitted by users against any content
 * entity (polymorphic, keyed by target_type + target_id).
 *
 *   actor_id    — reporting user; FK → auth.users.id.
 *   target_type — 0=user, 1=post, 2=comment, 3=media (application-defined).
 *   reason      — free-text or code from a predefined reason list.
 *   rationale   — moderator's written justification for the decision.
 *   state       — 0=pending, 1=actioned, 2=dismissed (application-defined).
 */
CREATE TABLE moderation.reports (
    id           bigint                   NOT NULL,
    reporter_id  bigint,                  -- FK → auth.users.id
    target_type  smallint                 NOT NULL,
    target_ids   bigint[]                 NOT NULL,
    category     smallint                 NOT NULL,
    reason       text,
    rationale    text,
    state        smallint                 DEFAULT 0, -- 0 : pas traiter, 1: en cours, 2: clos
    importance   double precision         DEFAULT 0.0,
    created_at timestamp with time zone   DEFAULT now(),
    updated_at timestamp with time zone   DEFAULT now()
);


/*
 * moderation.legal_holds
 * ----------------------
 * Prevents hard deletion of posts that are subject to legal or regulatory
 * retention obligations (e.g. GDPR Subject Access Requests, litigation hold).
 *
 *   post_id     — PK and FK to content.posts; ON DELETE CASCADE ensures the
 *                 hold record is cleaned up if the post is ever purged.
 *   reason      — mandatory description of the hold basis.
 *   assigned_by — bigint soft-reference to the admin/moderator who placed the hold
 *                 (intentionally no FK to allow holds placed by system accounts).
 *
 * NOTE: The FK constraint is declared inline here because the referenced table
 * (content.posts) is already created above in this execution order.
 */
CREATE TABLE moderation.legal_holds (
    post_id      bigint                   NOT NULL,
    reason       text                     NOT NULL,
    created_at   timestamp with time zone NOT NULL DEFAULT TIMEZONE('utc'::text, NOW()),
    assigned_by  bigint,
    CONSTRAINT fk_legal_holds_post
        FOREIGN KEY (post_id) REFERENCES content.posts(id) ON DELETE CASCADE
);


/* =============================================================================
   SECTION 3 — FUNCTIONS & PROCEDURES
   Organised by schema.  All referenced tables now exist.  Functions are marked
   STABLE where they only read data; mutable helpers and procedures are
   VOLATILE (the default).
   ============================================================================= */

/* ---------------------------------------------------------------------------
   3.1  auth schema — user, relation, session, and settings loaders
   --------------------------------------------------------------------------- */

/*
 * auth.func_load_user
 * -------------------
 * Flexible single-table lookup for auth.users.  All parameters are optional;
 * passing NULL matches any value (i.e. "no filter on this column").
 * Email comparison is case-insensitive and whitespace-trimmed.
 * Used by login, profile fetch, and registration-duplicate checks.
 */
CREATE FUNCTION auth.func_load_user(
    p_id       bigint DEFAULT NULL::bigint,
    p_username text   DEFAULT NULL::text,
    p_email    text   DEFAULT NULL::text,
    p_phone    text   DEFAULT NULL::text
)
RETURNS TABLE(
    id                  bigint,
    username            text,
    email               text,
    email_verified      boolean,
    phone               text,
    phone_verified      boolean,
    password_hash       text,
    first_name          text,
    last_name           text,
    birthdate           date,
    sex                 smallint,
    bio                 text,
    profile_picture_id  bigint,
    grade               smallint,
    location            text,
    school              text,
    work                text,
    badges              jsonb,
    desactivated        boolean,
    banned              boolean,
    ban_reason          text,
    ban_expires_at      timestamp with time zone,
    created_at          timestamp with time zone,
    updated_at          timestamp with time zone
)
LANGUAGE plpgsql STABLE
AS $$
BEGIN
    RETURN QUERY
    SELECT
        u.id,
        u.username,
        u.email,
        u.email_verified,
        u.phone,
        u.phone_verified,
        u.password_hash,
        u.first_name,
        u.last_name,
        u.birthdate,
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
        u.ban_expires_at,
        u.created_at,
        u.updated_at
    FROM auth.users AS u
    WHERE
        (p_id       IS NULL OR u.id = p_id)
        AND (p_username IS NULL OR TRIM(u.username)           = TRIM(p_username))
        AND (p_email    IS NULL OR TRIM(LOWER(u.email))       = TRIM(LOWER(p_email)))
        AND (p_phone    IS NULL OR TRIM(u.phone)              = TRIM(p_phone));
END;
$$;

/*
 * auth.func_load_all_user_identifiers
 * -----------------------------------
 * Retrieves all unique identifiers (username, email, phone) for all users.
 * Used exclusively for Cuckoo Filter warm-up in RAM.
 */
CREATE OR REPLACE FUNCTION auth.func_load_all_user_identifiers()
RETURNS TABLE(username text, email text, phone text)
LANGUAGE plpgsql STABLE
AS $$
BEGIN
RETURN QUERY SELECT u.username, u.email, u.phone FROM auth.users u;
END;
$$;

/*
 * auth.func_load_relations
 * ------------------------
 * Flexible loader for the social-graph edge table auth.relations.
 * All parameters are optional filters (NULL = "any").
 * Used by follow/unfollow flows and relation-state checks.
 */
CREATE FUNCTION auth.func_load_relations(
    p_id           bigint   DEFAULT NULL::bigint,
    p_primary_id   bigint   DEFAULT NULL::bigint,
    p_secondary_id bigint   DEFAULT NULL::bigint,
    p_state        smallint DEFAULT NULL::smallint
)
RETURNS TABLE(
    id            bigint,
    primary_id    bigint,
    secondary_id  bigint,
    state         smallint,
    created_at    timestamp with time zone
)
LANGUAGE plpgsql STABLE
AS $$
BEGIN
    RETURN QUERY
    SELECT
        r.id,
        r.primary_id,
        r.secondary_id,
        r.state,
        r.created_at
    FROM auth.relations AS r
    WHERE
        (p_id           IS NULL OR r.id           = p_id)
        AND (p_primary_id   IS NULL OR r.primary_id   = p_primary_id)
        AND (p_secondary_id IS NULL OR r.secondary_id = p_secondary_id)
        AND (p_state        IS NULL OR r.state        = p_state);
END;
$$;


/*
 * auth.func_get_relation_state
 * ----------------------------
 * Lightweight scalar helper: returns the current state integer of the directed
 * edge from p_caller_id → p_target_id, or 0 if no relation exists.
 *
 * BUG FIXED: original dump used non-existent column names caller_id and
 * target_id (the table uses primary_id / secondary_id), and contained a stray
 * '<' character in the DECLARE block that caused a parse nubo_error.  Both corrected.
 */
CREATE OR REPLACE FUNCTION auth.func_get_relation_state(
    p_caller_id bigint,
    p_target_id bigint
)
RETURNS int
LANGUAGE plpgsql
AS $$
DECLARE
    v_state int;
BEGIN
    SELECT state INTO v_state
    FROM auth.relations
    WHERE primary_id = p_caller_id
      AND secondary_id = p_target_id;

    -- Return 0 (no relation) when the edge is absent
    IF NOT FOUND THEN
        RETURN 0;
    END IF;

    RETURN v_state;
END;
$$;

/*
 * auth.func_load_relations_paginated_by_direction
 * -----------------------------------------------
 * Récupère les IDs cibles d'une relation de manière paginée et chronologique.
 * direction: 'incoming' (mes abonnés, mes amis) ou 'outgoing' (mes bloqués).
 */
CREATE OR REPLACE FUNCTION auth.func_load_relations_paginated_by_direction(
    p_user_id bigint,
    p_state smallint,
    p_direction text,
    p_limit int,
    p_offset int
) RETURNS TABLE(matched_id bigint)
LANGUAGE plpgsql STABLE
AS $$
BEGIN
    IF p_direction = 'incoming' THEN
        RETURN QUERY
SELECT primary_id FROM auth.relations
WHERE secondary_id = p_user_id AND state = p_state
ORDER BY created_at DESC
    LIMIT p_limit OFFSET p_offset;
ELSE
        RETURN QUERY
SELECT secondary_id FROM auth.relations
WHERE primary_id = p_user_id AND state = p_state
ORDER BY created_at DESC
    LIMIT p_limit OFFSET p_offset;
END IF;
END;
$$;


/*
 * auth.func_load_sessions
 * -----------------------
 * Session lookup with optional filters.  When a firebase_installation_id is provided the
 * function short-circuits and returns at most one row (fast path used on every
 * authenticated request).  Otherwise it falls through to the generic
 * multi-predicate query.
 */
CREATE FUNCTION auth.func_load_sessions(
    p_id           bigint DEFAULT NULL::bigint,
    p_user_id      bigint DEFAULT NULL::bigint,
    p_firebase_installation_id text   DEFAULT NULL::text,
    p_master_token text   DEFAULT NULL::text
)
RETURNS TABLE(
    id            bigint,
    user_id       bigint,
    master_token  text,
    firebase_installation_id  text,
    device_info   jsonb,
    ip_history    inet[],
    created_at    timestamp with time zone,
    expires_at    timestamp with time zone
)
LANGUAGE plpgsql STABLE
AS $$
BEGIN
    -- Fast path: firebase_installation_id lookup (unique index guarantees ≤ 1 row)
    IF p_firebase_installation_id IS NOT NULL THEN
        RETURN QUERY
        SELECT
            s.id,
            s.user_id,
            s.master_token,
            s.firebase_installation_id,
            s.device_info,
            s.ip_history,
            s.created_at,
            s.expires_at
        FROM auth.sessions AS s
        WHERE s.firebase_installation_id = p_firebase_installation_id
        LIMIT 1;
        RETURN;
    END IF;

    -- General path: all optional filters combined
    RETURN QUERY
    SELECT
        s.id,
        s.user_id,
        s.master_token,
        s.firebase_installation_id,
        s.device_info,
        s.ip_history,
        s.created_at,
        s.expires_at
    FROM auth.sessions AS s
    WHERE
        (p_id           IS NULL OR s.id           = p_id)
        AND (p_user_id      IS NULL OR s.user_id      = p_user_id)
        AND (p_firebase_installation_id IS NULL OR s.firebase_installation_id = p_firebase_installation_id)
        AND (p_master_token IS NULL OR s.master_token = p_master_token);
END;
$$;

/*
 * auth.func_get_active_sessions_view
 * ----------------------------------
 * Projection strictement sécurisée des sessions actives d'un utilisateur.
 * Exclut physiquement les tokens (master_token, firebase_installation_id, secrets)
 * dès la couche BDD. Parfait alignement avec le DDD.
 */
CREATE OR REPLACE FUNCTION auth.func_get_active_sessions_view(
    p_user_id bigint
)
RETURNS TABLE(
    id            bigint,
    device_info   jsonb,
    ip_history    inet[],
    created_at    timestamp with time zone,
    expires_at    timestamp with time zone
)
LANGUAGE plpgsql STABLE
AS $$
BEGIN
RETURN QUERY
SELECT
    s.id,
    s.device_info,
    s.ip_history,
    s.created_at,
    s.expires_at
FROM auth.sessions AS s
WHERE s.user_id = p_user_id;
END;
$$;

/*
 * auth.func_load_user_settings
 * ----------------------------
 * Retrieves the personalisation and privacy settings record for a user.
 * Typically called with p_user_id; p_id is available for direct PK lookup.
 */
CREATE OR REPLACE FUNCTION auth.func_load_user_settings(
    p_id      bigint DEFAULT NULL::bigint,
    p_user_id bigint DEFAULT NULL::bigint
)
RETURNS TABLE(
    id                  bigint,
    user_id             bigint,
    privacy             jsonb,
    notifications       jsonb,
    display_and_content jsonb,    -- ✅ NOUVEAU
    telemetry_vector    real[],
    telemetry_tags      text[],
    telemetry_timestamp bigint
)
LANGUAGE plpgsql STABLE
AS $$
BEGIN
RETURN QUERY
SELECT
    s.id, s.user_id, s.privacy, s.notifications, s.display_and_content, -- ✅ NOUVEAU
    s.telemetry_vector, s.telemetry_tags, s.telemetry_timestamp
FROM auth.user_settings AS s
WHERE
    (p_id      IS NULL OR s.id      = p_id)
  AND (p_user_id IS NULL OR s.user_id = p_user_id);
END;
$$;

/*
 * auth.func_load_users_paginated
 * ------------------------------
 * Pageable user profile loader for SPEED Cache bulk seeding.
 * p_limit   — Maximum number of rows returned per batch.
 * p_offset  — Offset for pagination.
 *
 * Designed specifically for the V12 architecture warm-up phase.
 * Performs a LEFT JOIN between auth.users and content.profiles
 * to instantly hydrate the L1 Speed Cache (Redis) with lightweight
 * profile footprints avoiding full L2/L3 fallbacks.
 */
CREATE OR REPLACE FUNCTION auth.func_load_users_paginated(
    p_limit INTEGER,
    p_offset INTEGER
)
RETURNS TABLE (
    id BIGINT,
    username VARCHAR,
    first_name VARCHAR,
    last_name VARCHAR,
    profile_picture_id BIGINT,
    bio TEXT,
    grade SMALLINT,
    conversation_permission SMALLINT,
    add_group_permission SMALLINT,  -- ✅ CORRIGÉ (C'est un int)
    hide_connections SMALLINT        -- ✅ NOUVEAU
)
LANGUAGE plpgsql STABLE
AS $$
BEGIN
RETURN QUERY
SELECT
    u.id,
    u.username,
    u.first_name,
    u.last_name,
    u.profile_picture_id,
    u.bio,
    u.grade,
    COALESCE((s.privacy->>'conversation_permission')::smallint, 0::smallint) AS conversation_permission,
    COALESCE((s.privacy->>'add_group_permission')::smallint, 0::smallint) AS add_group_permission,
    COALESCE((s.privacy->>'hide_connections')::smallint, false) AS hide_connections
FROM auth.users u
         LEFT JOIN auth.user_settings s ON u.id = s.user_id
ORDER BY u.id
    LIMIT p_limit OFFSET p_offset;
END;
$$;

/*
 * auth.func_load_relations_paginated
 * ----------------------------------
 * Pageable loader for the social graph (relations) mass seeding.
 * p_limit   — Maximum number of rows returned per batch.
 * p_offset  — Offset for pagination.
 *
 * Iterates sequentially over the auth.relations table.
 * Used exclusively at server startup to reconstruct the L1 Graph Cache,
 * ensuring O(1) access to follower counts, friend lists, and access rights.
 */
CREATE OR REPLACE FUNCTION auth.func_load_relations_paginated(
    p_limit INTEGER,
    p_offset INTEGER
)
RETURNS TABLE (
    primary_id BIGINT,
    secondary_id BIGINT,
    state SMALLINT,
    created_at TIMESTAMP WITH TIME ZONE
)
LANGUAGE plpgsql STABLE
AS $$
BEGIN
RETURN QUERY
SELECT
    r.primary_id,
    r.secondary_id,
    r.state
FROM auth.relations r
ORDER BY r.primary_id, r.secondary_id
    LIMIT p_limit OFFSET p_offset;
END;
$$;

/*
 * auth.func_load_addable_relations
 * --------------------------------
 * Récupère les IDs cibles et l'état des relations actives (1 = Abonné, 2 = Ami)
 * initiées par un utilisateur spécifique.
 */
CREATE OR REPLACE FUNCTION auth.func_load_addable_relations(
    p_user_id bigint
)
RETURNS TABLE (
    target_id bigint,
    state smallint
)
LANGUAGE plpgsql STABLE
AS $$
BEGIN
RETURN QUERY
SELECT r.secondary_id AS target_id, r.state
FROM auth.relations r
WHERE r.primary_id = p_user_id
  AND r.state IN (1, 2);
END;
$$;

/* ---------------------------------------------------------------------------
   3.2  content schema — post, comment, media, like, and tag loaders
   --------------------------------------------------------------------------- */

/*
 * content.func_load_posts
 * -----------------------
 * Pageable post feed loader supporting three sort modes:
 *   0 = newest first (default)  |  1 = oldest first
 *
 *   p_visibility — smallint[] filter; defaults to [0, 1] (public + followers-only).
 *                  Posts with visibility = 2 (deleted) are always excluded.
 *   p_post_ids  — supply an explicit ID list for batch-hydration (e.g. feed engine
 *                 returns IDs, this function fetches the full rows).
 *   vector / vector_version — returned for downstream ML/recommendation use.
 */
CREATE OR REPLACE FUNCTION content.func_load_posts(
    p_user_id    bigint    DEFAULT NULL::bigint,
    p_post_ids   bigint[]  DEFAULT NULL::bigint[],
    p_visibility smallint[] DEFAULT ARRAY[0::smallint, 1::smallint],
    p_order_mode smallint  DEFAULT 0
)
RETURNS TABLE(
    id              bigint,
    user_id         bigint,
    content         text,
    hashtags        text[],
    indirect_hashtags text[], -- ✅ NOUVEAU
    identifiers     bigint[],
    media_ids       bigint[],
    visibility      smallint,
    priority_level  smallint,
    location        text,
    created_at      timestamp with time zone,
    updated_at      timestamp with time zone,
    like_count      integer,
    comment_count   integer,
    view_count      integer,
    report_count    integer,
    has_media       boolean,
    vector          real[],
    vector_version  integer,
    telemetry_dwell_sum double precision,
    telemetry_dwell_sq  double precision,
    telemetry_clicks    integer
)
LANGUAGE plpgsql
AS $$
BEGIN
RETURN QUERY
SELECT
    p.id, p.user_id, p.content, p.hashtags, p.indirect_hashtags, p.identifiers, p.media_ids, -- ✅ NOUVEAU
    p.visibility, p.priority_level, p.location, p.created_at, p.updated_at,
    p.like_count, p.comment_count, p.view_count, p.report_count, p.has_media, p.vector, p.vector_version,
    p.telemetry_dwell_sum, p.telemetry_dwell_sq, p.telemetry_clicks
FROM content.posts p
WHERE
    p.visibility != 2
        AND (p_user_id  IS NULL OR p.user_id = p_user_id)
        AND (p_post_ids IS NULL OR p.id = ANY(p_post_ids))
        AND (p.visibility = ANY(p_visibility))
ORDER BY
    CASE WHEN p_order_mode = 0 THEN p.created_at END DESC,
        CASE WHEN p_order_mode = 1 THEN p.created_at END ASC;
END;
$$;

/*
 * content.func_load_user_posts
 * ----------------------
 * Pageable timeline loader for a specific user profile.
 * * p_user_id — The target user whose chronological feed is requested.
 * p_limit   — Maximum number of rows returned (enforced by API, typically 50-100).
 * p_offset  — Offset for pagination.
 *
 * Posts with visibility = -1 (soft-deleted) are strictly excluded (visibility >= 0).
 * Designed as an absolute L3 fallback when L1 (ZSET) and L2 (Mongo) are missing.
 * Returns the full payload to instantly hydrate the ZSET and the Object Cache.
 */
CREATE OR REPLACE FUNCTION content.func_load_user_posts(p_user_id bigint, p_limit integer, p_offset integer)
RETURNS TABLE (
    id              bigint,
    user_id         bigint,
    content         text,
    hashtags        text[],
    indirect_hashtags text[], -- ✅ NOUVEAU
    identifiers     bigint[],
    media_ids       bigint[],
    visibility      smallint,
    priority_level  smallint,
    location        text,
    created_at      timestamp with time zone,
    updated_at      timestamp with time zone,
    like_count      integer,
    comment_count   integer,
    view_count      integer,
    has_media       boolean,
    vector          real[],
    vector_version  integer,
    telemetry_dwell_sum double precision,
    telemetry_dwell_sq  double precision,
    telemetry_clicks    integer
)
LANGUAGE plpgsql
AS $$
BEGIN
RETURN QUERY
SELECT
    p.id, p.user_id, p.content, p.hashtags, p.indirect_hashtags, p.identifiers, p.media_ids, -- ✅ NOUVEAU
    p.visibility, p.priority_level, p.location, p.created_at, p.updated_at,
    p.like_count, p.comment_count, p.view_count, p.has_media, p.vector, p.vector_version,
    p.telemetry_dwell_sum, p.telemetry_dwell_sq, p.telemetry_clicks
FROM content.posts p
WHERE p.user_id = p_user_id AND p.visibility >= 0
ORDER BY p.created_at DESC
    LIMIT p_limit OFFSET p_offset;
END;
$$;

/*
 * content.func_load_posts_paginated
 * ---------------------------------
 * Pageable loader for general posts, sorted by creation date.
 * Incorporates telemetry metrics for L1/L2 hydration.
 */
CREATE OR REPLACE FUNCTION content.func_load_posts_paginated(p_limit integer, p_offset integer)
RETURNS TABLE(
    id                  bigint,
    user_id             bigint,
    content             text,
    hashtags            text[],
    indirect_hashtags   text[], -- ✅ NOUVEAU
    identifiers         bigint[],
    media_ids           bigint[],
    visibility          smallint,
    priority_level      smallint,
    location            text,
    created_at          timestamp with time zone,
    updated_at          timestamp with time zone,
    like_count          integer,
    comment_count       integer,
    view_count          integer,
    has_media           boolean,
    vector              real[],
    vector_version      integer,
    telemetry_dwell_sum double precision,
    telemetry_dwell_sq  double precision,
    telemetry_clicks    integer
)
LANGUAGE plpgsql
AS $$
BEGIN
RETURN QUERY
SELECT
    p.id, p.user_id, p.content, p.hashtags, p.indirect_hashtags, p.identifiers, p.media_ids, -- ✅ NOUVEAU
    p.visibility, p.priority_level, p.location, p.created_at, p.updated_at,
    p.like_count, p.comment_count, p.view_count, p.has_media, p.vector, p.vector_version,
    p.telemetry_dwell_sum, p.telemetry_dwell_sq, p.telemetry_clicks
FROM content.posts p
WHERE p.visibility != 2
ORDER BY p.created_at DESC
    LIMIT p_limit OFFSET p_offset;
END;
$$;

-- Fonction dédiée à l'initialisation du Graphe Sémantique (Time-Travel Ingestion)
-- Retourne les posts chronologiquement pour respecter l'équation de Markov.
CREATE OR REPLACE FUNCTION content.func_load_posts_for_graph_seeding()
RETURNS TABLE (
    id bigint,
    hashtags text[],
    created_at timestamp with time zone
) AS $$
BEGIN
RETURN QUERY
SELECT p.id, p.hashtags, p.created_at
FROM content.posts p
-- On exclut les soft-deletes (-1) et on ne prend que ceux ayant au moins 2 tags
WHERE p.visibility >= 0 AND array_length(p.hashtags, 1) > 1
ORDER BY p.created_at ASC; -- ORDRE CHRONOLOGIQUE STRICT IMPÉRATIF
END;
$$ LANGUAGE plpgsql;

/*
 * content.func_load_recent_posts
 * ------------------------------
 * Récupère les posts récents sur une période glissante (en jours).
 * Exclut strictement les posts supprimés (visibility = 2).
 * Renvoie l'intégralité du payload (incluant les vecteurs et compteurs)
 * pour hydrater immédiatement le cache L1 (ZSETs et Object Cache).
 */
CREATE OR REPLACE FUNCTION content.func_load_recent_posts(p_days integer)
RETURNS TABLE(
    id              bigint,
    user_id         bigint,
    content         text,
    hashtags        text[],
    indirect_hashtags text[], -- ✅ NOUVEAU
    identifiers     bigint[],
    media_ids       bigint[],
    visibility      smallint,
    priority_level  smallint,
    location        text,
    created_at      timestamp with time zone,
    updated_at      timestamp with time zone,
    like_count      integer,
    comment_count   integer,
    view_count      integer,
    has_media       boolean,
    vector          real[],
    vector_version  integer,
    telemetry_dwell_sum double precision,
    telemetry_dwell_sq  double precision,
    telemetry_clicks    integer
)
LANGUAGE plpgsql
AS $$
BEGIN
RETURN QUERY
SELECT
    p.id, p.user_id, p.content, p.hashtags, p.indirect_hashtags, p.identifiers, p.media_ids, -- ✅ NOUVEAU
    p.visibility, p.priority_level, p.location, p.created_at, p.updated_at,
    p.like_count, p.comment_count, p.view_count, p.has_media, p.vector, p.vector_version,
    p.telemetry_dwell_sum, p.telemetry_dwell_sq, p.telemetry_clicks
FROM content.posts p
WHERE p.visibility != 2 AND p.created_at >= NOW() - (p_days || ' days')::interval
ORDER BY p.created_at DESC;
END;
$$;

/*
 * content.func_increment_post_telemetry
 * -------------------------------------
 * Atomic updater for post telemetry metrics (edge-to-cloud sync).
 */
CREATE OR REPLACE FUNCTION content.func_increment_post_telemetry(
    p_post_id bigint,
    p_dwell_sum double precision,
    p_dwell_sq double precision,
    p_clicks integer
)
RETURNS void
LANGUAGE plpgsql
AS $$
BEGIN
UPDATE content.posts
SET telemetry_dwell_sum = telemetry_dwell_sum + p_dwell_sum,
    telemetry_dwell_sq  = telemetry_dwell_sq + p_dwell_sq,
    telemetry_clicks    = telemetry_clicks + p_clicks
WHERE id = p_post_id;
END;
$$;

/*
 * content.func_increment_post_view
 * --------------------------------
 * Atomic updater for post view counts, preventing negative values.
 */
CREATE OR REPLACE FUNCTION content.func_increment_post_view(p_post_id bigint, p_delta integer)
RETURNS void
LANGUAGE plpgsql
AS $$
BEGIN
UPDATE content.posts
SET view_count = GREATEST(0, view_count + p_delta)
WHERE id = p_post_id;
END;
$$;

/*
 * content.func_load_post_ids_by_tag_paginated
 * -------------------------------------------
 * Retrieves post IDs containing a specific hashtag (direct or indirect), paginated.
 * Excludes soft-deleted posts (visibility = 2).
 */
CREATE OR REPLACE FUNCTION content.func_load_post_ids_by_tag_paginated(
    p_tag text, p_offset integer, p_limit integer
)
RETURNS TABLE(id bigint)
LANGUAGE plpgsql STABLE
AS $$
BEGIN
RETURN QUERY
SELECT p.id FROM content.posts p
WHERE (p_tag = ANY(p.hashtags) OR p_tag = ANY(p.indirect_hashtags)) -- ✅ MODIFIÉ
  AND p.visibility != 2
ORDER BY p.created_at DESC
OFFSET p_offset LIMIT p_limit;
END;
$$;

/*
 * content.func_load_posts_for_time_decay
 * --------------------------------------
 * Retrieves essential post metrics within a specific age window
 * for the Time-Decay algorithm workers.
 */
CREATE OR REPLACE FUNCTION content.func_load_posts_for_time_decay(
    p_min_age interval, p_max_age interval
)
RETURNS TABLE(
    id bigint, like_count int, comment_count int, view_count int,
    has_media boolean, created_at timestamp with time zone,
    hashtags text[], indirect_hashtags text[], -- ✅ NOUVEAU
    visibility smallint, priority_level smallint, report_count int
)
LANGUAGE plpgsql STABLE
AS $$
BEGIN
RETURN QUERY
SELECT p.id, p.like_count, p.comment_count, p.view_count, p.has_media,
       p.created_at, p.hashtags, p.indirect_hashtags, p.visibility, p.priority_level, p.report_count -- ✅ NOUVEAU
FROM content.posts p
WHERE p.created_at <= NOW() - p_min_age
  AND p.created_at > NOW() - p_max_age
  AND p.visibility != 2;
END;
$$;


/* 6. content.func_load_posts_for_graph_seeding */
CREATE OR REPLACE FUNCTION content.func_load_posts_for_graph_seeding()
RETURNS TABLE (
    id bigint,
    hashtags text[],
    indirect_hashtags text[], -- ✅ NOUVEAU
    created_at timestamp with time zone
) AS $$
BEGIN
RETURN QUERY
SELECT p.id, p.hashtags, p.indirect_hashtags, p.created_at -- ✅ NOUVEAU
FROM content.posts p
WHERE p.visibility >= 0 AND (array_length(p.hashtags, 1) > 1 OR array_length(p.indirect_hashtags, 1) > 0)
ORDER BY p.created_at ASC;
END;
$$ LANGUAGE plpgsql;

/*
 * content.func_search_post_ids_by_tag
 * -----------------------------------
 * Recherche des posts par hashtag (direct ou indirect) avec tri dynamique.
 * order_mode: 0=views, 1=likes, 2=comments, 3=recent, 4=oldest
 */
CREATE OR REPLACE FUNCTION content.func_search_post_ids_by_tag(
    p_tag text, p_order_mode smallint, p_offset integer, p_limit integer
) RETURNS TABLE(id bigint) AS $$
BEGIN
RETURN QUERY
SELECT p.id FROM content.posts p
WHERE (p_tag = ANY(p.hashtags) OR p_tag = ANY(p.indirect_hashtags))
  AND p.visibility != 2 -- Exclut les posts supprimés
ORDER BY
    CASE WHEN p_order_mode = 0 THEN p.view_count END DESC NULLS LAST,
        CASE WHEN p_order_mode = 1 THEN p.like_count END DESC NULLS LAST,
        CASE WHEN p_order_mode = 2 THEN p.comment_count END DESC NULLS LAST,
        CASE WHEN p_order_mode = 3 THEN p.created_at END DESC NULLS LAST,
        CASE WHEN p_order_mode = 4 THEN p.created_at END ASC NULLS LAST
    OFFSET p_offset LIMIT p_limit;
END;
$$ LANGUAGE plpgsql STABLE;

/*
 * content.func_search_post_ids_by_users
 * -------------------------------------
 * Recherche des posts pour un PANIER d'utilisateurs (Lexicographique) avec tri dynamique.
 */
CREATE OR REPLACE FUNCTION content.func_search_post_ids_by_users(
    p_user_ids bigint[], p_order_mode smallint, p_offset integer, p_limit integer
) RETURNS TABLE(id bigint) AS $$
BEGIN
RETURN QUERY
SELECT p.id FROM content.posts p
WHERE p.user_id = ANY(p_user_ids)
  AND p.visibility != 2
ORDER BY
    CASE WHEN p_order_mode = 0 THEN p.view_count END DESC NULLS LAST,
        CASE WHEN p_order_mode = 1 THEN p.like_count END DESC NULLS LAST,
        CASE WHEN p_order_mode = 2 THEN p.comment_count END DESC NULLS LAST,
        CASE WHEN p_order_mode = 3 THEN p.created_at END DESC NULLS LAST,
        CASE WHEN p_order_mode = 4 THEN p.created_at END ASC NULLS LAST
    OFFSET p_offset LIMIT p_limit;
END;
$$ LANGUAGE plpgsql STABLE;

/*
 * content.func_search_post_ids_by_text
 * ------------------------------------
 * Recherche textuelle avancée sur le contenu des posts.
 */
CREATE OR REPLACE FUNCTION content.func_search_post_ids_by_text(
    p_query text, p_order_mode int, p_offset integer, p_limit integer
) RETURNS TABLE(id bigint) AS $$
BEGIN
RETURN QUERY
SELECT p.id FROM content.posts p
-- Recherche textuelle via le moteur natif de Postgres
WHERE to_tsvector('french', p.content) @@ plainto_tsquery('french', p_query)
  AND p.visibility != 2 -- Exclut les posts supprimés
ORDER BY
    CASE WHEN p_order_mode = 0 THEN p.view_count END DESC NULLS LAST,
        CASE WHEN p_order_mode = 1 THEN p.like_count END DESC NULLS LAST,
        CASE WHEN p_order_mode = 2 THEN p.comment_count END DESC NULLS LAST,
        CASE WHEN p_order_mode = 3 THEN p.created_at END DESC NULLS LAST,
        CASE WHEN p_order_mode = 4 THEN p.created_at END ASC NULLS LAST
    OFFSET p_offset LIMIT p_limit;
END;
$$ LANGUAGE plpgsql STABLE;

/*
 * content.func_load_comments
 * --------------------------
 * Returns visible comments for a post, optionally filtered by user, with
 * three sort modes:
 *   0 = newest first (default)  |  1 = most-liked  |  2 = oldest first
 *
 * BUG FIXED: original function omitted visibility from the SELECT (it filtered
 * on it but never returned it).  Kept as INTEGER to match the table column type
 * (was incorrectly BOOLEAN in the original dump).
 *
 * like_count is computed live via a correlated sub-query on content.likes
 * (target_type = 2 → comment reactions).
 */
CREATE OR REPLACE FUNCTION content.func_load_comments(
    p_post_id    bigint   DEFAULT NULL::bigint,
    p_user_id    bigint   DEFAULT NULL::bigint,
    p_limit      integer  DEFAULT 100,
    p_order_mode smallint DEFAULT 0
)
RETURNS TABLE(
    id          bigint,
    post_id     bigint,
    user_id     bigint,
    content     text,
    visibility  integer,
    like_count  integer,
    score       integer, -- ✅ NOUVEAU
    created_at  timestamp with time zone,
    updated_at  timestamp with time zone
)
LANGUAGE plpgsql
AS $$
BEGIN
RETURN QUERY
SELECT
    c.id,
    c.post_id,
    c.user_id,
    c.content,
    c.visibility,
    c.like_count, -- Lecture directe de la colonne dénormalisée
    c.score,
    c.created_at,
    c.updated_at
FROM content.comments c
WHERE
    c.visibility = 0
  AND (p_post_id IS NULL OR c.post_id = p_post_id)
  AND (p_user_id IS NULL OR c.user_id = p_user_id)
ORDER BY
    CASE WHEN p_order_mode = 0 THEN c.created_at END DESC,
    CASE WHEN p_order_mode = 1 THEN c.score END DESC,      -- ✅ Tri sur le Score absolu unifié
    CASE WHEN p_order_mode = 2 THEN c.created_at END ASC
    LIMIT p_limit;
END;
$$;

/*
 * content.func_increment_comment_metrics
 * --------------------------------------
 * Atomic updater for comment engagement metrics.
 * p_comment_id — Target comment ID.
 * p_delta      — Increment value (+1 or -1).
 *
 * Synchronously updates the denormalised like_count and the unified
 * recommendation score to maintain strict L3 consistency without race conditions.
 */
CREATE OR REPLACE FUNCTION content.func_increment_comment_metrics(
    p_comment_id bigint,
    p_delta integer
)
RETURNS void
LANGUAGE plpgsql
AS $$
BEGIN
UPDATE content.comments
SET
    like_count = GREATEST(0, like_count + p_delta),
    score      = score + p_delta
WHERE id = p_comment_id;
END;
$$;

/*
 * content.func_increment_post_comment
 * -----------------------------------
 * Atomic updater for post comment counts, preventing negative values.
 */
CREATE OR REPLACE FUNCTION content.func_increment_post_comment(p_post_id bigint, p_delta integer)
RETURNS void
LANGUAGE plpgsql
AS $$
BEGIN
UPDATE content.posts
SET comment_count = GREATEST(0, comment_count + p_delta)
WHERE id = p_post_id;
END;
$$;

/*
 * content.func_load_comments_paginated
 * -------------------------------------
 * Paginated fallback loader (L3 cache miss path).  Returns all non-deleted
 * comments for a post sorted by engagement (like_count DESC, oldest first as
 * tiebreaker) with LIMIT/OFFSET for cursor-style pagination.
 */
CREATE OR REPLACE FUNCTION content.func_load_comments_paginated(
    p_post_id bigint,
    p_limit   int,
    p_offset  int
)
RETURNS TABLE(
    id          bigint,
    post_id     bigint,
    user_id     bigint,
    content     text,
    visibility  int,
    like_count  int,
    score       int, -- ✅ NOUVEAU
    created_at  timestamp with time zone,
    updated_at  timestamp with time zone
)
LANGUAGE plpgsql
AS $$
BEGIN
RETURN QUERY
SELECT
    c.id,
    c.post_id,
    c.user_id,
    c.content,
    c.visibility,
    c.like_count,
    c.score,     -- ✅ NOUVEAU
    c.created_at,
    c.updated_at
FROM content.comments c
WHERE
    c.post_id = p_post_id
  AND c.visibility != -1
ORDER BY c.score DESC, c.created_at ASC -- ✅ Remplacement de like_count par score
    LIMIT p_limit OFFSET p_offset;
END;
$$;


/*
 * content.func_get_comment
 * ------------------------
 * Single-comment lookup by PK.  Returns the full comment row including
 * visibility status and the denormalised like_count.
 */
CREATE OR REPLACE FUNCTION content.func_get_comment(
    p_comment_id bigint
)
RETURNS TABLE(
    id          bigint,
    post_id     bigint,
    user_id     bigint,
    content     text,
    visibility  int,
    like_count  int,
    score       int, -- ✅ NOUVEAU
    created_at  timestamp with time zone,
    updated_at  timestamp with time zone
)
LANGUAGE plpgsql
AS $$
BEGIN
RETURN QUERY
SELECT
    c.id,
    c.post_id,
    c.user_id,
    c.content,
    c.visibility,
    c.like_count,
    c.score,     -- ✅ NOUVEAU
    c.created_at,
    c.updated_at
FROM content.comments c
WHERE c.id = p_comment_id;
END;
$$;


/*
 * content.func_load_likes
 * -----------------------
 * Flexible like record loader.  Supports filtering by target_type, target_id,
 * and user_id.  Sort modes: 0 = newest first, 1 = oldest first.
 * Used for reaction feeds and checking whether a user already liked an entity.
 */
CREATE FUNCTION content.func_load_likes(
    p_target_type smallint DEFAULT NULL::smallint,
    p_target_id   bigint   DEFAULT NULL::bigint,
    p_user_id     bigint   DEFAULT NULL::bigint,
    p_limit       integer  DEFAULT 100,
    p_order_mode  smallint DEFAULT 0
)
RETURNS TABLE(
    id           bigint,
    target_type  smallint,
    target_id    bigint,
    user_id      bigint,
    created_at   timestamp with time zone
)
LANGUAGE plpgsql
AS $$
BEGIN
    RETURN QUERY
    SELECT
        l.id,
        l.target_type,
        l.target_id,
        l.user_id,
        l.created_at
    FROM content.likes l
    WHERE
        (p_target_type IS NULL OR l.target_type = p_target_type)
        AND (p_target_id   IS NULL OR l.target_id   = p_target_id)
        AND (p_user_id     IS NULL OR l.user_id     = p_user_id)
    ORDER BY
        CASE WHEN p_order_mode = 0 THEN l.created_at END DESC,
        CASE WHEN p_order_mode = 1 THEN l.created_at END ASC
    LIMIT p_limit;
END;
$$;


/*
 * content.func_increment_post_like
 * --------------------------------
 * Atomic updater for post like counts, preventing negative values.
 */
CREATE OR REPLACE FUNCTION content.func_increment_post_like(p_post_id bigint, p_delta integer)
RETURNS void
LANGUAGE plpgsql
AS $$
BEGIN
UPDATE content.posts
SET like_count = GREATEST(0, like_count + p_delta)
WHERE id = p_post_id;
END;
$$;

/*
 * content.func_delete_orphan_likes
 * --------------------------------
 * Purge les likes orphelins (attachés à des posts ou commentaires supprimés).
 * Retourne les types et IDs des cibles pour répercuter la purge sur MongoDB.
 */
CREATE OR REPLACE FUNCTION content.func_delete_orphan_likes()
RETURNS TABLE(target_type smallint, target_id bigint)
LANGUAGE plpgsql
AS $$
BEGIN
RETURN QUERY
DELETE FROM content.likes l
WHERE
    (l.target_type = 0 AND NOT EXISTS (SELECT 1 FROM content.posts p WHERE p.id = l.target_id AND p.visibility >= 0 AND p.visibility != 2))
   OR
    (l.target_type = 1 AND NOT EXISTS (SELECT 1 FROM content.comments c WHERE c.id = l.target_id AND c.visibility >= 0))
    RETURNING l.target_type, l.target_id;
END;
$$;

/*
 * content.func_load_media
 * -----------------------
 * Loads visible media records, optionally filtered by owner or an explicit
 * list of IDs.  Used for profile picture resolution and post-media hydration.
 * Sort modes: 0 = newest first, 1 = oldest first.
 */
CREATE FUNCTION content.func_load_media(
    p_owner_id   bigint   DEFAULT NULL::bigint,
    p_media_ids  bigint[] DEFAULT NULL::bigint[],
    p_order_mode smallint DEFAULT 0
)
RETURNS TABLE(
    id            bigint,
    owner_id      bigint,
    storage_path  text,
    visibility    boolean,
    created_at    timestamp with time zone
)
LANGUAGE plpgsql
AS $$
BEGIN
    RETURN QUERY
    SELECT
        m.id,
        m.owner_id,
        m.storage_path,
        m.visibility,
        m.created_at
    FROM content.media m
    WHERE
        m.visibility = TRUE
        AND (p_owner_id  IS NULL OR m.owner_id = p_owner_id)
        AND (p_media_ids IS NULL OR m.id = ANY(p_media_ids))
    ORDER BY
        CASE WHEN p_order_mode = 0 THEN m.created_at END DESC,
        CASE WHEN p_order_mode = 1 THEN m.created_at END ASC;
END;
$$;


/*
 * content.get_media
 * -----------------
 * Single-asset lookup by PK (includes updated_at, unlike func_load_media).
 * Used when a specific media record needs to be fully hydrated (e.g. when
 * serving a media detail page or validating an attachment reference).
 */
CREATE OR REPLACE FUNCTION content.get_media(
    p_media_id bigint
)
RETURNS TABLE(
    id            bigint,
    owner_id      bigint,
    storage_path  text,
    visibility    boolean,
    created_at    timestamp with time zone,
    updated_at    timestamp with time zone
)
LANGUAGE plpgsql
AS $$
BEGIN
    RETURN QUERY
    SELECT
        m.id,
        m.owner_id,
        m.storage_path,
        m.visibility,
        m.created_at,
        m.updated_at
    FROM content.media m
    WHERE m.id = p_media_id;
END;
$$;

/*
 * content.func_delete_orphan_media
 * --------------------------
 * Returns every media record that is orphaned (not referenced by any post, comment, or user profile)
 * and has been invisible for more than 24 hours. Deletes the records from the database and returns their
 * IDs and storage paths for downstream cleanup.
 */
CREATE OR REPLACE FUNCTION content.func_delete_orphan_media()
RETURNS TABLE(id bigint, storage_path text)
LANGUAGE plpgsql
AS $$
BEGIN
RETURN QUERY
DELETE FROM content.media m
WHERE m.visibility = false

  -- ====================================================================
  -- BOUCLIER 1 : SCELLÉS JUDICIAIRES (Durée infinie)
  -- ====================================================================
  AND NOT EXISTS (
    SELECT 1 FROM moderation.legal_holds lh
                      JOIN content.posts p ON lh.post_id = p.id
    WHERE m.id = ANY(p.media_ids)
)

  -- ====================================================================
  -- BOUCLIER 2 : SIGNALEMENTS EN COURS (Durée : le temps du traitement)
  -- ====================================================================
  AND NOT EXISTS (
    SELECT 1 FROM moderation.reports r
    WHERE r.state < 2 -- 0=Pending, 1=Actioned (2=Dismissed/Clos, donc on peut supprimer)
      AND (
        (r.target_type = 3 AND m.id = ANY(r.target_ids)) OR
        (r.target_type = 1 AND EXISTS (SELECT 1 FROM content.posts p WHERE p.id = ANY(r.target_ids) AND m.id = ANY(p.media_ids)))
        )
)

  -- ====================================================================
  -- CYCLE DE VIE DES MÉDIAS NON PROTÉGÉS
  -- ====================================================================
  AND (
    -- CAS A : Brouillons & Anciens Avatars (Orphelins purs). Délai : 6 heures.
    (
        m.updated_at < NOW() - INTERVAL '6 hours'
            AND NOT EXISTS (SELECT 1 FROM content.posts p WHERE m.id = ANY(p.media_ids))
            AND NOT EXISTS (SELECT 1 FROM messaging.messages msg WHERE msg.attachments @> jsonb_build_object('media_id', m.id))
            AND NOT EXISTS (SELECT 1 FROM auth.users u WHERE u.profile_picture_id = m.id)
        )
        OR
        -- CAS B : Suppression volontaire (Soft Delete). Délai de grâce : 48 heures.
    (
        m.updated_at < NOW() - INTERVAL '48 hours'
        )
    )
    RETURNING m.id, m.storage_path;
END;
$$;

/*
 * content.func_load_all_tags
 * --------------------------
 * Returns every hashtag slug in the canonical tag registry.
 * Used to hydrate autocomplete / trending-tag lists.
 */
CREATE OR REPLACE FUNCTION content.func_load_all_tags()
RETURNS TABLE(slug text)
LANGUAGE plpgsql
AS $$
BEGIN
    RETURN QUERY
    SELECT t.slug
    FROM content.tags t;
END;
$$;

/*
 * content.func_load_timeline_seed_paginated
 * -----------------------------------------
 * Pageable loader extracting essential metadata to rebuild user timelines.
 * p_limit   — Maximum number of rows returned per batch.
 * p_offset  — Offset for pagination.
 *
 * Excludes soft-deleted or banned posts (visibility >= 0).
 * Optimized to retrieve only ID, User ID, and Creation Date.
 * Returns lightweight payloads to instantly reconstruct the chronological
 * ZSETs in the L1 USER Cache during server cold start.
 */
CREATE OR REPLACE FUNCTION content.func_load_timeline_seed_paginated(
    p_limit INTEGER,
    p_offset INTEGER
)
RETURNS TABLE (
    id BIGINT,
    user_id BIGINT,
    created_at TIMESTAMP WITH TIME ZONE
)
LANGUAGE plpgsql STABLE
AS $$
BEGIN
RETURN QUERY
SELECT
    p.id,
    p.user_id,
    p.created_at
FROM content.posts p
WHERE p.visibility >= 0
ORDER BY p.created_at DESC
    LIMIT p_limit OFFSET p_offset;
END;
$$;

/*
 * content.func_save_post
 * ----------------------
 * Ajoute un post aux favoris d'un utilisateur. Ignore silencieusement les doublons.
 */
CREATE OR REPLACE FUNCTION content.func_save_post(p_id bigint, p_user_id bigint, p_post_id bigint)
RETURNS void
LANGUAGE plpgsql
AS $$
BEGIN
INSERT INTO content.saved (id, user_id, post_id, created_at)
VALUES (p_id, p_user_id, p_post_id, now())
    ON CONFLICT (user_id, post_id) DO NOTHING;
END;
$$;

/*
 * content.func_unsave_post
 * ------------------------
 * Retire un post des favoris d'un utilisateur.
 */
CREATE OR REPLACE FUNCTION content.func_unsave_post(p_user_id bigint, p_post_id bigint)
RETURNS void
LANGUAGE plpgsql
AS $$
BEGIN
DELETE FROM content.saved
WHERE user_id = p_user_id AND post_id = p_post_id;
END;
$$;

/*
 * content.func_load_saved_posts
 * -----------------------------
 * Récupère les métadonnées des posts sauvegardés (pagination).
 */
CREATE OR REPLACE FUNCTION content.func_load_saved_posts(p_user_id bigint, p_limit int, p_offset int)
RETURNS TABLE(id bigint, user_id bigint, post_id bigint, created_at timestamp with time zone)
LANGUAGE plpgsql STABLE
AS $$
BEGIN
RETURN QUERY
SELECT
    s.id,
    s.user_id,
    s.post_id,
    s.created_at
FROM content.saved s
WHERE s.user_id = p_user_id
ORDER BY s.created_at DESC
    LIMIT p_limit OFFSET p_offset;
END;
$$;

/*
 * content.func_delete_orphan_saved
 * --------------------------------
 * Purge les favoris orphelins (attachés à des posts supprimés).
 * Retourne les IDs des posts concernés pour répercuter la purge sur MongoDB.
 */
CREATE OR REPLACE FUNCTION content.func_delete_orphan_saved()
RETURNS TABLE(post_id bigint)
LANGUAGE plpgsql
AS $$
BEGIN
RETURN QUERY
DELETE FROM content.saved s
-- On supprime si le post n'existe plus, ou si sa visibilité est < 0 (Soft Delete) ou égale à 2
WHERE NOT EXISTS (
    SELECT 1 FROM content.posts p
    WHERE p.id = s.post_id
      AND p.visibility >= 0
      AND p.visibility != 2
)
    RETURNING s.post_id;
END;
$$;

/*
 * content.func_add_indirect_tag_to_post
 * -------------------------------------
 * Ajoute de manière atomique un hashtag indirect au tableau d'un post.
 * Garantit l'unicité du tag dans le tableau pour éviter les doublons
 * (ex: si plusieurs utilisateurs commentent avec le même tag).
 */
CREATE OR REPLACE FUNCTION content.func_add_indirect_tag_to_post(
    p_post_id bigint,
    p_tag text
)
RETURNS void
LANGUAGE plpgsql
AS $$
BEGIN
UPDATE content.posts
SET indirect_hashtags = array_append(COALESCE(indirect_hashtags, '{}'), p_tag)
WHERE id = p_post_id
  AND NOT (COALESCE(indirect_hashtags, '{}') @> ARRAY[p_tag]::text[]);
END;
$$;

/* ---------------------------------------------------------------------------
   3.3  messaging schema — conversation, member, and message loaders
   --------------------------------------------------------------------------- */

/*
 * messaging.func_load_conversation
 * ---------------------------------
 * Returns all non-archived conversations for a given user, joining through
 * messaging.members to messaging.conversations.  Conversations with state = 1
 * (archived/deleted) are excluded.  Includes per-member unread_count so the
 * inbox list can show notification badges without a separate query.
 */
CREATE OR REPLACE FUNCTION messaging.func_load_conversation(p_user_id bigint)
RETURNS TABLE(
    conversation_id bigint, title text, description text, avatar_id bigint, type smallint, conversation_settings jsonb, external_link jsonb, state smallint, created_at timestamp with time zone, last_message_id bigint, joined_at timestamp with time zone, frozen_message_id bigint, last_read_message_id bigint, role smallint, settings jsonb, unread_count integer
)
LANGUAGE plpgsql
AS $$
BEGIN
RETURN QUERY
SELECT
    c.id AS conversation_id, c.title, c.description, c.avatar_id, c.type, c.settings AS conversation_settings, c.external_link, c.state, c.created_at, c.last_message_id, m.joined_at, m.frozen_message_id, m.last_read_message_id, m.role, m.settings, m.unread_count
FROM messaging.members AS m
         INNER JOIN messaging.conversations AS c ON m.conversation_id = c.id
WHERE m.user_id = p_user_id AND c.state <> 1;
END;
$$;

/*
 * messaging.func_load_active_communities
 * ---------------------------------
 * Retourne les communautés publiques (Type 3) actives pour amorcer la barre
 * de recherche dans le Speed Cache L1.
 */
CREATE OR REPLACE FUNCTION messaging.func_load_active_communities()
RETURNS TABLE(
    id bigint,
    name text,
    description text,
    avatar_id bigint,
    member_count integer
)
LANGUAGE plpgsql STABLE
AS $$
BEGIN
RETURN QUERY
SELECT
    c.id,
    c.title AS name,
    COALESCE(c.description, '') AS description,
    COALESCE(c.avatar_id, 0) AS avatar_id,
    (SELECT COUNT(*)::integer FROM messaging.members m WHERE m.conversation_id = c.id AND m.role >= 0) AS member_count
FROM messaging.conversations c
WHERE c.type = 3 AND c.state = 0;
END;
$$;

/*
 * messaging.func_load_members
 * ----------------------------
 * Returns all participants of a conversation, enriched with user profile data
 * (from auth.users) and profile picture storage path (LEFT JOIN content.media,
 * since a profile picture is optional).
 */
CREATE FUNCTION messaging.func_load_members(
    p_conversation_id bigint
)
RETURNS TABLE(
    user_id                       bigint,
    username                      text,
    grade                         smallint,
    banned                        boolean,
    desactivated                  boolean,
    profile_picture_storage_path  text,
    role                          smallint,
    settings                      jsonb,
    joined_at                     timestamp with time zone,
    unread_count                  integer
)
LANGUAGE plpgsql
AS $$
BEGIN
    RETURN QUERY
    SELECT
        u.id          AS user_id,
        u.username,
        u.grade,
        u.banned,
        u.desactivated,
        med.storage_path AS profile_picture_storage_path,
        m.role,
        m.settings,
        m.joined_at,
        m.unread_count
    FROM messaging.members   AS m
    INNER JOIN auth.users    AS u   ON m.user_id = u.id
    LEFT  JOIN content.media AS med ON u.profile_picture_id = med.id
    WHERE m.conversation_id = p_conversation_id;
END;
$$;


/*
 * messaging.func_load_messages
 * ----------------------------
 * Returns all visible (non-deleted) messages for a conversation, ordered
 * chronologically ascending (oldest first — natural chat display order).
 * Returns SETOF messaging.messages to stay in sync with any future column
 * additions to the messages table without requiring a function signature change.
 */
CREATE FUNCTION messaging.func_load_messages(
    p_conversation_id bigint
)
RETURNS SETOF messaging.messages
LANGUAGE sql
AS $$
    SELECT *
    FROM messaging.messages
    WHERE conversation_id = p_conversation_id
      AND visibility = TRUE          -- exclude soft-deleted messages
    ORDER BY created_at ASC;
$$;

/*
 * messaging.func_get_message_reactions_paginated
 * ----------------------------------------------
 * Retourne la liste détaillée des réactions pour un message spécifique avec pagination.
 * Utilisé pour le "Slow Path" (Bottom Sheet).
 */
CREATE OR REPLACE FUNCTION messaging.func_get_message_reactions_paginated(
    p_message_id bigint,
    p_limit      int,
    p_offset     int
)
RETURNS TABLE(
    id         bigint,
    message_id bigint,
    user_id    bigint,
    reaction   text,
    created_at timestamp with time zone
)
LANGUAGE plpgsql
AS $$
BEGIN
RETURN QUERY
SELECT
    mr.id,
    mr.message_id,
    mr.user_id,
    mr.reaction,
    mr.created_at
FROM messaging.message_reactions mr
WHERE mr.message_id = p_message_id
ORDER BY mr.created_at DESC
    LIMIT p_limit OFFSET p_offset;
END;
$$;

/*
 * messaging.func_update_message_reaction_counts
 * ---------------------------------------------
 * Recalcule les compteurs agrégés d'un message et met à jour le champ JSONB attachments.
 * Cette procédure garantit que le L3 reste la source de vérité.
 */
CREATE OR REPLACE FUNCTION messaging.func_update_message_reaction_counts(
    p_message_id bigint
)
RETURNS void
LANGUAGE plpgsql
AS $$
DECLARE
v_counts jsonb;
BEGIN
    -- Agrégation des réactions
SELECT COALESCE(jsonb_object_agg(reaction, count), '{}'::jsonb)
INTO v_counts
FROM (
         SELECT reaction, count(*) as count
         FROM messaging.message_reactions
         WHERE message_id = p_message_id
         GROUP BY reaction
     ) sub;

-- Mise à jour du champ attachments (on ajoute ou remplace la clé 'reaction_counts')
UPDATE messaging.messages
SET attachments = jsonb_set(COALESCE(attachments, '{}'::jsonb), '{reaction_counts}', v_counts, true)
WHERE id = p_message_id;
END;
$$;

/*
 * messaging.func_apply_reaction_delta
 * -----------------------------------
 * Applique des additions/soustractions directement dans le JSONB 'reaction_counts'
 * de la table messaging.messages, sans recalculer la table des réactions.
 * p_deltas est un JSONB au format {"❤️": 2, "😂": -1}
 */
CREATE OR REPLACE FUNCTION messaging.func_apply_reaction_delta(
    p_message_id bigint,
    p_deltas jsonb
)
RETURNS void
LANGUAGE plpgsql
AS $$
DECLARE
v_current_counts jsonb;
    v_new_counts jsonb := '{}'::jsonb;
    v_key text;
    v_val text;
    v_current_val int;
    v_delta_val int;
    v_new_val int;
BEGIN
    -- 1. Récupérer les compteurs actuels
SELECT COALESCE(attachments->'reaction_counts', '{}'::jsonb)
INTO v_current_counts
FROM messaging.messages
WHERE id = p_message_id;

-- Si le message n'existe pas, on sort silencieusement
IF NOT FOUND THEN
        RETURN;
END IF;

    -- 2. Initialiser la nouvelle map avec l'actuelle
    v_new_counts := v_current_counts;

    -- 3. Itérer sur chaque clé/valeur du payload p_deltas
FOR v_key, v_val IN SELECT * FROM jsonb_each_text(p_deltas)
                                      LOOP
                                  -- Récupérer la valeur actuelle (0 si non trouvée)
    v_current_val := COALESCE((v_current_counts->>v_key)::int, 0);

-- Récupérer le delta
v_delta_val := v_val::int;

        -- Calculer la nouvelle valeur
        v_new_val := v_current_val + v_delta_val;

        -- 4. Appliquer ou supprimer selon le résultat
        IF v_new_val <= 0 THEN
            -- Si ça tombe à 0 (ou en dessous), on supprime la clé
            v_new_counts := v_new_counts - v_key;
ELSE
            -- Sinon on met à jour avec la nouvelle valeur
            v_new_counts := jsonb_set(v_new_counts, ARRAY[v_key], to_jsonb(v_new_val), true);
END IF;
END LOOP;

    -- 5. Sauvegarder dans la base
UPDATE messaging.messages
SET attachments = jsonb_set(COALESCE(attachments, '{}'::jsonb), '{reaction_counts}', v_new_counts, true)
WHERE id = p_message_id;
END;
$$;

/*
 * messaging.func_delete_orphan_reactions
 * --------------------------------------
 * Purge les réactions orphelines (attachées à des messages supprimés).
 * Retourne les IDs des messages concernés pour répercuter la purge sur MongoDB.
 */
CREATE OR REPLACE FUNCTION messaging.func_delete_orphan_reactions()
RETURNS TABLE(message_id bigint)
LANGUAGE plpgsql
AS $$
BEGIN
RETURN QUERY
DELETE FROM messaging.message_reactions r
-- On supprime si le message parent n'existe plus ou s'il est invisible (visibility = false)
WHERE NOT EXISTS (
    SELECT 1 FROM messaging.messages m
    WHERE m.id = r.message_id
      AND m.visibility = TRUE
)
    RETURNING r.message_id;
END;
$$;

/*
 * messaging.func_load_message_ids_paginated
 * -----------------------------------------
 * Returns strictly the IDs to prevent memory overload. Solves the Cache Hole problem.
 */
CREATE OR REPLACE FUNCTION messaging.func_load_message_ids_paginated(
    p_conv_id bigint,
    p_offset_id bigint,
    p_limit integer,
    p_direction text,
    p_frozen_id bigint DEFAULT NULL -- ✅ NOUVEAU PARAMÈTRE
)
RETURNS TABLE(id bigint)
LANGUAGE plpgsql STABLE
AS $$
BEGIN
    IF p_direction = 'top' THEN
        -- Historique : plus anciens que l'offset
        RETURN QUERY
SELECT m.id FROM messaging.messages m
WHERE m.conversation_id = p_conv_id
  AND m.visibility = TRUE
  AND (p_offset_id = 0 OR m.id < p_offset_id)
  AND (p_frozen_id IS NULL OR m.id <= p_frozen_id) -- ✅ FILTRE DE GEL
ORDER BY m.id DESC
    LIMIT p_limit;
ELSE
        -- Nouveaux messages : plus récents que l'offset
        RETURN QUERY
SELECT m.id FROM messaging.messages m
WHERE m.conversation_id = p_conv_id
  AND m.visibility = TRUE
  AND (p_offset_id = 0 OR m.id > p_offset_id)
  AND (p_frozen_id IS NULL OR m.id <= p_frozen_id) -- ✅ FILTRE DE GEL
ORDER BY m.id ASC
    LIMIT p_limit;
END IF;
END;
$$;

/*
 * messaging.func_load_messages_by_ids
 * -----------------------------------
 * Fallback L3 for Object Cache hydratation.
 */
CREATE OR REPLACE FUNCTION messaging.func_load_messages_by_ids(
    p_message_ids bigint[]
)
RETURNS SETOF messaging.messages
LANGUAGE sql STABLE
AS $$
SELECT * FROM messaging.messages
WHERE id = ANY(p_message_ids)
ORDER BY id ASC;
$$;

/*
 * messaging.func_load_active_conversations
 * ----------------------------------------
 * Returns essential metadata for all active conversations (state = 0).
 * Used exclusively for rebuilding the L1 SPEED Cache during server cold starts.
 */
CREATE OR REPLACE FUNCTION messaging.func_load_active_conversations()
RETURNS TABLE(
    id bigint, type smallint, title text, description text, avatar_id bigint, last_message_id bigint, settings jsonb, external_link
)
LANGUAGE plpgsql STABLE
AS $$
BEGIN
RETURN QUERY
SELECT c.id, c.type, c.title, c.description, c.avatar_id, c.last_message_id, c.settings, c.external_link
FROM messaging.conversations c
WHERE c.state = 0;
END;
$$;

/*
 * messaging.func_load_active_members
 * ----------------------------------
 * Returns membership records joined with active conversation metadata.
 * Used exclusively for rebuilding the Inbox ZSETs during server cold starts.
 */
CREATE OR REPLACE FUNCTION messaging.func_load_active_members()
RETURNS TABLE(
    conversation_id bigint,
    user_id bigint,
    role smallint,
    settings jsonb,
    unread_count integer,
    frozen_message_id bigint,
    last_read_message_id bigint,
    last_message_id bigint,
    joined_at timestamp with time zone -- ✅ NOUVEAU
)
LANGUAGE plpgsql STABLE
AS $$
BEGIN
RETURN QUERY
SELECT m.conversation_id, m.user_id, m.role, m.settings, m.unread_count, m.frozen_message_id, m.last_read_message_id, c.last_message_id, m.joined_at -- ✅ NOUVEAU
FROM messaging.members m
         JOIN messaging.conversations c ON m.conversation_id = c.id
WHERE c.state = 0;
END;
$$;

CREATE OR REPLACE FUNCTION messaging.func_load_muted_members_paginated(
    p_conv_id bigint,
    p_limit integer,
    p_offset integer
)
RETURNS TABLE(
    user_id bigint,
    restricted_until bigint
)
LANGUAGE plpgsql STABLE
AS $$
BEGIN
RETURN QUERY
SELECT m.user_id, COALESCE((m.settings->>'restricted_until')::bigint, 0) AS restricted_until
FROM messaging.members m
WHERE m.conversation_id = p_conv_id
  AND COALESCE((m.settings->>'restricted_until')::bigint, 0) > (extract(epoch from now()) * 1000)::bigint
ORDER BY COALESCE((m.settings->>'restricted_until')::bigint, 0) DESC
    LIMIT p_limit OFFSET p_offset;
END;
$$;

/*
 * messaging.func_load_conversation_fallback
 * ----------------------------------
 * Resolves specific missing conversations from the Inbox.
 * Wraps func_load_conversation to filter only on requested conversation IDs.
 */
CREATE OR REPLACE FUNCTION messaging.func_load_conversation_fallback(p_user_id bigint, p_conv_ids bigint[])
RETURNS TABLE(
    conversation_id bigint, title text, description text, avatar_id bigint, type smallint, conversation_settings jsonb, external_link jsonb, last_message_id bigint, role smallint, settings jsonb, frozen_message_id bigint, unread_count integer, joined_at timestamp with time zone
)
LANGUAGE plpgsql STABLE
AS $$
BEGIN
RETURN QUERY
SELECT f.conversation_id, f.title, f.description, f.avatar_id, f.type, f.conversation_settings, f.last_message_id, f.role, f.settings, f.external_link, f.frozen_message_id, f.last_read_message_id, f.unread_count, f.joined_at
FROM messaging.func_load_conversation(p_user_id) f
WHERE f.conversation_id = ANY(p_conv_ids);
END;
$$;

/*
 * messaging.func_load_conversation_paginated
 * ----------------------------------------
 * Returns FULL payloads for both conversations and members, sorted by last_message_id.
 * Used for L3 Fallback to properly rehydrate MongoDB (L2) and Redis (L1).
 */
CREATE OR REPLACE FUNCTION messaging.func_load_conversation_paginated(p_user_id bigint, p_limit integer, p_offset integer)
RETURNS TABLE(
    conv_id bigint, conv_type smallint, conv_title text, conv_description text, conv_avatar_id bigint, conv_last_msg_id bigint, conv_state smallint, conv_settings jsonb, external_link jsonb, conv_created timestamp with time zone, conv_updated timestamp with time zone,
    mem_id bigint, mem_conv_id bigint, mem_user_id bigint, mem_role smallint, mem_settings jsonb, mem_joined timestamp with time zone, mem_frozen_id bigint, mem_last_read_message_id bigint, mem_unread integer, mem_created timestamp with time zone, mem_updated timestamp with time zone -- ✅ NOUVEAU
)
LANGUAGE plpgsql STABLE
AS $$
BEGIN
RETURN QUERY
SELECT
    c.id, c.type, c.title, c.description, c.avatar_id, c.last_message_id, c.state, c.settings, c.external_link, c.created_at, c.updated_at,
    m.id, m.conversation_id, m.user_id, m.role, m.settings, m.joined_at, m.frozen_message_id, m.last_read_message_id, m.unread_count, m.created_at, m.updated_at -- ✅ NOUVEAU
FROM messaging.members m
         JOIN messaging.conversations c ON m.conversation_id = c.id
WHERE m.user_id = p_user_id AND c.state <> 1
ORDER BY c.last_message_id DESC NULLS LAST
    LIMIT p_limit OFFSET p_offset;
END;
$$;

/*
 * messaging.func_get_conversation
 * -------------------------------
 * Returns the full payload of a single conversation by its ID.
 * Used for hydratation and security checks before updates.
 */
CREATE OR REPLACE FUNCTION messaging.func_get_conversation(p_conv_id bigint)
RETURNS TABLE(
    id bigint, type smallint, title text, description text, avatar_id bigint, last_message_id bigint, state smallint, settings jsonb, external_link jsonb, created_at timestamp with time zone, updated_at timestamp with time zone
)
LANGUAGE plpgsql STABLE
AS $$
BEGIN
RETURN QUERY
SELECT c.id, c.type, c.title, c.description, c.avatar_id, c.last_message_id, c.state, c.settings, c.external_link, c.created_at, c.updated_at
FROM messaging.conversations c
WHERE c.id = p_conv_id;
END;
$$;

/*
 * messaging.func_get_direct_conversation
 * --------------------------------------
 * Retourne la conversation privée complète (type = 0, state = 0)
 * partagée activement par les deux utilisateurs.
 */
CREATE OR REPLACE FUNCTION messaging.func_get_direct_conversation(p_user1 bigint, p_user2 bigint)
RETURNS TABLE(
    id bigint, type smallint, title text, description text, avatar_id bigint, last_message_id bigint, state smallint, settings jsonb, external_link jsonb, created_at timestamp with time zone, updated_at timestamp with time zone
)
LANGUAGE plpgsql STABLE
AS $$
BEGIN
RETURN QUERY
SELECT c.id, c.type, c.title, c.description, c.avatar_id, c.last_message_id, c.state, c.settings, c.external_link, c.created_at, c.updated_at
FROM messaging.conversations c
         JOIN messaging.members m1 ON c.id = m1.conversation_id
         JOIN messaging.members m2 ON c.id = m2.conversation_id
WHERE c.type = 0 AND c.state = 0 AND m1.user_id = p_user1 AND m2.user_id = p_user2
    LIMIT 1;
END;
$$;

/*
 * messaging.func_get_member
 * -------------------------
 * Returns the full payload of a single member by conversation_id and user_id.
 * SMART RE-COUNT: Recalcule les non-lus à la volée si la donnée est à 0 ou a plus de 5s de retard.
 */
CREATE OR REPLACE FUNCTION messaging.func_get_member(
    p_conv_id bigint,
    p_user_id bigint
)
RETURNS TABLE(
    id bigint, conversation_id bigint, user_id bigint, role smallint, settings jsonb,
    joined_at timestamp with time zone, unread_count integer,
    frozen_message_id bigint, last_read_message_id bigint, -- ✅ NOUVEAU
    created_at timestamp with time zone, updated_at timestamp with time zone
)
LANGUAGE plpgsql STABLE
AS $$
DECLARE
v_mem messaging.members%ROWTYPE;
    v_new_messages integer;
BEGIN
SELECT * INTO v_mem
FROM messaging.members
WHERE conversation_id = p_conv_id AND user_id = p_user_id;

IF NOT FOUND THEN
        RETURN;
END IF;

    -- SMART RE-COUNT
    IF v_mem.unread_count = 0 OR v_mem.updated_at < (NOW() - INTERVAL '5 seconds') THEN
SELECT COUNT(*) INTO v_new_messages
FROM messaging.messages
WHERE conversation_id = p_conv_id
  AND created_at > v_mem.updated_at
  AND sender_id != p_user_id;

v_mem.unread_count := v_mem.unread_count + v_new_messages;
END IF;

RETURN QUERY SELECT
        v_mem.id, v_mem.conversation_id, v_mem.user_id, v_mem.role, v_mem.settings,
        v_mem.joined_at, v_mem.unread_count, v_mem.frozen_message_id,
        v_mem.last_read_message_id, -- ✅ NOUVEAU
        v_mem.created_at, v_mem.updated_at;
END;
$$;

/*
 * messaging.func_get_conversation_participant_ids
 * -----------------------------------------------
 * Returns strictly the user IDs of a conversation for L1 Speed Cache auto-healing.
 */
CREATE OR REPLACE FUNCTION messaging.func_get_conversation_participant_ids(
    p_conv_id bigint
)
RETURNS TABLE(user_id bigint)
LANGUAGE plpgsql STABLE
AS $$
BEGIN
RETURN QUERY
SELECT m.user_id
FROM messaging.members m
WHERE m.conversation_id = p_conv_id;
END;
$$;

/*
    * messaging.func_load_members_by_role_paginated
    * ---------------------------------------------
    * Returns members of a conversation filtered by role, with pagination.
    * Used for moderation and admin tools to inspect specific roles (e.g., admins, moderators).
 */
CREATE OR REPLACE FUNCTION messaging.func_load_members_by_role_paginated(
    p_conv_id bigint,
    p_role smallint,
    p_limit integer,
    p_offset integer
)
RETURNS TABLE(
    id bigint, conversation_id bigint, user_id bigint, role smallint,
    joined_at timestamp with time zone, unread_count integer,
    frozen_message_id bigint, last_read_message_id bigint, created_at timestamp with time zone,
    updated_at timestamp with time zone
)
LANGUAGE plpgsql STABLE
AS $$
BEGIN
RETURN QUERY
SELECT m.id, m.conversation_id, m.user_id, m.role, m.joined_at,
       m.unread_count, m.frozen_message_id, m.last_read_message_id, m.created_at, m.updated_at
FROM messaging.members m
WHERE m.conversation_id = p_conv_id AND m.role = p_role
ORDER BY m.joined_at DESC
    LIMIT p_limit OFFSET p_offset;
END;
$$;

/* ---------------------------------------------------------------------------
   3.4  moderation schema — report management procedures
   --------------------------------------------------------------------------- */

/*
 * moderation.proc_create_report
 * ------------------------------
 * Inserts a new abuse/policy-violation report.  Called by the application
 * whenever a user submits a report via the UI.  rationale is left NULL on
 * creation and populated later by a moderator via proc_update_report.
 */
CREATE PROCEDURE moderation.proc_create_report(
    IN p_actor_id    bigint,
    IN p_target_type smallint,
    IN p_target_id   bigint,
    IN p_reason      text,
    IN p_state       smallint
)
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO moderation.reports(actor_id, target_type, target_id, reason, rationale, state)
    VALUES (p_actor_id, p_target_type, p_target_id, p_reason, NULL, p_state);
END;
$$;


/*
 * moderation.proc_update_report
 * ------------------------------
 * Updates the state and moderator rationale of an existing report.
 * Called from the moderation dashboard when a moderator takes action
 * (approve, dismiss, escalate, etc.).
 */
CREATE PROCEDURE moderation.proc_update_report(
    IN p_report_id    bigint,
    IN p_new_state    smallint,
    IN p_new_rationale text
)
LANGUAGE plpgsql
AS $$
BEGIN
    UPDATE moderation.reports
    SET
        state     = p_new_state,
        rationale = p_new_rationale
    WHERE id = p_report_id;
END;
$$;

/*
 * content.func_increment_post_report
 * ----------------------------------
 * Atomic updater for post report counts, preventing negative values.
 */
CREATE OR REPLACE FUNCTION content.func_increment_post_report(p_post_id bigint, p_delta integer)
RETURNS void
LANGUAGE plpgsql
AS $$
BEGIN
UPDATE content.posts
SET report_count = GREATEST(0, report_count + p_delta)
WHERE id = p_post_id;
END;
$$;


/* =============================================================================
   SECTION 4 — VIEWS
   All base tables and functions are now in place.  Views are created here so
   their definitions can reference any object already defined above.
   ============================================================================= */

/*
 * views.user_public_profile
 * -------------------------
 * Denormalised projection of a user's publicly-visible profile fields,
 * joined with their settings record (privacy, notification prefs, language,
 * theme).  Used by the public profile page and the user-search results.
 * LEFT JOIN on user_settings handles users who have no settings row yet.
 */
CREATE OR REPLACE VIEW views.user_public_profile AS
SELECT
    u.id               AS user_id,
    u.username,
    u.first_name,
    u.last_name,
    u.birthdate,
    u.sex,
    u.bio,
    u.profile_picture_id,
    u.grade,
    u.location,
    u.school,
    u.work,
    u.badges
FROM auth.users u;


/*
 * views.user_relations_view
 * -------------------------
 * Enriched social-graph edge view that resolves both usernames from the
 * same auth.users table.  Aliases the generic primary/secondary IDs to the
 * more semantic follower_id / followed_id naming for API clarity.
 */
CREATE VIEW views.user_relations_view AS
SELECT
    r.id           AS relation_id,
    r.primary_id   AS follower_id,
    u1.username    AS follower_username,
    r.secondary_id AS followed_id,
    u2.username    AS followed_username,
    r.state,
    r.created_at
FROM auth.relations r
JOIN auth.users u1 ON r.primary_id   = u1.id
JOIN auth.users u2 ON r.secondary_id = u2.id;


/*
 * views.conversation_participants_view
 * ------------------------------------
 * Full participant roster for every conversation, joining membership records
 * with conversation metadata and user identity.  Used by the group-info panel
 * and admin tools to inspect conversation membership.
 */
CREATE OR REPLACE VIEW views.conversation_participants_view AS
SELECT
    cm.conversation_id,
    cm.user_id,
    u.username,
    u.first_name,
    u.last_name,
    cm.role,
    cm.settings,
    cm.joined_at,
    cm.unread_count,
    cm.last_read_message_id, -- ✅ NOUVEAU
    conv.type        AS conversation_type,
    conv.title       AS conversation_title,
    conv.description AS conversation_description,
    conv.avatar_id   AS conversation_avatar_id,
    conv.state       AS conversation_state,
    conv.created_at  AS conversation_created_at
FROM messaging.members       cm
         JOIN messaging.conversations conv ON cm.conversation_id = conv.id
         JOIN auth.users              u    ON cm.user_id         = u.id;


/*
 * views.conversation_summary
 * --------------------------
 * Per-member conversation summary including the most-recent message details,
 * resolved via a LATERAL subquery (one correlated scan per member row, ordered
 * by created_at DESC LIMIT 1).  Powers the inbox list "last message" preview.
 */
CREATE VIEW views.conversation_summary AS
SELECT
    cm.conversation_id,
    cm.user_id,
    cm.role,
    cm.settings
    cm.joined_at,
    cm.unread_count,
    last_msg.id           AS last_message_id,
    last_msg.sender_id    AS last_sender_id,
    last_msg.message_type AS last_message_type,
    last_msg.content      AS last_message_content,
    last_msg.created_at   AS last_message_time
FROM messaging.members cm
LEFT JOIN LATERAL (
    SELECT
        m.id,
        m.sender_id,
        m.message_type,
        m.content,
        m.created_at
    FROM messaging.messages m
    WHERE m.conversation_id = cm.conversation_id AND (cm.frozen_message_id IS NULL OR m.id <= cm.frozen_message_id) -- LE FILTRE EST ICI
    ORDER BY m.created_at DESC
    LIMIT 1
) last_msg ON true;


/*
 * views.conversation_user_view
 * ----------------------------
 * Aggregates all conversation IDs for each user, sorted by the most recent
 * *incoming* message (from another sender) so the inbox appears in
 * activity order.  The array_agg with ORDER BY allows the application to
 * iterate in priority order without additional sorting.
 */
CREATE VIEW views.conversation_user_view AS
SELECT
    cm.user_id,
    array_agg(
        cm.conversation_id
        ORDER BY last_msg.created_at DESC NULLS LAST
    ) AS conversation_ids
FROM messaging.members cm
LEFT JOIN LATERAL (
    SELECT m.created_at
    FROM messaging.messages m
    WHERE m.conversation_id = cm.conversation_id
      AND m.sender_id <> cm.user_id   -- only messages from *other* participants
      AND (cm.frozen_message_id IS NULL OR m.id <= cm.frozen_message_id)
    ORDER BY m.created_at DESC
    LIMIT 1
) last_msg ON true
GROUP BY cm.user_id;


/*
 * views.post_engagement_view
 * --------------------------
 * Real-time engagement metrics view (live counts from content.likes and
 * content.comments) joined onto posts.  Also surfaces the vector fields
 * so recommendation engines can query a single view.
 * COALESCE ensures 0 is returned for posts that have no likes or comments.
 */
CREATE OR REPLACE VIEW views.post_engagement_view AS
SELECT
    p.id               AS post_id,
    p.user_id,
    p.content,
    p.media_ids,
    p.visibility,
    p.location,
    p.created_at,
    p.updated_at,
    p.vector,
    p.vector_version,
    p.report_count, -- ✅ NOUVEAU
    COALESCE(l.like_count,    0::bigint) AS like_count,
    COALESCE(c.comment_count, 0::bigint) AS comment_count
FROM content.posts p
LEFT JOIN (
    SELECT target_id, COUNT(*) AS like_count
    FROM   content.likes
    WHERE  target_type = 0           -- 0 = post reaction
    GROUP  BY target_id
) l ON p.id = l.target_id
LEFT JOIN (
    SELECT post_id, COUNT(*) AS comment_count
    FROM   content.comments
    GROUP  BY post_id
) c ON p.id = c.post_id;


/* ---------------------------------------------------------------------------
   4.1  views schema — thin wrapper functions over the views above
   These functions are placed after the views they reference.
   --------------------------------------------------------------------------- */

/*
 * views.func_load_user_public_profile
 * ------------------------------------
 * Parameterised wrapper for views.user_public_profile.
 * Passing NULL for p_user_id returns all profiles (use with care).
 *
 * BUG FIXED: return column theme corrected from TEXT → SMALLINT to match
 * the auth.user_settings.theme column type.
 */
CREATE OR REPLACE FUNCTION views.func_load_user_public_profile(
    p_user_id bigint DEFAULT NULL::bigint
)
RETURNS TABLE(
    user_id             bigint,
    username            text,
    first_name          text,
    last_name           text,
    birthdate           date,
    sex                 smallint,
    bio                 text,
    profile_picture_id  bigint,
    grade               smallint,
    location            text,
    school              text,
    work                text,
    badges              jsonb
    -- On supprime privacy, notifications et display_and_content ici aussi
)
LANGUAGE plpgsql STABLE
AS $$
BEGIN
    RETURN QUERY
    SELECT *
    FROM views.user_public_profile
    WHERE (p_user_id IS NULL OR user_public_profile.user_id = p_user_id);
END;
$$;


/*
 * views.func_load_user_relations_view
 * ------------------------------------
 * Parameterised wrapper for views.user_relations_view.
 * Supports one-sided or two-sided filtering (both params optional).
 */
CREATE FUNCTION views.func_load_user_relations_view(
    p_follower_id bigint DEFAULT NULL::bigint,
    p_followed_id bigint DEFAULT NULL::bigint
)
RETURNS TABLE(
    relation_id       bigint,
    follower_id       bigint,
    follower_username text,
    followed_id       bigint,
    followed_username text,
    state             smallint,
    created_at        timestamp with time zone
)
LANGUAGE plpgsql
AS $$
BEGIN
    RETURN QUERY
    SELECT *
    FROM views.user_relations_view
    WHERE
        (p_follower_id IS NULL OR user_relations_view.follower_id = p_follower_id)
        AND (p_followed_id IS NULL OR user_relations_view.followed_id = p_followed_id);
END;
$$;


/*
 * views.func_load_conversation_participants_view
 * -----------------------------------------------
 * Parameterised wrapper for views.conversation_participants_view.
 * Filter by conversation, by user, or by both simultaneously.
 */
CREATE OR REPLACE FUNCTION views.func_load_conversation_participants_view(
    p_conversation_id bigint DEFAULT NULL::bigint,
    p_user_id         bigint DEFAULT NULL::bigint
)
RETURNS TABLE(
    conversation_id       bigint,
    user_id               bigint,
    username              text,
    first_name            text,
    last_name             text,
    role                  smallint,
    settings              jsonb,
    joined_at             timestamp with time zone,
    unread_count          integer,
    last_read_message_id  bigint, -- ✅ NOUVEAU
    conversation_type     smallint,
    conversation_title    text,
    conversation_description text,
    conversation_avatar_id   bigint,
    conversation_state    smallint,
    conversation_created_at timestamp with time zone
)
LANGUAGE plpgsql
AS $$
BEGIN
RETURN QUERY
SELECT *
FROM views.conversation_participants_view
WHERE
    (p_conversation_id IS NULL OR conversation_participants_view.conversation_id = p_conversation_id)
  AND (p_user_id     IS NULL OR conversation_participants_view.user_id         = p_user_id);
END;
$$;


/*
 * views.func_load_conversation_summary
 * -------------------------------------
 * Parameterised wrapper for views.conversation_summary.
 * Returns the inbox-preview row(s) for a user, or the summary for one
 * specific conversation.
 */
CREATE FUNCTION views.func_load_conversation_summary(
    p_conversation_id bigint DEFAULT NULL::bigint,
    p_user_id         bigint DEFAULT NULL::bigint
)
RETURNS TABLE(
    conversation_id      bigint,
    user_id              bigint,
    role                 smallint,
    settings             jsonb,
    joined_at            timestamp with time zone,
    unread_count         integer,
    last_message_id      bigint,
    last_sender_id       bigint,
    last_message_type    smallint,
    last_message_content text,
    last_message_time    timestamp with time zone
)
LANGUAGE plpgsql
AS $$
BEGIN
    RETURN QUERY
    SELECT
        s.conversation_id,
        s.user_id,
        s.role,
        s.settings,
        s.joined_at,
        s.unread_count,
        s.last_message_id,
        s.last_sender_id,
        s.last_message_type,
        s.last_message_content,
        s.last_message_time
    FROM views.conversation_summary AS s
    WHERE
        (p_conversation_id IS NULL OR s.conversation_id = p_conversation_id)
        AND (p_user_id     IS NULL OR s.user_id         = p_user_id);
END;
$$;


/*
 * views.func_load_conversation_user_view
 * ----------------------------------------
 * Parameterised wrapper for views.conversation_user_view.
 * Returns the ordered conversation_ids array for one (or all) users.
 */
CREATE FUNCTION views.func_load_conversation_user_view(
    p_user_id bigint DEFAULT NULL::bigint
)
RETURNS TABLE(
    user_id          bigint,
    conversation_ids bigint[]
)
LANGUAGE plpgsql
AS $$
BEGIN
    RETURN QUERY
    SELECT *
    FROM views.conversation_user_view
    WHERE (p_user_id IS NULL OR conversation_user_view.user_id = p_user_id);
END;
$$;


/*
 * views.func_load_post_engagement_view
 * -------------------------------------
 * Parameterised wrapper for views.post_engagement_view.
 * Filter by post ID, by author user ID, or by both.
 */
CREATE OR REPLACE FUNCTION views.func_load_post_engagement_view(
    p_post_id bigint DEFAULT NULL::bigint,
    p_user_id bigint DEFAULT NULL::bigint
)
RETURNS TABLE(
    post_id        bigint,
    user_id        bigint,
    content        text,
    media_ids      bigint[],
    visibility     smallint,
    location       text,
    created_at     timestamp with time zone,
    updated_at     timestamp with time zone,
    vector         real[],   -- 🔧 CORRECTION (Manquant dans le dump)
    vector_version integer,  -- 🔧 CORRECTION (Manquant dans le dump)
    report_count   integer,  -- ✅ NOUVEAU
    like_count     bigint,
    comment_count  bigint
)
LANGUAGE plpgsql
AS $$
BEGIN
RETURN QUERY
SELECT *
FROM views.post_engagement_view
WHERE
    (p_post_id IS NULL OR post_engagement_view.post_id = p_post_id)
  AND (p_user_id IS NULL OR post_engagement_view.user_id = p_user_id);
END;
$$;

/*
 * views.func_check_unique
 * -----------------------
 * Vérifie dynamiquement et de manière sécurisée l'unicité d'une valeur
 * dans n'importe quelle table. Utilise quote_ident (%I) pour empêcher
 * toute injection SQL sur les noms de schémas, tables et colonnes.
 * Retourne TRUE si la valeur existe, FALSE sinon.
 */
CREATE OR REPLACE FUNCTION views.func_check_unique(
    p_schema text,
    p_table text,
    p_field text,
    p_value text
)
RETURNS boolean
LANGUAGE plpgsql STABLE
AS $$
DECLARE
v_exists boolean;
BEGIN
EXECUTE format('SELECT EXISTS(SELECT 1 FROM %I.%I WHERE %I = $1)', p_schema, p_table, p_field)
    INTO v_exists
    USING p_value;

RETURN v_exists;
END;
$$;


/* =============================================================================
   SECTION 5 — CONSTRAINTS
   Primary keys, unique constraints, and foreign keys are all added here via
   ALTER TABLE so they can reference any object already created above without
   risking missing-object errors.
   ============================================================================= */

/* ---------------------------------------------------------------------------
   5.1  auth schema constraints
   --------------------------------------------------------------------------- */

-- Primary key on auth.users
ALTER TABLE ONLY auth.users
    ADD CONSTRAINT users_pkey PRIMARY KEY (id);

-- Business-key uniqueness on auth.users
ALTER TABLE ONLY auth.users
    ADD CONSTRAINT users_username_key UNIQUE (username);
ALTER TABLE ONLY auth.users
    ADD CONSTRAINT users_email_key    UNIQUE (email);
ALTER TABLE ONLY auth.users
    ADD CONSTRAINT users_phone_key    UNIQUE (phone);

-- Primary key on auth.relations
ALTER TABLE ONLY auth.relations
    ADD CONSTRAINT relations_pkey PRIMARY KEY (id);

-- No duplicate directed edge (secondary_id, primary_id) — reversed order
-- matches the original dump and provides the most useful composite index
ALTER TABLE ONLY auth.relations
    ADD CONSTRAINT relations_secondary_id_primary_id_key UNIQUE (secondary_id, primary_id);

-- Foreign keys on auth.relations → auth.users
ALTER TABLE ONLY auth.relations
    ADD CONSTRAINT relations_primary_id_fkey
        FOREIGN KEY (primary_id)   REFERENCES auth.users(id);
ALTER TABLE ONLY auth.relations
    ADD CONSTRAINT relations_secondary_id_fkey
        FOREIGN KEY (secondary_id) REFERENCES auth.users(id);

-- Primary key on auth.user_settings
ALTER TABLE ONLY auth.user_settings
    ADD CONSTRAINT user_settings_pkey        PRIMARY KEY (id);
ALTER TABLE ONLY auth.user_settings
    ADD CONSTRAINT user_settings_user_id_key UNIQUE (user_id);

-- Foreign key on auth.user_settings → auth.users (CASCADE: removing a user removes their settings)
ALTER TABLE ONLY auth.user_settings
    ADD CONSTRAINT user_settings_user_id_fkey
        FOREIGN KEY (user_id) REFERENCES auth.users(id) ON DELETE CASCADE;

-- Primary key on auth.sessions
ALTER TABLE ONLY auth.sessions
    ADD CONSTRAINT sessions_pkey PRIMARY KEY (id);

-- Foreign key on auth.sessions → auth.users
ALTER TABLE ONLY auth.sessions
    ADD CONSTRAINT sessions_user_id_fkey
        FOREIGN KEY (user_id) REFERENCES auth.users(id);


/* ---------------------------------------------------------------------------
   5.2  content schema constraints
   --------------------------------------------------------------------------- */

-- Primary key on content.posts
ALTER TABLE ONLY content.posts
    ADD CONSTRAINT posts_pkey PRIMARY KEY (id);

-- Foreign key on content.posts → auth.users
ALTER TABLE ONLY content.posts
    ADD CONSTRAINT posts_user_id_fkey
        FOREIGN KEY (user_id) REFERENCES auth.users(id);

-- Primary key on content.comments
ALTER TABLE ONLY content.comments
    ADD CONSTRAINT comments_pkey PRIMARY KEY (id);

-- Foreign keys on content.comments
ALTER TABLE ONLY content.comments
    ADD CONSTRAINT comments_post_id_fkey
        FOREIGN KEY (post_id) REFERENCES content.posts(id) ON DELETE CASCADE;
ALTER TABLE ONLY content.comments
    ADD CONSTRAINT comments_user_id_fkey
        FOREIGN KEY (user_id) REFERENCES auth.users(id);

-- Primary key on content.media
ALTER TABLE ONLY content.media
    ADD CONSTRAINT media_pkey PRIMARY KEY (id);

-- Foreign key on content.media → auth.users
ALTER TABLE ONLY content.media
    ADD CONSTRAINT media_owner_id_fkey
        FOREIGN KEY (owner_id) REFERENCES auth.users(id);

-- Primary key on content.likes
ALTER TABLE ONLY content.likes
    ADD CONSTRAINT likes_pkey PRIMARY KEY (id);

-- Prevent duplicate likes: one like per user per entity
ALTER TABLE ONLY content.likes
    ADD CONSTRAINT likes_target_type_target_id_user_id_key
        UNIQUE (target_type, target_id, user_id);

-- Foreign key on content.likes → auth.users
ALTER TABLE ONLY content.likes
    ADD CONSTRAINT likes_user_id_fkey
        FOREIGN KEY (user_id) REFERENCES auth.users(id);

-- Foreign key on content.likes → content.posts (only for target_type = 0)
ALTER TABLE ONLY content.saved
    ADD CONSTRAINT saved_pkey
        PRIMARY KEY (id);

-- Composite uniqueness on (user_id, post_id) to prevent duplicate saves
ALTER TABLE ONLY content.saved
    ADD CONSTRAINT saved_user_id_post_id_key
        UNIQUE (user_id, post_id);

-- Foreign keys on content.saved → auth.users and content.posts
ALTER TABLE ONLY content.saved
    ADD CONSTRAINT saved_user_id_fkey
        FOREIGN KEY (user_id) REFERENCES auth.users(id) ON DELETE CASCADE;

-- Foreign key on content.saved → content.posts (CASCADE: deleting a post removes its saved entries)
ALTER TABLE ONLY content.saved
    ADD CONSTRAINT saved_post_id_fkey
        FOREIGN KEY (post_id) REFERENCES content.posts(id) ON DELETE CASCADE;

-- Note: content.tags uses an inline PRIMARY KEY on slug (declared in CREATE TABLE)


/* ---------------------------------------------------------------------------
   5.3  messaging schema constraints
   --------------------------------------------------------------------------- */

-- Primary key on messaging.conversations
ALTER TABLE ONLY messaging.conversations
    ADD CONSTRAINT conversations_pkey            PRIMARY KEY (id);
ALTER TABLE ONLY messaging.conversations
    ADD CONSTRAINT conversations_last_message_id_key UNIQUE (last_message_id);

-- Primary key on messaging.messages
ALTER TABLE ONLY messaging.messages
    ADD CONSTRAINT messages_pkey PRIMARY KEY (id);

-- Foreign key on messaging.messages → messaging.conversations
ALTER TABLE ONLY messaging.messages
    ADD CONSTRAINT messages_conversation_id_fkey
        FOREIGN KEY (conversation_id) REFERENCES messaging.conversations(id);

-- Primary key on messaging.message_reactions
ALTER TABLE ONLY messaging.message_reactions
    ADD CONSTRAINT message_reactions_pkey PRIMARY KEY (id);

-- Prevent duplicate reactions: one reaction per user per message
ALTER TABLE ONLY messaging.message_reactions
    ADD CONSTRAINT message_reactions_message_id_user_id_key
    UNIQUE (message_id, user_id);

-- Foreign keys on messaging.message_reactions
ALTER TABLE ONLY messaging.message_reactions
    ADD CONSTRAINT message_reactions_message_id_fkey
    FOREIGN KEY (message_id) REFERENCES messaging.messages(id) ON DELETE CASCADE;
ALTER TABLE ONLY messaging.message_reactions
    ADD CONSTRAINT message_reactions_user_id_fkey
    FOREIGN KEY (user_id) REFERENCES auth.users(id);

-- Primary key on messaging.members
ALTER TABLE ONLY messaging.members
    ADD CONSTRAINT members_pkey                     PRIMARY KEY (id);
ALTER TABLE ONLY messaging.members
    ADD CONSTRAINT members_conversation_id_user_id_key UNIQUE (conversation_id, user_id);

-- Foreign keys on messaging.members
ALTER TABLE ONLY messaging.members
    ADD CONSTRAINT members_conversation_id_fkey
        FOREIGN KEY (conversation_id) REFERENCES messaging.conversations(id);
ALTER TABLE ONLY messaging.members
    ADD CONSTRAINT members_user_id_fkey
        FOREIGN KEY (user_id) REFERENCES auth.users(id);


/* ---------------------------------------------------------------------------
   5.4  moderation schema constraints
   --------------------------------------------------------------------------- */

-- Primary key on moderation.reports
ALTER TABLE ONLY moderation.reports
    ADD CONSTRAINT reports_pkey PRIMARY KEY (id);

-- Foreign key on moderation.reports → auth.users
ALTER TABLE ONLY moderation.reports
    ADD CONSTRAINT reports_reporter_id_fkey
        FOREIGN KEY (reporter_id) REFERENCES auth.users(id);

-- Note: moderation.legal_holds PK and FK were declared inline in CREATE TABLE


/* =============================================================================
   SECTION 6 — INDEXES
   Created last to maximise performance during any initial bulk data load.
   Covering indexes, GIN indexes, and composite indexes are annotated with
   their intended query patterns.
   ============================================================================= */

/* ---------------------------------------------------------------------------
   6.1  auth schema indexes
   --------------------------------------------------------------------------- */

-- Supports username lookup during login and @-mention autocomplete
CREATE INDEX idx_users_username ON auth.users USING btree (username);

-- Supports email-based login and registration duplicate check
CREATE INDEX idx_users_email ON auth.users USING btree (email);

-- Supports phone-based login
CREATE INDEX idx_users_phone ON auth.users USING btree (phone);

-- Supports badge lookups and JSONB containment queries
CREATE INDEX idx_users_badges ON auth.users USING gin (badges);

-- Accelerates outgoing relation queries (who does this user follow?)
CREATE INDEX idx_relations_primary_id ON auth.relations USING btree (primary_id);

-- Accelerates incoming relation queries (who follows this user?)
CREATE INDEX idx_relations_secondary_id ON auth.relations USING btree (secondary_id);

-- Accelerates settings lookup by user (most common access pattern)
CREATE INDEX idx_user_settings_user_id ON auth.user_settings USING btree (user_id);

-- Unique index on (user_id, firebase_installation_id) — enforces one session per device
-- and supports the fast-path lookup in func_load_sessions
CREATE UNIQUE INDEX idx_sessions_user_device ON auth.sessions USING btree (user_id, firebase_installation_id);


/* ---------------------------------------------------------------------------
   6.2  content schema indexes
   --------------------------------------------------------------------------- */

-- Composite index for user feed queries (all posts by a user, newest first)
CREATE INDEX idx_posts_user_created ON content.posts USING btree (user_id, created_at DESC);

-- GIN index for hashtag containment queries (WHERE hashtags @> '{travel}')
CREATE INDEX idx_posts_hashtags ON content.posts USING gin (hashtags);

-- GIN index for mention/identifier lookups (WHERE identifiers @> '{42}')
CREATE INDEX idx_posts_identifiers ON content.posts USING gin (identifiers);

-- GIN index for indirect hashtag containment queries
CREATE INDEX idx_posts_indirect_hashtags ON content.posts USING gin (indirect_hashtags);

-- Composite index for comment pagination (all comments on a post, best score, newest first)
CREATE INDEX idx_comments_post_score_created ON content.comments USING btree (post_id, score DESC, created_at ASC);

-- Composite index for polymorphic like lookups (count likes on a post/comment/media)
CREATE INDEX idx_likes_target ON content.likes USING btree (target_type, target_id);

-- Index for "all likes by a user" queries (activity feed, unlike validation)
CREATE INDEX idx_likes_user ON content.likes USING btree (user_id);

-- Composite index for saved-posts pagination (all saved posts by a user, newest first)
CREATE INDEX idx_saved_user_created ON content.saved USING btree (user_id, created_at DESC);

-- Index for "all users who saved this post" queries (post popularity / analytics)
CREATE INDEX idx_saved_post ON content.saved USING btree (post_id);

-- Index for media by owner (user gallery / profile picture resolution)
CREATE INDEX idx_media_owner ON content.media USING btree (owner_id);

-- Index for media by upload date (chronological gallery views)
CREATE INDEX idx_media_created ON content.media USING btree (created_at);


/* ---------------------------------------------------------------------------
   6.3  messaging schema indexes
   --------------------------------------------------------------------------- */

-- Supports inbox ordering by latest message
CREATE INDEX idx_conversations_last_message ON messaging.conversations USING btree (last_message_id);

-- Supports chronological inbox pagination
CREATE INDEX idx_conversations_created ON messaging.conversations USING btree (created_at DESC);

-- Supports filtering archived conversations (state filter in func_load_conversation)
CREATE INDEX idx_conversations_state ON messaging.conversations USING btree (state);

-- Accelerates "which conversations is this user in?" joins
CREATE INDEX idx_members_conversation ON messaging.members USING btree (conversation_id);

-- Accelerates "which conversations does this user belong to?" lookups
CREATE INDEX idx_members_user ON messaging.members USING btree (user_id);

-- Composite index for message chronology within a conversation (primary read pattern)
CREATE INDEX idx_message_conv_created ON messaging.messages USING btree (conversation_id, created_at DESC);

-- Composite index for role-based member queries (e.g., moderation/admin tools)
CREATE INDEX idx_members_conv_role_joined ON messaging.members USING btree (conversation_id, role, joined_at DESC);

-- Index for loading reactions by message
CREATE INDEX idx_message_reactions_message_id ON messaging.message_reactions USING btree (message_id);

-- Index for looking up a specific user's reactions
CREATE INDEX idx_message_reactions_user_id ON messaging.message_reactions USING btree (user_id);


/* ---------------------------------------------------------------------------
   6.4  moderation schema indexes
   --------------------------------------------------------------------------- */

-- Supports "all reports filed by user X" queries (reporter history view)
CREATE INDEX idx_reports_reporter ON moderation.reports USING btree (reporter_id);

-- Supports "all reports concerning target Y" queries (moderation queue filtering by target)
CREATE INDEX idx_reports_targets ON moderation.reports USING gin (target_ids);

-- Supports moderation queue chronological ordering
CREATE INDEX idx_reports_created ON moderation.reports USING btree (created_at);

-- Supports queue filtering by state (pending / actioned / dismissed)
CREATE INDEX idx_reports_state ON moderation.reports USING btree (state);


/* =============================================================================
   END OF SCHEMA
   =============================================================================
   Database:   Social Platform
   Schemas:    auth · content · messaging · moderation · views
   Tables:     14   (auth×4, content×5, messaging×3, moderation×2)
   Functions:  19   (auth×4, content×7, messaging×3, moderation×0, views×6)
   Procedures: 2    (moderation×2)
   Views:      6    (views×6)
   Constraints: PKs×14 + UNIQUEs×9 + FKs×14 + inline×2 = 39 total
   Indexes:    24   (auth×5, content×8, messaging×5, moderation×4) + 1 UNIQUE idx
   ============================================================================= */
