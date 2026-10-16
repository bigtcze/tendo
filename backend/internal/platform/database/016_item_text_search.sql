-- +goose Up
CREATE EXTENSION IF NOT EXISTS unaccent WITH SCHEMA public;
CREATE EXTENSION IF NOT EXISTS pg_trgm WITH SCHEMA public;
DO $$
BEGIN
 IF EXISTS (SELECT 1 FROM pg_catalog.pg_extension e JOIN pg_catalog.pg_namespace n ON n.oid = e.extnamespace
   WHERE e.extname IN ('unaccent','pg_trgm') AND n.nspname <> 'public') THEN
   RAISE EXCEPTION 'item search extensions must be installed in public';
 END IF;
END
$$;
CREATE FUNCTION public.tendo_item_search_normalize(value text) RETURNS text
LANGUAGE sql STABLE STRICT SECURITY INVOKER SET search_path = pg_catalog, pg_temp
RETURN pg_catalog.normalize(public.unaccent('public.unaccent'::pg_catalog.regdictionary,
  pg_catalog.casefold(pg_catalog.normalize(value, 'NFC') COLLATE pg_catalog.pg_unicode_fast)), 'NFC');
CREATE FUNCTION public.tendo_item_search_pattern(value text) RETURNS text
LANGUAGE sql STABLE STRICT SECURITY INVOKER SET search_path = pg_catalog, pg_temp
RETURN CASE WHEN public.tendo_item_search_normalize(value) = '' THEN NULL
 ELSE '%' || pg_catalog.replace(pg_catalog.replace(pg_catalog.replace(
   public.tendo_item_search_normalize(value), E'\\', E'\\\\'), '%', E'\\%'), '_', E'\\_') || '%' END;
ALTER TABLE public.items ADD COLUMN search_text text COLLATE pg_catalog."C";
CREATE FUNCTION public.tendo_items_search_text() RETURNS trigger
LANGUAGE plpgsql VOLATILE SECURITY INVOKER SET search_path = pg_catalog, pg_temp AS $$
BEGIN
 NEW.search_text := public.tendo_item_search_normalize(NEW.title || E'\n' || COALESCE(NEW.notes, ''));
 RETURN NEW;
END
$$;
CREATE TRIGGER items_search_text BEFORE INSERT OR UPDATE OF title, notes, search_text ON public.items
FOR EACH ROW EXECUTE FUNCTION public.tendo_items_search_text();
UPDATE public.items SET search_text = public.tendo_item_search_normalize(title || E'\n' || COALESCE(notes, ''));
ALTER TABLE public.items ALTER COLUMN search_text SET NOT NULL;
CREATE INDEX items_search_text_trgm_idx ON public.items USING gin(search_text public.gin_trgm_ops);
REVOKE ALL ON FUNCTION public.tendo_item_search_normalize(text) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.tendo_item_search_pattern(text) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.tendo_items_search_text() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.tendo_item_search_normalize(text) TO tendo;
GRANT EXECUTE ON FUNCTION public.tendo_item_search_pattern(text) TO tendo;
-- +goose Down
DROP TRIGGER items_search_text ON public.items;
DROP FUNCTION public.tendo_items_search_text();
DROP INDEX public.items_search_text_trgm_idx;
ALTER TABLE public.items DROP COLUMN search_text;
DROP FUNCTION public.tendo_item_search_pattern(text);
DROP FUNCTION public.tendo_item_search_normalize(text);
-- extensions intentionally left installed
