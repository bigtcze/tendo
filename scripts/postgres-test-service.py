#!/usr/bin/env python3
"""Provision and clean isolated PostgreSQL integration-test databases and roles."""
from __future__ import annotations

import contextlib
import os
import signal
import subprocess
import threading
import time
import secrets
import sys
import urllib.parse


def parse_url(value: str) -> dict[str, str]:
    parsed = urllib.parse.urlsplit(value)
    if parsed.scheme not in ("postgres", "postgresql") or not parsed.hostname or not parsed.username or not parsed.path.strip("/") or parsed.fragment:
        raise ValueError("TENDO_TEST_POSTGRES_ADMIN_URL must be a postgres URL with host/database and no fragment")
    pairs = urllib.parse.parse_qsl(parsed.query, keep_blank_values=True)
    keys = [key for key, _ in pairs]
    allowed = {"sslmode", "sslrootcert", "sslcert", "sslkey", "options", "application_name", "connect_timeout"}
    if len(keys) != len(set(keys)) or set(keys) - allowed:
        raise ValueError("admin URL has duplicate or unsupported query parameters")
    host = f"[{parsed.hostname}]" if ":" in parsed.hostname else parsed.hostname
    result = {"host": parsed.hostname, "url_host": host, "port": str(parsed.port or 5432), "dbname": urllib.parse.unquote(parsed.path.lstrip("/"))}
    if parsed.username:
        result["user"] = urllib.parse.unquote(parsed.username)
    if parsed.password:
        result["password"] = urllib.parse.unquote(parsed.password)
    for key, value in pairs:
        result[key] = value
    result.setdefault("connect_timeout", "5")
    return result


