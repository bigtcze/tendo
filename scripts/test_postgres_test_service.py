import importlib.util
import pathlib
import unittest
from unittest import mock

MODULE = pathlib.Path(__file__).with_name("postgres-test-service.py")
spec = importlib.util.spec_from_file_location("postgres_test_service", MODULE)
service = importlib.util.module_from_spec(spec)
spec.loader.exec_module(service)


class ConnectionURLTests(unittest.TestCase):
    def test_psql_errors_include_sqlstate_and_redact_admin_and_role_passwords(self):
        instance = service.Service("postgresql://admin:admin-secret@localhost/postgres")
        with mock.patch.object(service.subprocess, "run") as run:
            run.return_value.returncode = 3
            run.return_value.stdout = ""
            run.return_value.stderr = "ERROR:  42710: role already exists\\nDETAIL: password 'generated-secret'"
            with self.assertRaises(RuntimeError) as raised:
                instance.run("CREATE ROLE x")
        message = str(raised.exception)
        self.assertIn("42710", message)
        self.assertNotIn("admin-secret", message)
        self.assertNotIn("generated-secret", message)

    def test_structurally_parses_escaped_credentials_preserves_tls_and_ipv6(self):
        parsed = service.parse_url("postgresql://test%40user:p%40ss%2Fword@[::1]:5544/postgres?sslmode=verify-full&sslrootcert=%2Ftmp%2Fca.pem")
        self.assertEqual(parsed["user"], "test@user")
        self.assertEqual(parsed["password"], "p@ss/word")
        self.assertEqual(parsed["dbname"], "postgres")
        self.assertEqual(parsed["sslmode"], "verify-full")
        self.assertEqual(parsed["url_host"], "[::1]")
        self.assertEqual(parsed["connect_timeout"], "5")
        service_instance = service.Service("postgresql://admin:secret@localhost/postgres")
        with mock.patch.object(service.subprocess, "run") as run:
            run.return_value.returncode = 0
            run.return_value.stdout = "ok"
            run.return_value.stderr = ""
            service_instance.run("SELECT 1")
            env = run.call_args.kwargs["env"]
            self.assertEqual(env["PGPASSWORD"], "secret")
            self.assertNotIn("PGSERVICE", env)
            self.assertEqual(env["PGCONNECT_TIMEOUT"], "5")
            self.assertNotIn("TENDO_TEST_POSTGRES_ADMIN_URL", env)

    def test_rejects_non_postgres_incomplete_fragment_duplicate_and_unknown_query(self):
        for value in (
            "http://db.example/postgres", "postgresql:///postgres", "postgres://db.example/",
            "postgres://db.example/postgres#fragment", "postgres://db.example/postgres?sslmode=require&sslmode=disable",
            "postgres://db.example/postgres?connect_timeout=0&application_name=a&application_name=b",
            "postgres://db.example/postgres?hostaddr=127.0.0.1",
        ):
            with self.subTest(value=value), self.assertRaises(ValueError):
                service.parse_url(value)


