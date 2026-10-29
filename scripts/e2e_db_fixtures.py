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


def seed_large_paging_household(
    inherited: dict[str, str], register: Callable[[str], None]
) -> tuple[str, str]:
    """Create an isolated household with 1001 records in each G7 paging collection."""
    register(EXISTING_ACCOUNT_PASSWORD)
    login = "large_paging_owner_e2e"
    seed = psql_admin(inherited, f"""WITH h AS (
      INSERT INTO households(name, timezone) VALUES ('Large paging household', 'UTC') RETURNING id
    ), owner AS (
      INSERT INTO user_accounts(login, default_household_id)
      SELECT '{login}', id FROM h RETURNING id, default_household_id
    ), membership AS (
      INSERT INTO household_memberships(user_id, household_id, role)
      SELECT id, default_household_id, 'owner' FROM owner RETURNING user_id, household_id
    ), credential AS (
      INSERT INTO local_credentials(user_id, password_hash)
      SELECT id, '{EXISTING_ACCOUNT_PASSWORD_HASH}' FROM owner
    ), members AS (
      INSERT INTO user_accounts(login)
      SELECT 'large_paging_member_' || lpad(n::text, 4, '0') FROM generate_series(1, 1001) n
      RETURNING id, login
    ), member_rows AS (
      INSERT INTO household_memberships(user_id, household_id, role)
      SELECT members.id, membership.household_id, 'member'
      FROM members CROSS JOIN membership
      RETURNING user_id
    ), subjects AS (
      INSERT INTO subjects(household_id, type, name)
      SELECT membership.household_id, 'person', 'Large paging person ' || lpad(n::text, 4, '0')
      FROM generate_series(1, 1001) n CROSS JOIN membership
      RETURNING id
    ), invitations AS (
      INSERT INTO household_invitations(
        household_id, created_by_user_id, creation_key, token_hash, created_at, expires_at
      )
      SELECT membership.household_id, membership.user_id, 'large-paging-' || n,
        decode(md5('large-paging-token-' || n) || md5('large-paging-token-check-' || n), 'hex'),
        now(), now() + interval '7 days'
      FROM generate_series(1, 1001) n CROSS JOIN membership
      RETURNING id
    ), materialized AS (
      SELECT
        (SELECT count(*) FROM member_rows) AS member_count,
        (SELECT count(*) FROM subjects) AS subject_count,
        (SELECT count(*) FROM invitations) AS invitation_count
    )
    SELECT membership.household_id::text || '|' || membership.user_id::text || '|'
      || materialized.member_count::text || '|' || materialized.subject_count::text || '|'
      || materialized.invitation_count::text
    FROM membership CROSS JOIN materialized""")
    identifiers = seed.split("|")
    if (
        len(identifiers) != 5
        or any(not UUID_PATTERN.fullmatch(value) for value in identifiers[:2])
        or identifiers[2:] != ["1001", "1001", "1001"]
    ):
        raise RuntimeError("large paging seed did not return the household, owner and 1001-row fixture counts")
    return identifiers[0], login


def assert_large_paging_household(
    inherited: dict[str, str], household_id: str, owner_login: str
) -> None:
    """Verify fixture isolation and exact record counts after the browser walk."""
    if not UUID_PATTERN.fullmatch(household_id) or owner_login != "large_paging_owner_e2e":
        raise RuntimeError("large paging fixture identifiers are invalid")
    actual = psql_admin(inherited, f"""SELECT concat_ws('|',
      (SELECT count(*) FROM household_memberships WHERE household_id='{household_id}'),
      (SELECT count(*) FROM household_memberships m JOIN user_accounts u ON u.id=m.user_id
        WHERE m.household_id='{household_id}' AND u.login LIKE 'large_paging_member_%'),
      (SELECT count(*) FROM subjects WHERE household_id='{household_id}'),
      (SELECT count(*) FROM household_invitations WHERE household_id='{household_id}'),
      (SELECT count(*) FROM household_invitations WHERE household_id='{household_id}' AND revoked_at IS NULL AND accepted_at IS NULL),
      (SELECT count(*) FROM household_invitations WHERE household_id='{household_id}' AND token_hash IS NULL)
    )""")
    if actual != "1002|1001|1001|1001|1001|0":
        raise RuntimeError(f"large paging fixture state mismatch: {actual!r}")


def cleanup_large_paging_household(inherited: dict[str, str], household_id: str) -> None:
    """Remove the isolated fixture household and its generated accounts after verification."""
    if not UUID_PATTERN.fullmatch(household_id):
        raise RuntimeError("large paging fixture household ID must be a UUID")
    # Bare DML would also print psql's command tag, so each statement reports its row count via a CTE.
    owner = psql_admin(inherited, """WITH changed AS (UPDATE user_accounts SET default_household_id=NULL
      WHERE login='large_paging_owner_e2e' RETURNING id) SELECT count(*) FROM changed""")
    households = psql_admin(inherited, f"""WITH removed AS (DELETE FROM households
      WHERE id='{household_id}' RETURNING id) SELECT count(*) FROM removed""")
    accounts = psql_admin(inherited, """WITH removed AS (DELETE FROM user_accounts WHERE login='large_paging_owner_e2e'
      OR login LIKE 'large_paging_member_%' RETURNING id) SELECT count(*) FROM removed""")
    if (owner, households, accounts) != ("1", "1", "1002"):
        raise RuntimeError(f"large paging fixture cleanup mismatch: owner={owner} households={households} accounts={accounts}")


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


def seed_suite_fixtures(
    inherited: dict[str, str], register: Callable[[str], None]
) -> dict[str, str]:
    """Seed every runner-owned database fixture and return the Playwright environment that names it."""
    foreign_household_id, foreign_subject_id, foreign_item_id = seed_foreign_household(inherited)
    conflict_household_id = seed_existing_invitation_accounts(inherited, register)
    large_household_id, large_owner_login = seed_large_paging_household(inherited, register)
    register(large_owner_login)
    return {
        "E2E_FOREIGN_HOUSEHOLD_ID": foreign_household_id,
        "E2E_FOREIGN_SUBJECT_ID": foreign_subject_id,
        "E2E_FOREIGN_ITEM_ID": foreign_item_id,
        "E2E_EXISTING_PASSWORD": EXISTING_ACCOUNT_PASSWORD,
        "E2E_CONFLICT_HOUSEHOLD_ID": conflict_household_id,
        "E2E_LARGE_HOUSEHOLD_ID": large_household_id,
        "E2E_LARGE_OWNER_LOGIN": large_owner_login,
    }


def verify_suite_fixtures(inherited: dict[str, str], fixture_env: dict[str, str]) -> None:
    """Assert persisted fixture state after the suite; the large household is removed even when its check fails."""
    assert_foreign_seed_unchanged(
        inherited, fixture_env["E2E_FOREIGN_HOUSEHOLD_ID"],
        fixture_env["E2E_FOREIGN_SUBJECT_ID"], fixture_env["E2E_FOREIGN_ITEM_ID"],
    )
    assert_existing_invitation_accounts(inherited, fixture_env["E2E_CONFLICT_HOUSEHOLD_ID"])
    try:
        assert_large_paging_household(
            inherited, fixture_env["E2E_LARGE_HOUSEHOLD_ID"], fixture_env["E2E_LARGE_OWNER_LOGIN"]
        )
    finally:
        cleanup_large_paging_household(inherited, fixture_env["E2E_LARGE_HOUSEHOLD_ID"])
