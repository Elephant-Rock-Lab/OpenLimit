package usage

import (
	"context"
	"database/sql"
	"fmt"
)

// BudgetResult indicates whether a request should be allowed based on budget.
type BudgetResult struct {
	Allowed bool
	Spend   float64
	Limit   float64
}

// CheckBudget evaluates whether a request should be allowed based on the
// current spend against the budget limit. It consolidates the budget check
// logic that was previously duplicated across auth/middleware.go,
// api/openai/governed.go, and api/openai/embeddings.go.
//
// Parameters:
//   - ctx:      request context for timeout/cancellation
//   - db:       database connection (may be nil — treated as error)
//   - keyID:    virtual key ID for spend lookup
//   - period:   budget period (e.g. "monthly")
//   - limit:    budget limit in USD (0 = no budget enforcement)
//   - failClosed: if true, DB errors cause the request to be rejected
//
// Returns:
//   - (allowed=true, nil)    when spend < limit or no budget configured
//   - (allowed=false, nil)   when spend >= limit (budget exceeded)
//   - (allowed=false, err)   when failClosed=true and DB error occurs
//   - (allowed=true, nil)    when failClosed=false and DB error occurs (fail-open)
func CheckBudget(ctx context.Context, db *sql.DB, keyID string, period string, limit float64, failClosed bool) (*BudgetResult, error) {
	if limit <= 0 {
		return &BudgetResult{Allowed: true}, nil
	}
	if db == nil {
		if failClosed {
			return nil, fmt.Errorf("budget check failed: database unavailable")
		}
		return &BudgetResult{Allowed: true}, nil
	}

	spend, err := GetSpendForCurrentPeriod(ctx, db, keyID, period)
	if err != nil {
		if failClosed {
			return nil, fmt.Errorf("budget check failed: %w", err)
		}
		// Fail-open: DB error means we can't enforce budget, allow through
		return &BudgetResult{Allowed: true}, nil
	}

	if spend >= limit {
		return &BudgetResult{
			Allowed: false,
			Spend:   spend,
			Limit:   limit,
		}, nil
	}

	return &BudgetResult{
		Allowed: true,
		Spend:   spend,
		Limit:   limit,
	}, nil
}