class FakeService(service.Service):
    def __init__(self, *, version="180006", role="1|f|f|f|f|f|f", inherited="", fail_on=""):
        super().__init__("postgresql://admin:secret@localhost/postgres")
        self.version = version
        self.role = role
        self.inherited = inherited
        self.fail_on = fail_on
        self.statements = []
        self.roles = {}
        self.databases = {}
        self.next_oid = 100

    def run(self, sql, *, database=None, tuples=False, allow_cancelled=False):
        self.statements.append((sql, database, tuples))
        if "SHOW server_version_num" in sql:
            return self.version
        if "string_agg(r.rolname" in sql:
            return self.inherited
        if "FROM pg_roles WHERE rolname='tendo'" in sql or "FROM pg_roles WHERE rolname='candidate'" in sql:
            return self.role
        if "SELECT oid || '|' || rolcanlogin" in sql:
            name = sql.split("rolname='")[1].split("'")[0]
            if name not in self.roles:
                return ""
            return f"{self.roles[name]}|t|f|{'t' if name == self.migrator else 'f'}|f|f|f"
        if sql.startswith("BEGIN; CREATE ROLE"):
            import re
            names = re.findall(r'CREATE ROLE "([^"]+)"', sql)
            if any(name in self.roles for name in names):
                raise RuntimeError("role collision")
            if self.fail_on == "runtime-role":
                raise RuntimeError("SQLSTATE 42710: injected runtime-role failure; role transaction rolled back")
            oids = []
            for name in names:
                self.next_oid += 1
                self.roles[name] = self.next_oid
                oids.append(str(self.next_oid))
            return "\n".join(oids)
        if sql.startswith("CREATE DATABASE"):
            name = sql.split('"')[1]
            if name in self.databases:
                raise RuntimeError("SQLSTATE 42P04: database collision")
            self.next_oid += 1
            self.databases[name] = self.next_oid
            return ""
        if "SELECT oid FROM pg_roles WHERE rolname=" in sql:
            name = sql.split("rolname='")[1].split("'")[0]
            return str(self.roles[name]) if name in self.roles else ""
        if "left(datname" in sql:
            return "\n".join(f"{name}|{oid}" for name, oid in self.databases.items() if name.startswith("tendo_upgrade_"))
        if "SELECT datname || '|' || oid::text" in sql:
            return "\n".join(f"{name}|{oid}" for name, oid in self.databases.items() if name.startswith("tendo_upgrade_"))
        if "SELECT oid || '|' || datdba FROM pg_database WHERE datname=" in sql:
            name = sql.split("datname='")[1].split("'")[0]
            if name not in self.databases:
                return ""
            if "WHERE datname" in sql and "left(datname" not in sql:
                return f"{self.databases[name]}|{self.roles[self.migrator]}"
            return f"{self.databases[name]}|{self.roles[self.migrator]}"
        if "SELECT oid FROM pg_database WHERE datname=" in sql:
            name = sql.split("datname='")[1].split("'")[0]
            if "datdba=" in sql:
                return str(self.databases[name]) if name in self.databases else ""
            return str(self.databases[name]) if name in self.databases else ""
        if sql.startswith("DROP DATABASE"):
            self.databases.pop(sql.split('"')[1], None)
            return ""
        if sql.startswith("REVOKE tendo FROM"):
            return ""
        if sql.startswith("DROP ROLE"):
            self.roles.pop(sql.split('"')[1], None)
            return ""
        return ""


