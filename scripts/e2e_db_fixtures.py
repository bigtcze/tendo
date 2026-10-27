"""Database fixture setup and persisted-state checks for native E2E runs."""
from __future__ import annotations

import re
import subprocess
import urllib.parse
from collections.abc import Callable


UUID_PATTERN = re.compile(
    r"[0-9a-fA-F]{8}-(?:[0-9a-fA-F]{4}-){3}[0-9a-fA-F]{12}"
)
FOREIGN_SEED_EXPECTED = "Foreign cottage|1|false|Foreign gutter check|1|false|open|null|false|0|1|1|0|0"
EXISTING_ACCOUNT_PASSWORD = "G4-existing-account-test-password"
EXISTING_ACCOUNT_PASSWORD_HASH = (
    "$argon2id$v=19$m=65536,t=3,p=1$wjup57/JzmJBVZuEaGU9ag$"
    "9QaKkmlhckTJyT9jquSV3m0UvJ1TAPkPFN6NpNY+z/Y"
)


def psql_admin(inherited: dict[str, str], sql: str) -> str:
    """Run one bounded admin query without exposing connection credentials in argv."""
    value = inherited.get("TEST_DATABASE_ADMIN_URL", "")
    parsed = urllib.parse.urlsplit(value)
    if (
        parsed.scheme not in ("postgres", "postgresql")
        or not parsed.hostname
        or not parsed.username
        or not parsed.path.strip("/")
        or parsed.fragment
    ):
        raise RuntimeError(
            "TEST_DATABASE_ADMIN_URL must be a postgres URL with host/database and no fragment"
        )
    env = {
        key: val for key, val in inherited.items()
        if not key.startswith("PG")
        and key not in {"TEST_DATABASE_ADMIN_URL", "TEST_DATABASE_URL"}
    }
    env.update({
        "PGHOST": parsed.hostname,
        "PGPORT": str(parsed.port or 5432),
        "PGUSER": urllib.parse.unquote(parsed.username),
        "PGPASSWORD": urllib.parse.unquote(parsed.password or ""),
        "PGDATABASE": urllib.parse.unquote(parsed.path.lstrip("/")),
    })
    allowed = {
        "sslmode", "sslrootcert", "sslcert", "sslkey", "options",
        "application_name", "connect_timeout",
    }
    pairs = urllib.parse.parse_qsl(parsed.query, keep_blank_values=True)
    keys = [key for key, _ in pairs]
    if len(keys) != len(set(keys)) or set(keys) - allowed:
        raise RuntimeError(
            "TEST_DATABASE_ADMIN_URL has duplicate or unsupported query parameters"
        )
    for key, val in pairs:
        env["PG" + key.upper()] = val
    try:
        result = subprocess.run(
            [inherited.get("PSQL", "psql"), "-X", "-At", "-v", "ON_ERROR_STOP=1", "-c", sql],
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            env=env,
            timeout=30,
        )
    except subprocess.TimeoutExpired as exc:
        raise RuntimeError("admin psql query exceeded 30-second deadline") from exc
    if result.returncode:
        raise RuntimeError(
            f"admin psql query failed with exit status {result.returncode}"
        )
    return result.stdout.strip()


def seed_foreign_household(inherited: dict[str, str]) -> tuple[str, str, str]:
    """Insert the foreign household fixture and return its household/subject/item IDs."""
    sql = """WITH h AS (
      INSERT INTO households(name, timezone)
      VALUES ('Foreign household', 'UTC') RETURNING id
    ), s AS (
      INSERT INTO subjects(household_id, type, name)
      SELECT id, 'home', 'Foreign cottage' FROM h
      RETURNING id, household_id
    ), i AS (
      INSERT INTO items(household_id, subject_id, title)
      SELECT household_id, id, 'Foreign gutter check' FROM s
      RETURNING household_id, subject_id, id
    )
    SELECT household_id || '|' || subject_id || '|' || id FROM i;"""
    seed = psql_admin(inherited, sql)
    identifiers = seed.split("|")
    if len(identifiers) != 3 or any(
        not UUID_PATTERN.fullmatch(value) for value in identifiers
    ):
        raise RuntimeError("foreign household seed did not return three UUIDs")
    return identifiers[0], identifiers[1], identifiers[2]