class Service:
    def __init__(self, url: str):
        self.conn = parse_url(url)
        self.psql = os.environ.get("PSQL", "psql")
        self.created_roles: dict[str, int] = {}
        self.created_dbs: dict[str, int] = {}
        self.provision_attempted = False
        self.role_transaction_uncertain = False
        self.database_creation_uncertain = False
        self.cleaning = False
        self.cancelled = threading.Event()
        self.child_env: dict[str, str] = {}
        self.migrator = ""
        self.runtime = ""
        self.database = ""

    def run(self, sql: str, *, database: str | None = None, tuples: bool = False, allow_cancelled: bool = False) -> str:
        env = {key: value for key, value in os.environ.items() if not key.startswith("PG") and key not in {"TENDO_TEST_POSTGRES_ADMIN_URL", "TEST_DATABASE_ADMIN_URL", "TEST_DATABASE_URL"}}
        env.update({"PATH": os.environ.get("PATH", ""), "PGHOST": self.conn["host"], "PGPORT": self.conn["port"], "PGDATABASE": database or self.conn["dbname"]})
        for key in ("user", "password", "sslmode", "sslrootcert", "sslcert", "sslkey", "options", "application_name", "connect_timeout"):
            env_key = {"user": "PGUSER", "password": "PGPASSWORD"}.get(key, "PG" + key.upper())
            env[env_key] = self.conn.get(key, "")
        args = [self.psql, "-X", "-v", "ON_ERROR_STOP=1", "-v", "VERBOSITY=verbose", "-At" if tuples else "-q"]
        if self.cancelled.is_set() and not self.cleaning and not allow_cancelled:
            raise RuntimeError("PostgreSQL test setup cancelled before SQL execution")
        try:
            proc = subprocess.run(args, input=sql, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=env, timeout=10)
        except subprocess.TimeoutExpired as exc:
            raise RuntimeError("psql command exceeded 10-second deadline") from exc
        if proc.returncode:
            detail = proc.stderr.strip()
            for secret in (self.conn.get("password", ""),):
                if secret:
                    detail = detail.replace(secret, "[redacted]")
            import re
            detail = re.sub(r"(?i)(PASSWORD\s+)'(?:''|[^'])*'", r"\1'[redacted]'", detail)
            # VERBOSITY=verbose prints "ERROR:  42P04: ..."; surface the SQLSTATE so
            # callers can distinguish a definite server-side statement error from an
            # ambiguous outcome. FATAL/connection failures stay unclassified.
            sqlstate = re.search(r"(?m)^ERROR:\s+([0-9A-Z]{5}):", detail)
            prefix = f"SQLSTATE {sqlstate.group(1)}: " if sqlstate else ""
            raise RuntimeError(f"psql failed ({proc.returncode}): {prefix}{detail or 'no diagnostic output'}")
        return proc.stdout.strip()

    @staticmethod
    def ident(value: str) -> str:
        return '"' + value.replace('"', '""') + '"'

    def validate_runtime_role(self, role_name: str) -> None:
        literal = self.literal(role_name)
        role = self.run(f"SELECT oid, rolcanlogin, rolsuper, rolcreatedb, rolcreaterole, rolbypassrls, rolreplication FROM pg_roles WHERE rolname={literal}", tuples=True)
        fields = role.split("|") if role else []
        if len(fields) != 7:
            raise RuntimeError(f"pre-existing NOLOGIN role {role_name} is required")
        if fields[1:] != ["f", "f", "f", "f", "f", "f"]:
            raise RuntimeError(f"role {role_name} must be NOLOGIN and have no elevated role attributes")
        inherited = self.run(f"WITH RECURSIVE memberships(roleid) AS (SELECT oid FROM pg_roles WHERE rolname={literal} UNION SELECT m.roleid FROM pg_auth_members m JOIN memberships x ON m.member=x.roleid) SELECT string_agg(r.rolname, ',') FROM memberships x JOIN pg_roles r ON r.oid=x.roleid WHERE r.oid<>(SELECT oid FROM pg_roles WHERE rolname={literal})", tuples=True)
        if inherited:
            raise RuntimeError(f"role {role_name} inherits other roles: {inherited}")

    def provision(self) -> None:
        if self.cancelled.is_set():
            raise RuntimeError("PostgreSQL test setup cancelled before provisioning")
        if self.provision_attempted:
            raise RuntimeError("service provisioning may run only once")
        self.provision_attempted = True
        if self.conn["dbname"] != "postgres":
            raise RuntimeError("admin URL must connect to the dedicated test service's postgres maintenance database, not an application database")
        version = self.run("SHOW server_version_num", tuples=True)
        if int(version) // 10000 != 18:
            raise RuntimeError(f"dedicated PostgreSQL 18 test service required; server_version_num={version}")
        self.validate_runtime_role("tendo")
        suffix = secrets.token_hex(10)
        self.migrator, self.runtime, self.database = f"tendo_m_{suffix}", f"tendo_r_{suffix}", f"tendo_db_{suffix}"
        mpw, rpw = secrets.token_urlsafe(32), secrets.token_urlsafe(32)
        self.role_transaction_uncertain = True
        try:
            created = self.run(
                f"BEGIN; CREATE ROLE {self.ident(self.migrator)} LOGIN NOSUPERUSER NOCREATEROLE NOBYPASSRLS CREATEDB PASSWORD {self.literal(mpw)}; "
                f"SELECT oid FROM pg_roles WHERE rolname={self.literal(self.migrator)}; "
                f"CREATE ROLE {self.ident(self.runtime)} LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS INHERIT PASSWORD {self.literal(rpw)}; "
                f"SELECT oid FROM pg_roles WHERE rolname={self.literal(self.runtime)}; "
                f"GRANT tendo TO {self.ident(self.runtime)}; COMMIT;", tuples=True)
        except BaseException as exc:
            if not isinstance(exc, subprocess.TimeoutExpired) and "SQLSTATE" in str(exc):
                self.role_transaction_uncertain = False
            raise
        import re
        role_oids = [int(value) for value in created.splitlines() if re.fullmatch(r"[0-9]+", value)]
        if len(role_oids) != 2:
            raise RuntimeError("role creation transaction returned incomplete ownership identities; no cleanup adoption attempted")
        self.created_roles[self.migrator], self.created_roles[self.runtime] = role_oids
        self.role_transaction_uncertain = False
        self.database_creation_uncertain = True
        try:
            self.run(f"CREATE DATABASE {self.ident(self.database)} OWNER {self.ident(self.migrator)} TEMPLATE template0")
        except BaseException as exc:
            if "SQLSTATE" in str(exc):
                self.database_creation_uncertain = False
            raise
        db_oid = self.run(f"SELECT oid FROM pg_database WHERE datname={self.literal(self.database)}", tuples=True, allow_cancelled=True)
        if not db_oid:
            raise RuntimeError("created database identity lookup returned no OID; cleanup will not adopt an ambiguous database")
        self.created_dbs[self.database] = int(db_oid)
        self.database_creation_uncertain = False
        if self.cancelled.is_set():
            raise RuntimeError("PostgreSQL test setup cancelled after recording database ownership")
        self.run("REVOKE CREATE ON SCHEMA public FROM PUBLIC; GRANT USAGE ON SCHEMA public TO tendo", database=self.database)
        self.child_env = {
            "TEST_DATABASE_ADMIN_URL": self.url(self.migrator, mpw, self.database),
            "TEST_DATABASE_URL": self.url(self.runtime, rpw, self.database),
            "TEST_DATABASE_RUNTIME_ROLE": self.runtime,
            "TEST_DATABASE_MIGRATOR_ROLE": self.migrator,
        }

    @staticmethod
    def literal(value: str) -> str:
        return "'" + value.replace("'", "''") + "'"

    def url(self, user: str, password: str, database: str) -> str:
        query = {k: v for k, v in self.conn.items() if k in {"sslmode", "sslrootcert", "sslcert", "sslkey", "options", "application_name", "connect_timeout"}}
        query["application_name"] = "tendo-integration-tests"
        return urllib.parse.urlunsplit(("postgresql", f"{urllib.parse.quote(user)}:{urllib.parse.quote(password)}@{self.conn['url_host']}:{self.conn['port']}", "/" + urllib.parse.quote(database), urllib.parse.urlencode(query), ""))

    def cleanup(self) -> None:
        self.cleaning = True
        errors = []
        owned_dbs = dict(self.created_dbs)
        if self.role_transaction_uncertain:
            errors.append("role transaction completion is uncertain; refusing to adopt unverified matching role names")
        if self.database_creation_uncertain and self.database in self.created_dbs:
            self.database_creation_uncertain = False
        if self.database_creation_uncertain:
            errors.append("database creation completion is uncertain; refusing to adopt unverified matching database name")
        if self.migrator in self.created_roles:
            try:
                role_row = self.run(f"SELECT oid FROM pg_roles WHERE rolname={self.literal(self.migrator)}", tuples=True)
                role_oid = self.created_roles[self.migrator]
                if role_row and int(role_row) != role_oid:
                    raise RuntimeError(f"refusing upgrade fixture cleanup: migrator role OID changed for {self.migrator}")
                if role_row:
                    rows = self.run(f"SELECT datname || '|' || oid::text FROM pg_database WHERE datdba={role_oid} AND left(datname, 14)='tendo_upgrade_'", tuples=True)
                    for row in filter(None, rows.splitlines()):
                        name, oid = row.rsplit("|", 1)
                        owned_dbs.setdefault(name, int(oid))
            except Exception as exc:
                errors.append(f"enumerate migrator-owned upgrade fixtures: {exc}")
        for name, oid in owned_dbs.items():
            try:
                current = self.run(f"SELECT oid || '|' || datdba FROM pg_database WHERE datname={self.literal(name)}", tuples=True)
                if current:
                    current_oid, owner_oid = (int(part) for part in current.split("|", 1))
                    expected_owner = self.created_roles.get(self.migrator)
                    if current_oid != oid:
                        raise RuntimeError(f"refusing to drop database {name}: catalog OID changed")
                    if expected_owner is None or owner_oid != expected_owner:
                        raise RuntimeError(f"refusing to drop database {name}: owner OID changed")
                    self.run(f"DROP DATABASE {self.ident(name)} WITH (FORCE)")
            except Exception as exc:
                errors.append(str(exc))
        for name, oid in list(self.created_roles.items()):
            try:
                current = self.run(f"SELECT oid FROM pg_roles WHERE rolname={self.literal(name)}", tuples=True)
                if current:
                    if int(current) != oid:
                        raise RuntimeError(f"refusing to drop role {name}: catalog OID changed")
                    if name == self.runtime:
                        self.run(f"REVOKE tendo FROM {self.ident(name)}")
                    self.run(f"DROP ROLE {self.ident(name)}")
            except Exception as exc:
                errors.append(str(exc))
        if errors:
            raise RuntimeError("PostgreSQL test cleanup failed: " + "; ".join(errors))