class ProvisioningTests(unittest.TestCase):
    def test_creates_unique_nonempty_resources_and_records_ownership(self):
        first, second = FakeService(), FakeService()
        first.provision()
        second.provision()
        self.assertTrue(first.database.startswith("tendo_db_") and len(first.database) > 12)
        self.assertTrue(first.migrator.startswith("tendo_m_") and first.migrator)
        self.assertTrue(first.runtime.startswith("tendo_r_") and first.runtime)
        self.assertNotEqual(first.database, second.database)
        self.assertEqual(len(first.created_roles), 2)
        self.assertEqual(len(first.created_dbs), 1)
        self.assertEqual(set(first.child_env), {"TEST_DATABASE_ADMIN_URL", "TEST_DATABASE_URL", "TEST_DATABASE_RUNTIME_ROLE", "TEST_DATABASE_MIGRATOR_ROLE"})
        self.assertTrue(any("TEMPLATE template0" in sql for sql, _, _ in first.statements))
        self.assertEqual(first.run("SELECT 1"), "")

    def test_rejects_wrong_version_and_privileged_role_before_writes(self):
        cases = [FakeService(version="170006"), FakeService(role="1|t|t|f|f|f|f")]
        for control in cases:
            with self.subTest(control=control), self.assertRaises(RuntimeError):
                control.provision()
            self.assertFalse(any("CREATE ROLE" in sql or "CREATE DATABASE" in sql for sql, _, _ in control.statements))

    def test_rejects_any_inherited_role_before_writes(self):
        control = FakeService(inherited="pg_read_all_data")
        with self.assertRaisesRegex(RuntimeError, "inherits other roles"):
            control.validate_runtime_role("candidate")
        self.assertFalse(any("CREATE ROLE" in sql or "CREATE DATABASE" in sql for sql, _, _ in control.statements))

    def test_role_transaction_failure_leaves_no_partial_roles(self):
        control = FakeService(fail_on="runtime-role")
        with self.assertRaisesRegex(RuntimeError, "injected runtime-role failure"):
            control.provision()
        self.assertEqual(control.roles, {})
        self.assertEqual(control.created_roles, {})
        control.cleanup()

    def test_role_oid_readback_failure_is_reported_without_adopting(self):
        control = FakeService()
        original = control.run
        def fail_readback(sql, **kwargs):
            if sql.startswith("BEGIN; CREATE ROLE"):
                original(sql, **kwargs)
                raise RuntimeError("connection lost after role transaction")
            return original(sql, **kwargs)
        control.run = fail_readback
        with self.assertRaisesRegex(RuntimeError, "connection lost"):
            control.provision()
        self.assertEqual(control.created_roles, {})
        self.assertTrue(control.roles)
        self.assertTrue(control.role_transaction_uncertain)
        control.run = original
        with self.assertRaisesRegex(RuntimeError, "completion is uncertain"):
            control.cleanup()
        self.assertTrue(control.roles, "unknown role ownership must be left intact")

    def test_database_create_collision_does_not_adopt_or_drop_preexisting_name(self):
        control = FakeService()
        suffix = "0123456789abcdef0123"
        control.database = f"tendo_db_{suffix}"
        control.databases[control.database] = 77
        with mock.patch.object(service.secrets, "token_hex", return_value=suffix), self.assertRaisesRegex(RuntimeError, "database collision"):
            control.provision()
        self.assertEqual(control.databases, {f"tendo_db_{suffix}": 77})
        self.assertEqual(control.created_roles.keys(), {control.migrator, control.runtime})
        control.cleanup()
        self.assertEqual(control.databases, {f"tendo_db_{suffix}": 77})
        self.assertFalse(control.roles)

    def test_cleanup_refuses_changed_catalog_oid_without_dropping_replacement(self):
        control = FakeService()
        control.provision()
        name = control.database
        control.databases[name] += 1000
        with self.assertRaisesRegex(RuntimeError, "catalog OID changed"):
            control.cleanup()
        self.assertIn(name, control.databases)

    def test_role_collision_is_not_adopted(self):
        control = FakeService()
        control.provision()
        existing = dict(control.roles)
        with self.assertRaisesRegex(RuntimeError, "service provisioning may run only once"):
            control.provision()
        self.assertEqual(control.roles, existing)

    def test_context_exception_stays_primary_when_cleanup_fails(self):
        control = FakeService()
        with mock.patch.object(service, "Service", return_value=control), mock.patch.object(FakeService, "cleanup", side_effect=RuntimeError("secondary cleanup failure")):
            with self.assertRaisesRegex(ValueError, "primary test failure"):
                with service.postgres_test_service("postgresql://admin:secret@localhost/postgres"):
                    raise ValueError("primary test failure")

    def test_cleanup_failure_after_success_is_reported(self):
        control = FakeService()
        with mock.patch.object(service, "Service", return_value=control), mock.patch.object(FakeService, "cleanup", side_effect=RuntimeError("cleanup failed")):
            with self.assertRaisesRegex(RuntimeError, "cleanup failed"):
                with service.postgres_test_service("postgresql://admin:secret@localhost/postgres"):
                    pass

    def test_cleanup_removes_owned_orphan_upgrade_db_and_not_unrelated_name(self):
        control = FakeService()
        control.provision()
        control.databases["tendo_upgrade_owned"] = 990
        control.databases["legacy_upgrade_sentinel"] = 991
        control.cleanup()
        self.assertEqual(control.databases, {"legacy_upgrade_sentinel": 991})

    def test_role_oid_replacement_refuses_orphan_sweep(self):
        control = FakeService()
        control.provision()
        control.databases["tendo_upgrade_owned"] = 990
        control.roles[control.migrator] += 1
        with self.assertRaisesRegex(RuntimeError, "migrator role OID changed"):
            control.cleanup()
        self.assertIn("tendo_upgrade_owned", control.databases)


if __name__ == "__main__":
    unittest.main()
