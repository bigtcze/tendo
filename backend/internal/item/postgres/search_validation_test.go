package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/platform/database"
	"github.com/bigtcze/tendo/backend/internal/testpostgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

const validSearchTrigger = `CREATE TRIGGER items_search_text BEFORE INSERT OR UPDATE OF title, notes, search_text ON public.items FOR EACH ROW EXECUTE FUNCTION public.tendo_items_search_text()`
const validSearchPattern = `CREATE FUNCTION public.tendo_item_search_pattern(value text) RETURNS text LANGUAGE sql STABLE STRICT SECURITY INVOKER SET search_path = pg_catalog, pg_temp RETURN CASE WHEN public.tendo_item_search_normalize(value) = '' THEN NULL ELSE '%' || pg_catalog.replace(pg_catalog.replace(pg_catalog.replace(public.tendo_item_search_normalize(value), E'\\', E'\\\\'), '%', E'\\%'), '_', E'\\_') || '%' END`
const validSearchNormalizer = `CREATE FUNCTION public.tendo_item_search_normalize(value text) RETURNS text LANGUAGE sql STABLE STRICT SECURITY INVOKER SET search_path = pg_catalog, pg_temp RETURN pg_catalog.normalize(public.unaccent('public.unaccent'::pg_catalog.regdictionary, pg_catalog.casefold(pg_catalog.normalize(value, 'NFC') COLLATE pg_catalog.pg_unicode_fast)), 'NFC')`
const validSearchTriggerFunction = `CREATE FUNCTION public.tendo_items_search_text() RETURNS trigger LANGUAGE plpgsql VOLATILE SECURITY INVOKER SET search_path = pg_catalog, pg_temp AS $$ BEGIN NEW.search_text := public.tendo_item_search_normalize(NEW.title || E'\n' || COALESCE(NEW.notes, '')); RETURN NEW; END $$`

