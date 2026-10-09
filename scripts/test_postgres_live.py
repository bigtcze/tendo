#!/usr/bin/env python3
"""Explicit real-PostgreSQL provisioning ownership tests; requires PG18 test service env."""
from __future__ import annotations

import concurrent.futures
import importlib.util
import os
from pathlib import Path
import secrets
import unittest
from unittest import mock

MODULE = Path(__file__).with_name("postgres-test-service.py")
spec = importlib.util.spec_from_file_location("postgres_test_service_live", MODULE)
service_module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(service_module)


class LivePostgresProvisioningTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.url = os.environ.get("TENDO_TEST_POSTGRES_ADMIN_URL")
        if not cls.url:
            raise RuntimeError("TENDO_TEST_POSTGRES_ADMIN_URL is required for live PostgreSQL harness tests")
        cls.probe = service_module.Service(cls.url)

    def test_concurrent_allocations_cleanup_independently(self):
        roles_before = set(filter(None, self.probe.run("SELECT rolname FROM pg_roles WHERE rolname LIKE 'tendo_m_%' OR rolname LIKE 'tendo_r_%'", tuples=True).splitlines()))
        dbs_before = set(filter(None, self.probe.run("SELECT datname FROM pg_database WHERE datname LIKE 'tendo_db_%'", tuples=True).splitlines()))
        services = [service_module.Service(self.url), service_module.Service(self.url)]
        executor = concurrent.futures.ThreadPoolExecutor(max_workers=2)
        futures = [executor.submit(item.provision) for item in services]
        primary_error = None
        try:
            for future in futures:
                future.result(timeout=30)
            first, second = services
            self.assertNotEqual(first.database, second.database)
            self.assertNotEqual(first.migrator, second.migrator)
            self.assertEqual(first.run("SELECT current_database()", database=first.database, tuples=True), first.database)
            self.assertEqual(second.run("SELECT current_database()", database=second.database, tuples=True), second.database)
            first.cleanup()
            self.assertEqual(second.run("SELECT current_database()", database=second.database, tuples=True), second.database)
        except BaseException as exc:
            primary_error = exc
            raise
        finally:
            executor.shutdown(wait=True, cancel_futures=True)
            cleanup_errors = []
            for item in services:
                try:
                    item.cleanup()
                except BaseException as exc:
                    cleanup_errors.append(exc)
            roles_after = set(filter(None, self.probe.run("SELECT rolname FROM pg_roles WHERE rolname LIKE 'tendo_m_%' OR rolname LIKE 'tendo_r_%'", tuples=True).splitlines()))
            dbs_after = set(filter(None, self.probe.run("SELECT datname FROM pg_database WHERE datname LIKE 'tendo_db_%'", tuples=True).splitlines()))
            if primary_error is None and cleanup_errors:
                raise cleanup_errors[0]
            if primary_error is None:
                self.assertEqual(roles_after, roles_before, "concurrent allocation leaked roles")
                self.assertEqual(dbs_after, dbs_before, "concurrent allocation leaked databases")

    def test_preexisting_database_collision_is_never_adopted(self):
        suffix = secrets.token_hex(10)
        db_name = f"tendo_db_{suffix}"
        existing_oid = None
        primary_error = None
        roles_query = "SELECT rolname FROM pg_roles WHERE rolname LIKE 'tendo_m_%' OR rolname LIKE 'tendo_r_%'"
        roles_before = set(filter(None, self.probe.run(roles_query, tuples=True).splitlines()))
        allocated = service_module.Service(self.url)
        self.probe.run(f"CREATE DATABASE {self.probe.ident(db_name)} TEMPLATE template0")
        try:
            existing_oid = int(self.probe.run(f"SELECT oid FROM pg_database WHERE datname={self.probe.literal(db_name)}", tuples=True))
            with mock.patch.object(service_module.secrets, "token_hex", return_value=suffix):
                with self.assertRaisesRegex(RuntimeError, r"SQLSTATE 42P04: .*already exists"):
                    allocated.provision()
            self.assertNotIn(db_name, allocated.created_dbs)
            # A real server-side collision error is definite, not ambiguous: the
            # allocation's own roles must be cleaned without adopting the database.
            self.assertFalse(allocated.database_creation_uncertain)
            allocated.cleanup()
            self.assertEqual(int(self.probe.run(f"SELECT oid FROM pg_database WHERE datname={self.probe.literal(db_name)}", tuples=True)), existing_oid)
            roles_after = set(filter(None, self.probe.run(roles_query, tuples=True).splitlines()))
            self.assertEqual(roles_after, roles_before, "collision allocation leaked roles")
        except BaseException as exc:
            primary_error = exc
            raise
        finally:
            cleanup_errors = []
            try:
                allocated.cleanup()
            except BaseException as exc:
                cleanup_errors.append(exc)
            try:
                row = self.probe.run(f"SELECT oid FROM pg_database WHERE datname={self.probe.literal(db_name)}", tuples=True)
                if row and existing_oid is not None:
                    if int(row) != existing_oid:
                        raise AssertionError("collision sentinel OID changed; refusing cleanup")
                    self.probe.run(f"DROP DATABASE {self.probe.ident(db_name)} WITH (FORCE)")
            except BaseException as exc:
                cleanup_errors.append(exc)
            if primary_error is None and cleanup_errors:
                raise cleanup_errors[0]

    def test_inherited_builtin_role_is_rejected_before_provisioning(self):
        suffix = secrets.token_hex(10)
        candidate_name = f"tendo_candidate_{suffix}"
        candidate_oid = None
        self.probe.run(f"CREATE ROLE {self.probe.ident(candidate_name)} NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS")
        try:
            candidate_oid = int(self.probe.run(f"SELECT oid FROM pg_roles WHERE rolname={self.probe.literal(candidate_name)}", tuples=True))
            self.probe.run(f"GRANT pg_read_all_data TO {self.probe.ident(candidate_name)}")
            validator = service_module.Service(self.url)
            with self.assertRaisesRegex(RuntimeError, "inherits other roles"):
                validator.validate_runtime_role(candidate_name)
        finally:
            row = self.probe.run(f"SELECT oid FROM pg_roles WHERE rolname={self.probe.literal(candidate_name)}", tuples=True)
            if row:
                if candidate_oid is None or int(row) != candidate_oid:
                    raise AssertionError("candidate role OID changed; refusing cleanup")
                self.probe.run(f"REVOKE pg_read_all_data FROM {self.probe.ident(candidate_name)}")
                self.probe.run(f"DROP ROLE {self.probe.ident(candidate_name)}")

    def test_fixed_upgrade_name_sentinel_survives_service_cleanup(self):
        sentinel = "tendo_upgrade_v3_subjects_test"
        self.probe.run(f"CREATE DATABASE {self.probe.ident(sentinel)} TEMPLATE template0")
        oid = int(self.probe.run(f"SELECT oid FROM pg_database WHERE datname={self.probe.literal(sentinel)}", tuples=True))
        owner_oid = int(self.probe.run(f"SELECT datdba FROM pg_database WHERE oid={oid}", tuples=True))
        candidate = service_module.Service(self.url)
        primary_error = None
        try:
            candidate.provision()
            candidate.cleanup()
            current = self.probe.run(f"SELECT oid || '|' || datdba FROM pg_database WHERE datname={self.probe.literal(sentinel)}", tuples=True)
            self.assertEqual(tuple(map(int, current.split("|"))), (oid, owner_oid))
        except BaseException as exc:
            primary_error = exc
            raise
        finally:
            cleanup_errors = []
            try:
                candidate.cleanup()
            except BaseException as exc:
                cleanup_errors.append(exc)
            try:
                current = self.probe.run(f"SELECT oid || '|' || datdba FROM pg_database WHERE datname={self.probe.literal(sentinel)}", tuples=True)
                if current:
                    current_oid, current_owner = map(int, current.split("|"))
                    if current_oid != oid or current_owner != owner_oid:
                        raise AssertionError("upgrade sentinel ownership changed; refusing cleanup")
                    self.probe.run(f"DROP DATABASE {self.probe.ident(sentinel)} WITH (FORCE)")
            except BaseException as exc:
                cleanup_errors.append(exc)
            if primary_error is None and cleanup_errors:
                raise cleanup_errors[0]


if __name__ == "__main__":
    unittest.main(verbosity=2)