def seed_existing_invitation_accounts(
    inherited: dict[str, str],
    register: Callable[[str], None],
) -> str:
    """Create the free and conflict account fixtures and return conflict household ID."""
    register(EXISTING_ACCOUNT_PASSWORD)
    psql_admin(inherited, f"""WITH account AS (
      INSERT INTO user_accounts(login) VALUES ('existing_free_e2e') RETURNING id
    ) INSERT INTO local_credentials(user_id, password_hash)
      SELECT id, '{EXISTING_ACCOUNT_PASSWORD_HASH}' FROM account;""")
    conflict_seed = psql_admin(inherited, f"""WITH conflict_household AS (
      INSERT INTO households(name, timezone) VALUES ('Conflict household', 'UTC') RETURNING id
    ), conflict_user AS (
      INSERT INTO user_accounts(login, default_household_id)
      SELECT 'existing_conflict_e2e', id FROM conflict_household
      RETURNING id, default_household_id
    ), conflict_membership AS (
      INSERT INTO household_memberships(user_id, household_id, role)
      SELECT id, default_household_id, 'owner' FROM conflict_user
      RETURNING user_id, household_id
    ), conflict_credential AS (
      INSERT INTO local_credentials(user_id, password_hash)
      SELECT id, '{EXISTING_ACCOUNT_PASSWORD_HASH}' FROM conflict_user
    )
    SELECT household_id::text || '|' || user_id::text FROM conflict_membership""")
    identifiers = conflict_seed.split("|")
    if len(identifiers) != 2 or any(
        not UUID_PATTERN.fullmatch(value) for value in identifiers
    ):
        raise RuntimeError(
            "existing-account seed did not return conflict household and user UUIDs"
        )
    return identifiers[0]


def assert_existing_invitation_accounts(
    inherited: dict[str, str], conflict_household_id: str
) -> None:
    if not UUID_PATTERN.fullmatch(conflict_household_id):
        raise RuntimeError("existing-account seed identifiers must be UUIDs")
    actual = psql_admin(inherited, f"""SELECT concat_ws('|',
      (SELECT count(*) FROM household_memberships m
        JOIN user_accounts u ON u.id=m.user_id WHERE u.login='existing_free_e2e'),
      (SELECT count(*) FROM household_memberships m
        JOIN user_accounts u ON u.id=m.user_id
        JOIN user_accounts o ON o.login='owner_e2e'
        WHERE u.login='existing_free_e2e'
          AND m.household_id=o.default_household_id AND m.role='member'),
      (SELECT default_household_id::text FROM user_accounts
        WHERE login='existing_free_e2e'),
      (SELECT default_household_id::text FROM user_accounts WHERE login='owner_e2e'),
      (SELECT count(*) FROM household_memberships m
        JOIN user_accounts u ON u.id=m.user_id
        WHERE u.login='existing_conflict_e2e'),
      (SELECT count(*) FROM household_memberships m
        JOIN user_accounts u ON u.id=m.user_id
        WHERE u.login='existing_conflict_e2e'
          AND m.household_id='{conflict_household_id}' AND m.role='owner'),
      (SELECT default_household_id::text FROM user_accounts
        WHERE login='existing_conflict_e2e'),
      (SELECT count(*) FROM household_memberships
        WHERE household_id='{conflict_household_id}'))""")
    owner_household_id = psql_admin(
        inherited,
        "SELECT default_household_id FROM user_accounts WHERE login='owner_e2e'",
    )
    expected = f"1|1|{owner_household_id}|{owner_household_id}|1|1|{conflict_household_id}|1"
    if actual != expected:
        raise RuntimeError(
            "existing-account invitation state mismatch "
            f"(expected {expected!r}, got {actual!r})"
        )


def assert_foreign_seed_unchanged(
    inherited: dict[str, str], household_id: str, subject_id: str, item_id: str
) -> None:
    identifiers = (household_id, subject_id, item_id)
    if any(not UUID_PATTERN.fullmatch(value) for value in identifiers):
        raise RuntimeError("foreign seed identifiers must be UUIDs")
    h, s, i = household_id, subject_id, item_id
    sql = f"""SELECT concat_ws('|',
      (SELECT name FROM subjects WHERE household_id = '{h}' AND id = '{s}'),
      (SELECT version FROM subjects WHERE household_id = '{h}' AND id = '{s}'),
      (SELECT archived::text FROM subjects WHERE household_id = '{h}' AND id = '{s}'),
      (SELECT title FROM items WHERE household_id = '{h}' AND id = '{i}'),
      (SELECT version FROM items WHERE household_id = '{h}' AND id = '{i}'),
      (SELECT archived::text FROM items WHERE household_id = '{h}' AND id = '{i}'),
      (SELECT workflow_state FROM items WHERE household_id = '{h}' AND id = '{i}'),
      (SELECT coalesce(attention_on::text, 'null') FROM items
        WHERE household_id = '{h}' AND id = '{i}'),
      (SELECT done::text FROM items WHERE household_id = '{h}' AND id = '{i}'),
      (SELECT count(*) FROM item_completions WHERE household_id = '{h}'),
      (SELECT count(*) FROM subjects WHERE household_id = '{h}'),
      (SELECT count(*) FROM items WHERE household_id = '{h}'),
      (SELECT count(*) FROM household_memberships WHERE household_id = '{h}'),
      (SELECT count(*) FROM household_invitations WHERE household_id = '{h}'))"""
    actual = psql_admin(inherited, sql)
    if actual != FOREIGN_SEED_EXPECTED:
        raise RuntimeError(
            "foreign household seed changed during E2E "
            f"(expected {FOREIGN_SEED_EXPECTED!r}, got {actual!r}); "
            "authorization isolation assertion failed"
        )