func TestValidateSchemaRequiresItemSearchObjects(t *testing.T) {
	appURL, adminURL := integrationURLs(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	adminConfig, err := pgxpool.ParseConfig(adminURL)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := pgxpool.NewWithConfig(ctx, adminConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	dbName := testpostgres.CreateDatabase(t, ctx, admin)
	adminDB, err := pgxpool.NewWithConfig(ctx, testpostgres.PoolConfigForDatabase(t, adminURL, dbName))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(adminDB.Close)
	appDB, err := pgxpool.NewWithConfig(ctx, testpostgres.PoolConfigForDatabase(t, appURL, dbName))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(appDB.Close)
	if err = database.Migrate(ctx, adminDB); err != nil {
		t.Fatal(err)
	}
	cases := []struct{ name, bad, restore string }{
		{"drop trigger", `DROP TRIGGER items_search_text ON public.items`, validSearchTrigger},
		{"disable trigger", `ALTER TABLE public.items DISABLE TRIGGER items_search_text`, `ALTER TABLE public.items ENABLE TRIGGER items_search_text`},
		{"drop index", `DROP INDEX public.items_search_text_trgm_idx`, `CREATE INDEX items_search_text_trgm_idx ON public.items USING gin(search_text public.gin_trgm_ops)`},
		{"drop normalizer", `DROP FUNCTION public.tendo_item_search_pattern(text); DROP FUNCTION public.tendo_item_search_normalize(text)`, validSearchNormalizer + `; ` + validSearchPattern},
		{"drop pattern", `DROP FUNCTION public.tendo_item_search_pattern(text)`, validSearchPattern},
		{"drop trigger function", `DROP TRIGGER items_search_text ON public.items; DROP FUNCTION public.tendo_items_search_text()`, validSearchTriggerFunction + `; ` + validSearchTrigger},
		{"trigger when false", `DROP TRIGGER items_search_text ON public.items; CREATE TRIGGER items_search_text BEFORE INSERT OR UPDATE OF title, notes, search_text ON public.items FOR EACH ROW WHEN (false) EXECUTE FUNCTION public.tendo_items_search_text()`, `DROP TRIGGER items_search_text ON public.items; ` + validSearchTrigger},
		{"trigger insert only", `DROP TRIGGER items_search_text ON public.items; CREATE TRIGGER items_search_text BEFORE INSERT ON public.items FOR EACH ROW EXECUTE FUNCTION public.tendo_items_search_text()`, `DROP TRIGGER items_search_text ON public.items; ` + validSearchTrigger},
		{"trigger function definer", `DROP TRIGGER items_search_text ON public.items; DROP FUNCTION public.tendo_items_search_text(); CREATE FUNCTION public.tendo_items_search_text() RETURNS trigger LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path = pg_catalog, pg_temp AS $$ BEGIN NEW.search_text := public.tendo_item_search_normalize(NEW.title || E'\n' || COALESCE(NEW.notes, '')); RETURN NEW; END $$; CREATE TRIGGER items_search_text BEFORE INSERT OR UPDATE OF title, notes, search_text ON public.items FOR EACH ROW EXECUTE FUNCTION public.tendo_items_search_text()`, `CREATE OR REPLACE FUNCTION public.tendo_items_search_text() RETURNS trigger LANGUAGE plpgsql VOLATILE SECURITY INVOKER SET search_path = pg_catalog, pg_temp AS $$ BEGIN NEW.search_text := public.tendo_item_search_normalize(NEW.title || E'\n' || COALESCE(NEW.notes, '')); RETURN NEW; END $$`},
		{"trigger function search path", `DROP TRIGGER items_search_text ON public.items; DROP FUNCTION public.tendo_items_search_text(); CREATE FUNCTION public.tendo_items_search_text() RETURNS trigger LANGUAGE plpgsql VOLATILE SECURITY INVOKER AS $$ BEGIN NEW.search_text := public.tendo_item_search_normalize(NEW.title || E'\n' || COALESCE(NEW.notes, '')); RETURN NEW; END $$; CREATE TRIGGER items_search_text BEFORE INSERT OR UPDATE OF title, notes, search_text ON public.items FOR EACH ROW EXECUTE FUNCTION public.tendo_items_search_text()`, `CREATE OR REPLACE FUNCTION public.tendo_items_search_text() RETURNS trigger LANGUAGE plpgsql VOLATILE SECURITY INVOKER SET search_path = pg_catalog, pg_temp AS $$ BEGIN NEW.search_text := public.tendo_item_search_normalize(NEW.title || E'\n' || COALESCE(NEW.notes, '')); RETURN NEW; END $$`},
		{"partial index", `DROP INDEX public.items_search_text_trgm_idx; CREATE INDEX items_search_text_trgm_idx ON public.items USING gin(search_text public.gin_trgm_ops) WHERE false`, `DROP INDEX public.items_search_text_trgm_idx; CREATE INDEX items_search_text_trgm_idx ON public.items USING gin(search_text public.gin_trgm_ops)`},
		{"expression index", `DROP INDEX public.items_search_text_trgm_idx; CREATE INDEX items_search_text_trgm_idx ON public.items USING gin((lower(search_text)) public.gin_trgm_ops)`, `DROP INDEX public.items_search_text_trgm_idx; CREATE INDEX items_search_text_trgm_idx ON public.items USING gin(search_text public.gin_trgm_ops)`},
		{"normalizer varchar", `DROP FUNCTION public.tendo_item_search_pattern(text); DROP FUNCTION public.tendo_item_search_normalize(text); CREATE FUNCTION public.tendo_item_search_normalize(value text) RETURNS varchar LANGUAGE sql STABLE STRICT SECURITY INVOKER SET search_path = pg_catalog, pg_temp RETURN value::varchar; CREATE FUNCTION public.tendo_item_search_pattern(value text) RETURNS text LANGUAGE sql STABLE STRICT SECURITY INVOKER SET search_path = pg_catalog, pg_temp RETURN value`, `DROP FUNCTION public.tendo_item_search_pattern(text); DROP FUNCTION public.tendo_item_search_normalize(text); ` + validSearchNormalizer + `; ` + validSearchPattern},
		{"normalizer search path", `DROP FUNCTION public.tendo_item_search_pattern(text); DROP FUNCTION public.tendo_item_search_normalize(text); CREATE FUNCTION public.tendo_item_search_normalize(value text) RETURNS text LANGUAGE sql STABLE STRICT SECURITY INVOKER SET search_path = public RETURN value; CREATE FUNCTION public.tendo_item_search_pattern(value text) RETURNS text LANGUAGE sql STABLE STRICT SECURITY INVOKER SET search_path = pg_catalog, pg_temp RETURN value`, `DROP FUNCTION public.tendo_item_search_pattern(text); DROP FUNCTION public.tendo_item_search_normalize(text); ` + validSearchNormalizer + `; ` + validSearchPattern},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, e := adminDB.Exec(ctx, tc.bad); e != nil {
				t.Fatalf("install malformed object: %v", e)
			}
			for name, pool := range map[string]*pgxpool.Pool{"admin": adminDB, "runtime": appDB} {
				if e := database.ValidateSchema(ctx, pool); e == nil {
					t.Errorf("%s pool accepted malformed object", name)
				}
			}
			if _, e := adminDB.Exec(ctx, tc.restore); e != nil {
				t.Fatalf("restore object: %v", e)
			}
			for name, pool := range map[string]*pgxpool.Pool{"admin": adminDB, "runtime": appDB} {
				if e := database.ValidateSchema(ctx, pool); e != nil {
					t.Errorf("%s pool rejected restored object: %v", name, e)
				}
			}
		})
	}
}
