package item

type completionListQuery struct {
	limit  int
	order  string
	after  string
	newest bool
}

func parseCompletionListQuery(limit int, cursor, order string) (completionListQuery, error) {
	if limit == 0 {
		limit = DefaultLimit
	}
	if limit < 1 || limit > MaxLimit {
		return completionListQuery{}, &InvalidQueryError{"limit"}
	}
	if order == "" {
		order = CompletionOrderOldest
	}
	if order != CompletionOrderOldest && order != CompletionOrderNewest {
		return completionListQuery{}, &InvalidQueryError{"order"}
	}
	query := completionListQuery{limit: limit, order: order, newest: order == CompletionOrderNewest, after: nilUUID}
	if query.newest {
		query.after = newestCompletionBoundary
	}
	if cursor != "" {
		after, err := DecodeCompletionCursorForOrder(cursor, order)
		if err != nil {
			return completionListQuery{}, &InvalidQueryError{"cursor"}
		}
		query.after = after
	}
	return query, nil
}
