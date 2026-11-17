# SQL Signatures Map (`schema.sql` <-> Go)

This file tracks critical SQL-to-Go scan contracts verified by `skills/schema-contract-validator/scripts/check_sql_scans.py`.

## Verified Core Contracts

| SQL Function (`schema.sql`) | Return View / Type | Columns | Go Repository (`internal/repository/postgres/`) |
| :--- | :--- | :--- | :--- |
| `auth.func_load_user` | `SETOF auth.users_l3` | 29 | `func_load_user.go` |
| `auth.func_load_relations_paginated` | `TABLE` | 4 | `func_load_relations_paginated.go` |
| `content.func_load_timeline_seed_paginated` | `TABLE` | 3 | `func_load_timeline_seed_paginated.go` |
| `messaging.func_load_members_by_role_paginated` | `TABLE` | 10 | `func_load_members_by_role_paginated.go` |
| `messaging.func_load_muted_members_paginated` | `TABLE` | 2 | `func_load_muted_members.go` |

> Run `make check-sql` after any modification to `schema.sql` or a PostgreSQL repository to validate contract alignment.