@contextlib.contextmanager
def postgres_test_service(url: str | None = None):
    service = Service(url or os.environ.get("TENDO_TEST_POSTGRES_ADMIN_URL", ""))
    if not service.conn:
        raise RuntimeError("TENDO_TEST_POSTGRES_ADMIN_URL is required")
    primary_error = None
    try:
        service.provision()
        yield service
    except BaseException as exc:
        primary_error = exc
        raise
    finally:
        try:
            service.cleanup()
        except BaseException as cleanup_error:
            if primary_error is None:
                raise
            print(f"secondary PostgreSQL cleanup failure: {cleanup_error}", file=sys.stderr)


def main() -> int:
    try:
        service = Service(os.environ.get("TENDO_TEST_POSTGRES_ADMIN_URL", ""))
        if not service.conn:
            raise RuntimeError("TENDO_TEST_POSTGRES_ADMIN_URL is required")
        result_code = 1
        child = None
        old_handlers = {}
        signal_seen = None
        def forward_signal(signum, _frame):
            nonlocal signal_seen
            signal_seen = signum
            service.cancelled.set()
            if child is not None and child.poll() is None:
                try:
                    os.killpg(child.pid, signal.SIGTERM)
                except ProcessLookupError:
                    pass
        try:
            for sig in (signal.SIGINT, signal.SIGTERM):
                old_handlers[sig] = signal.signal(sig, forward_signal)
            service.provision()
            if service.cancelled.is_set():
                raise RuntimeError("cancelled before integration test launch")
            command = sys.argv[1:]
            if not command:
                raise RuntimeError("usage: postgres-test-service.py COMMAND [ARG ...]")
            child_env = {key: value for key, value in os.environ.items() if not key.startswith("TEST_DATABASE_") and key != "TENDO_TEST_POSTGRES_ADMIN_URL"}
            child_env.update(service.child_env)
            child = subprocess.Popen(command, env=child_env, start_new_session=True)
            if service.cancelled.is_set():
                os.killpg(child.pid, signal.SIGTERM)
            try:
                while True:
                    try:
                        result_code = child.wait(timeout=0.2)
                        break
                    except subprocess.TimeoutExpired:
                        if signal_seen is not None and child.poll() is None:
                            try:
                                os.killpg(child.pid, signal.SIGTERM)
                            except ProcessLookupError:
                                pass
                            try:
                                result_code = child.wait(timeout=25)
                                break
                            except subprocess.TimeoutExpired:
                                try:
                                    os.killpg(child.pid, signal.SIGKILL)
                                except ProcessLookupError:
                                    pass
                                result_code = child.wait()
                                break
            except BaseException:
                if child.poll() is None:
                    os.killpg(child.pid, signal.SIGTERM)
                    try:
                        child.wait(timeout=25)
                    except subprocess.TimeoutExpired:
                        os.killpg(child.pid, signal.SIGKILL)
                        child.wait()
                raise
            if result_code < 0:
                result_code = 128 + -result_code
            if signal_seen is not None:
                result_code = 128 + signal_seen
        except BaseException as exc:
            if signal_seen is None:
                print(f"PostgreSQL integration setup failed: {exc}", file=sys.stderr)
            result_code = 128 + signal_seen if signal_seen is not None else 1
        try:
            service.cleanup()
        except BaseException as exc:
            print(f"{exc}", file=sys.stderr)
            if result_code == 0:
                result_code = 1
        finally:
            for sig, handler in old_handlers.items():
                signal.signal(sig, handler)
        return result_code
    except BaseException as exc:
        print(f"PostgreSQL integration setup failed: {exc}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
