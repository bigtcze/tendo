package database

import (
	"context"
	"fmt"
)

func validateItemSearch(ctx context.Context, conn migrationConn) error {
	var valid bool
	err := conn.QueryRow(ctx, `
SELECT
  EXISTS (SELECT 1 FROM pg_catalog.pg_extension e JOIN pg_catalog.pg_namespace n ON n.oid=e.extnamespace WHERE e.extname='unaccent' AND n.nspname='public')
  AND EXISTS (SELECT 1 FROM pg_catalog.pg_extension e JOIN pg_catalog.pg_namespace n ON n.oid=e.extnamespace WHERE e.extname='pg_trgm' AND n.nspname='public')
  AND EXISTS (
    SELECT 1 FROM pg_catalog.pg_attribute a
    JOIN pg_catalog.pg_class c ON c.oid=a.attrelid
    JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace
    JOIN pg_catalog.pg_collation co ON co.oid=a.attcollation
    WHERE n.nspname='public' AND c.relname='items' AND a.attname='search_text'
      AND a.attnotnull AND a.attgenerated='' AND a.atttypid='text'::regtype AND co.collname='C'
  )
  AND EXISTS (
    SELECT 1 FROM pg_catalog.pg_trigger t
    JOIN pg_catalog.pg_class c ON c.oid=t.tgrelid
    JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace
    JOIN pg_catalog.pg_attribute title ON title.attrelid=c.oid AND title.attname='title'
    JOIN pg_catalog.pg_attribute notes ON notes.attrelid=c.oid AND notes.attname='notes'
    JOIN pg_catalog.pg_attribute search_text ON search_text.attrelid=c.oid AND search_text.attname='search_text'
    JOIN pg_catalog.pg_proc p ON p.oid=t.tgfoid
    JOIN pg_catalog.pg_namespace pn ON pn.oid=p.pronamespace
    JOIN pg_catalog.pg_namespace en ON en.nspname='public'
    JOIN pg_catalog.pg_proc expected ON expected.pronamespace=en.oid AND expected.proname='tendo_items_search_text' AND expected.proargtypes=''
    WHERE n.nspname='public' AND c.relname='items' AND t.tgname='items_search_text' AND t.tgfoid=expected.oid
      AND NOT t.tgisinternal AND t.tgenabled='O'
      AND t.tgtype = (1 | 2 | 4 | 16)
      AND array_length(t.tgattr::smallint[], 1)=3
      AND t.tgattr[0]=title.attnum AND t.tgattr[1]=notes.attnum AND t.tgattr[2]=search_text.attnum

      AND t.tgqual IS NULL
      AND pn.nspname='public' AND p.proname='tendo_items_search_text'
      AND p.prorettype='trigger'::regtype AND NOT p.prosecdef
      AND p.proconfig @> ARRAY['search_path=pg_catalog, pg_temp']
  )
  AND EXISTS (
    SELECT 1 FROM pg_catalog.pg_proc p JOIN pg_catalog.pg_namespace n ON n.oid=p.pronamespace
    WHERE n.nspname='public' AND p.proname='tendo_item_search_normalize'
      AND p.proargtypes='25'::oidvector AND p.prorettype='text'::regtype
      AND NOT p.prosecdef AND p.proconfig @> ARRAY['search_path=pg_catalog, pg_temp']
  )
  AND EXISTS (
    SELECT 1 FROM pg_catalog.pg_proc p JOIN pg_catalog.pg_namespace n ON n.oid=p.pronamespace
    WHERE n.nspname='public' AND p.proname='tendo_item_search_pattern'
      AND p.proargtypes='25'::oidvector AND p.prorettype='text'::regtype
      AND NOT p.prosecdef AND p.proconfig @> ARRAY['search_path=pg_catalog, pg_temp']
  )
  AND EXISTS (
    SELECT 1 FROM pg_catalog.pg_index i
    JOIN pg_catalog.pg_class idx ON idx.oid=i.indexrelid
    JOIN pg_catalog.pg_namespace idxns ON idxns.oid=idx.relnamespace
    JOIN pg_catalog.pg_class tbl ON tbl.oid=i.indrelid
    JOIN pg_catalog.pg_namespace n ON n.oid=tbl.relnamespace
    JOIN pg_catalog.pg_am am ON am.oid=idx.relam
    JOIN pg_catalog.pg_attribute search_text ON search_text.attrelid=tbl.oid AND search_text.attname='search_text'
    JOIN pg_catalog.pg_opclass opc ON opc.oid=i.indclass[0]
    JOIN pg_catalog.pg_namespace opn ON opn.oid=opc.opcnamespace
    WHERE n.nspname='public' AND tbl.relname='items' AND idxns.nspname='public' AND idx.relname='items_search_text_trgm_idx'
      AND i.indisvalid AND i.indisready AND am.amname='gin'
      AND i.indnatts=1 AND i.indkey[0]=search_text.attnum
      AND i.indclass[0]=opc.oid
      AND pg_catalog.array_length(i.indclass::oid[],1)=1
      AND i.indexprs IS NULL AND i.indpred IS NULL
      AND opc.opcname='gin_trgm_ops' AND opn.nspname='public'
  )`).Scan(&valid)
	if err != nil {
		return fmt.Errorf("item search schema query failed: %w", err)
	}
	if !valid {
		return fmt.Errorf("item search schema is invalid (catalog checks failed)")
	}
	return nil
}
